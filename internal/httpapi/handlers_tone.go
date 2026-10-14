package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/attempts"
	"github.com/valeriandwipurnomo/hanpath/internal/exercises"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// Tone practice: drills on tone alone, from anywhere in the curriculum.
//
// Two decisions shape this surface, and both are deliberate:
//
//   - The queue is NOT gated by the path. A tone question is self-contained, and the
//     whole point of the page is to drill tone without walking a level first — a
//     learner's tone unit sits behind three units, and the review deck is empty until
//     something has been completed. It IS filtered by level, because answering
//     questions about words you have never met teaches nothing; the default is the
//     level the learner says they are working on.
//   - Attempts are off-path (SubmitOffPath, mode "tone"). They are recorded in the same
//     append-only log, so the tone record survives, but nothing derived moves: no
//     lesson status, no XP, no mastery, no daily rollup, no streak. Without that, a
//     tone drill served from a locked lesson would flip the lesson to in_progress and
//     let practice erode the linear path.

const (
	toneDefaultLimit = 10
	toneMaxLimit     = 50
	toneMaxAnswers   = 20
)

// toneDefaultLevel is the level a fresh learner starts on.
//
// users.target_level is nullable, and reading a nullable setting as if it were always
// set is how a feature quietly disappears for every new account. A learner who has not
// said which level they are on gets level 1 rather than every level at once.
func toneDefaultLevel(user store.User) int {
	if user.TargetLevel != nil && *user.TargetLevel > 0 && *user.TargetLevel <= 5 {
		return *user.TargetLevel
	}
	return 1
}

// toneItem is one drill as the learner sees it: the prompt, the level it comes from,
// and the playable audio URL the shared helper attaches. The answer key never appears —
// this is the same rule as a lesson payload.
type toneItem struct {
	store.ExerciseForLearner
	Level int `json:"level"`
}

func (s *Server) handleToneQueue(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	limit := parseLimit(r, toneDefaultLimit, toneMaxLimit)

	level := toneDefaultLevel(*user)
	if raw := r.URL.Query().Get("level"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 5 {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"level must be 0 (all levels) or 1 to 5", nil)
			return
		}
		level = n
	}

	drills, err := s.store.ToneQueue(r.Context(), user.ID, level, limit)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	// The audio URL belongs on the payload the learner receives, so the shared helper
	// runs over the prompts and the levels are carried alongside.
	prompts := make([]store.ExerciseForLearner, 0, len(drills))
	levels := make(map[uuid.UUID]int, len(drills))
	for _, d := range drills {
		prompts = append(prompts, d.ExerciseForLearner)
		levels[d.ID] = d.Level
	}
	s.attachAudioURLs(prompts)

	items := make([]toneItem, 0, len(prompts))
	for _, p := range prompts {
		items = append(items, toneItem{ExerciseForLearner: p, Level: levels[p.ID]})
	}

	stats, err := s.store.ToneStats(r.Context(), user.ID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items": items,
		"level": level,
		"stats": stats,
	})
}

type toneAnswer struct {
	ExerciseID uuid.UUID `json:"exercise_id"`
	ClientRef  string    `json:"client_ref"`
	OptionID   string    `json:"option_id"`
}

type toneAnswersRequest struct {
	Answers []toneAnswer `json:"answers"`
}

// toneTallyRow is the session's own diagnosis: how often each tone came up and how
// often it was missed. This is the thing a tone test is FOR — "you miss the third tone"
// is the finding, and it is computed from the answer keys on the server.
type toneTallyRow struct {
	Tone   string `json:"tone"`
	Label  string `json:"label"`
	Asked  int    `json:"asked"`
	Missed int    `json:"missed"`
}

func (s *Server) handleToneAnswers(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())

	// Its own rate-limit budget, so a learner hammering tone drills cannot lock
	// themselves out of the lesson path (and vice versa).
	if ok, _ := s.writeLimiter.Allow("tones:" + user.ID.String()); !ok {
		w.Header().Set("Retry-After", "5")
		writeError(w, r, http.StatusTooManyRequests, "rate_limited",
			"too many tone answers in a short window, retry shortly", nil)
		return
	}

	var req toneAnswersRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	switch {
	case len(req.Answers) == 0:
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "no answers in submission", nil)
		return
	case len(req.Answers) > toneMaxAnswers:
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
			"too many answers in one submission", nil)
		return
	}

	ids := make([]uuid.UUID, 0, len(req.Answers))
	seen := map[uuid.UUID]bool{}
	for _, a := range req.Answers {
		if a.ExerciseID == uuid.Nil {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "exercise_id is required", nil)
			return
		}
		if !seen[a.ExerciseID] {
			seen[a.ExerciseID] = true
			ids = append(ids, a.ExerciseID)
		}
	}

	// Only tone drills may be graded here. The check comes from the store's own
	// predicate — the same one the queue uses — so the two surfaces cannot drift into
	// disagreeing about what a tone drill is.
	toneIDs, err := s.store.ToneDrillIDs(r.Context(), ids)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	for _, id := range ids {
		if !toneIDs[id] {
			writeError(w, r, http.StatusUnprocessableEntity, "not_a_tone_drill",
				"this endpoint grades tone drills only", nil)
			return
		}
	}

	inputs := make([]attempts.Input, 0, len(req.Answers))
	for _, a := range req.Answers {
		// The wire shape the tone drill's grader expects, built here rather than
		// forwarded: the client sends an option id, and what a tone answer looks like
		// stays the server's business.
		response, err := json.Marshal(map[string]any{"option_id": a.OptionID})
		if err != nil {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"could not read the submitted answer", nil)
			return
		}
		inputs = append(inputs, attempts.Input{
			ExerciseID: a.ExerciseID,
			ClientRef:  a.ClientRef,
			Response:   response,
		})
	}

	summary, err := s.attempts.SubmitOffPath(r.Context(), user.ID, "tone", inputs)
	if err != nil {
		switch {
		case errors.Is(err, store.ErrClientRefConflict):
			writeError(w, r, http.StatusConflict, "client_ref_conflict",
				"a client_ref in this submission was already used for a different exercise", nil)
		case errors.Is(err, attempts.ErrEmptyBatch), errors.Is(err, attempts.ErrBatchTooLarge),
			errors.Is(err, attempts.ErrBadMode), errors.Is(err, attempts.ErrUnknownExercise):
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", err.Error(), nil)
		case errors.Is(err, attempts.ErrBadResponse):
			writeError(w, r, http.StatusUnprocessableEntity, "invalid_response",
				"the submitted answer does not match this exercise type", nil)
		default:
			s.respondStoreError(w, r, err)
		}
		return
	}

	// Session-scoped on purpose: it diagnoses this round, and the lifetime numbers come
	// from ToneStats below.
	tally, err := s.toneTally(r, req.Answers, summary)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	stats, err := s.store.ToneStats(r.Context(), user.ID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"results":    summary.Results,
		"tone_tally": tally,
		"stats":      stats,
	})
}

// toneTally counts, per tone digit, how often it was asked and how often it was missed
// in this batch. It reads the tones from the stored answer key, never from the payload.
func (s *Server) toneTally(r *http.Request, answers []toneAnswer, summary *attempts.Summary) ([]toneTallyRow, error) {
	keys, err := s.store.ExercisesForGrading(r.Context(), exerciseIDsOf(answers))
	if err != nil {
		return nil, err
	}

	type counts struct{ asked, missed int }
	byTone := map[string]*counts{}
	order := make([]string, 0, 5)

	// Results come back in submission order, so walking them beside the answers is
	// what pairs a verdict with the tones it was judged against.
	for i, res := range summary.Results {
		if i >= len(answers) {
			break
		}
		spec, ok := keys[answers[i].ExerciseID]
		if !ok {
			continue
		}
		var key struct {
			Tones string `json:"tones"`
		}
		if err := json.Unmarshal(spec.AnswerKey, &key); err != nil || key.Tones == "" {
			continue
		}
		missed := res.IsCorrect == nil || !*res.IsCorrect
		for _, tone := range splitTones(key.Tones) {
			c, ok := byTone[tone]
			if !ok {
				c = &counts{}
				byTone[tone] = c
				order = append(order, tone)
			}
			c.asked++
			if missed {
				c.missed++
			}
		}
	}

	out := make([]toneTallyRow, 0, len(order))
	for _, tone := range order {
		c := byTone[tone]
		out = append(out, toneTallyRow{
			Tone: tone, Label: exercises.ToneName(tone), Asked: c.asked, Missed: c.missed,
		})
	}
	return out, nil
}

// splitTones turns "4-3" into its digits. A pair drill's answer key carries no tones
// (it asks which word was heard, not which tones), so it contributes nothing here.
func splitTones(pattern string) []string {
	out := make([]string, 0, 4)
	current := ""
	for _, r := range pattern {
		switch {
		case r >= '1' && r <= '5':
			current += string(r)
		case r == '-':
			if current != "" {
				out = append(out, current)
				current = ""
			}
		}
	}
	if current != "" {
		out = append(out, current)
	}
	return out
}

func exerciseIDsOf(answers []toneAnswer) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(answers))
	seen := map[uuid.UUID]bool{}
	for _, a := range answers {
		if !seen[a.ExerciseID] {
			seen[a.ExerciseID] = true
			ids = append(ids, a.ExerciseID)
		}
	}
	return ids
}
