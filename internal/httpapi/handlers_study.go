package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/attempts"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// The study surfaces: the writing studio, the speaking studio, the passage reader
// and the study guide.
//
// Like the tone page, these serve content from any level — ungated by the path — and
// their attempts are recorded with mode "studio" and excluded from every derived
// value (migrations/0008_study.sql). The studios are where a learner practises
// something the path has not reached yet, so counting that practice as progress
// would let practice erode the linear path.
//
// What each endpoint grades is fixed by StudioDrillIDs, the store's own predicate,
// so an endpoint cannot drift into grading drills its queue would never serve.

const (
	studioDefaultLimit = 10
	studioMaxLimit     = 50
)

// writingTypes / speakingTypes are the studio queues. Each list lives here once, so
// the queue, the stats and the ID predicate read the same set.
var writingTypes = []string{"hanzi_trace", "hanzi_recall"}
var speakingTypes = []string{"speak_repeat", "read_aloud", "speak_answer"}

// studyLevel reads the ?level= query parameter, applying the same rule as the tone
// queue: 0 is every level, 1-5 one level, and no parameter means the learner's own
// level (which the caller supplies).
func studyLevel(w http.ResponseWriter, r *http.Request, defaultLevel int) (int, bool) {
	level := defaultLevel
	if raw := r.URL.Query().Get("level"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 5 {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"level must be 0 (all levels) or 1 to 5", nil)
			return 0, false
		}
		level = n
	}
	return level, true
}

// handleStudioQueue serves the writing studio's drills.
func (s *Server) handleStudioQueue(w http.ResponseWriter, r *http.Request) {
	s.serveStudioQueue(w, r, writingTypes, "writing")
}

// handleSpeakingQueue serves the speaking studio's drills.
func (s *Server) handleSpeakingQueue(w http.ResponseWriter, r *http.Request) {
	s.serveStudioQueue(w, r, speakingTypes, "speaking")
}

func (s *Server) serveStudioQueue(w http.ResponseWriter, r *http.Request, types []string, name string) {
	user, _ := userFrom(r.Context())
	limit := parseLimit(r, studioDefaultLimit, studioMaxLimit)

	// The studios are ungated practice, so a round is not confined to the level the
	// learner happens to be on: the default is every level, and the page's level control
	// narrows it on request. The openapi spec has always documented `level` defaulting to
	// 0; defaulting to the learner's own level instead meant a beginner's writing round
	// was the three drills level 1 happens to contain.
	level, ok := studyLevel(w, r, 0)
	if !ok {
		return
	}

	// With no level asked for, the learner's own level is served first and the round is
	// filled from the rest: a full round of practice that still starts where they are.
	prefer := 0
	if level == 0 {
		prefer = toneDefaultLevel(*user)
	}

	// The writing studio's character search: drills for one character, from any level.
	// Only prompts that carry a `character` can match, so the speaking studio ignores it.
	character := strings.TrimSpace(r.URL.Query().Get("character"))
	if utf8.RuneCountInString(character) > 4 {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
			"character must be at most 4 characters", nil)
		return
	}

	drills, err := s.store.StudioQueue(r.Context(), user.ID, types, level, limit, prefer, character)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	prompts := make([]store.ExerciseForLearner, 0, len(drills))
	levels := make(map[uuid.UUID]int, len(drills))
	for _, d := range drills {
		prompts = append(prompts, d.ExerciseForLearner)
		levels[d.ID] = d.Level
	}
	s.attachAudioURLs(prompts)

	items := make([]store.StudyDrill, 0, len(prompts))
	for _, p := range prompts {
		items = append(items, store.StudyDrill{ExerciseForLearner: p, Level: levels[p.ID]})
	}

	stats, err := s.store.StudioStats(r.Context(), user.ID, types)
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

// studioAnswer is one answer as the studios submit it: the exercise, the idempotency
// ref, and the response in the same shape a lesson submission uses.
type studioAnswer struct {
	ExerciseID uuid.UUID       `json:"exercise_id"`
	ClientRef  string          `json:"client_ref"`
	Response   map[string]any  `json:"response"`
}

type studioAnswersRequest struct {
	Answers []studioAnswer `json:"answers"`
}

// handleStudioAttempt grades studio answers off-path.
func (s *Server) handleStudioAttempt(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())

	// Its own rate-limit budget, so studio practice cannot lock a learner out of the
	// lesson path (and vice versa).
	if ok, _ := s.writeLimiter.Allow("studio:" + user.ID.String()); !ok {
		w.Header().Set("Retry-After", "5")
		writeError(w, r, http.StatusTooManyRequests, "rate_limited",
			"too many studio answers in a short window, retry shortly", nil)
		return
	}

	var req studioAnswersRequest
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

	// Only studio drills may be graded here — the store's own predicate, the same
	// rule the tone endpoint enforces.
	studioIDs, err := s.store.StudioDrillIDs(r.Context(), ids)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	for _, id := range ids {
		if !studioIDs[id] {
			writeError(w, r, http.StatusUnprocessableEntity, "not_a_studio_drill",
				"this endpoint grades writing and speaking drills only", nil)
			return
		}
	}

	inputs := make([]attempts.Input, 0, len(req.Answers))
	for _, a := range req.Answers {
		response, err := json.Marshal(a.Response)
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

	// Off-path: studio practice must not move lesson status, XP, mastery, the daily
	// rollup or the streak.
	summary, err := s.attempts.SubmitOffPath(r.Context(), user.ID, "studio", inputs)
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

	stats, err := s.store.StudioStats(r.Context(), user.ID, append(writingTypes, speakingTypes...))
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"results": summary.Results,
		"stats":   stats,
	})
}

// --- study guide -------------------------------------------------------

// handleVocabularyList serves the guide's vocabulary browser.
func (s *Server) handleVocabularyList(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	version := r.URL.Query().Get("version")
	if version == "" {
		version = DefaultCurriculumVersion
	}

	level := 0
	if raw := r.URL.Query().Get("level"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 5 {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"level must be 0 (all levels) or 1 to 5", nil)
			return
		}
		level = n
	}

	limit := parseLimit(r, 50, 200)
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"offset must be 0 or more", nil)
			return
		}
		offset = n
	}
	search := r.URL.Query().Get("search")

	items, total, err := s.store.VocabularyList(r.Context(), user.ID, version, level, limit, offset, search)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"items":  items,
		"total":  total,
		"level":  level,
		"limit":  limit,
		"offset": offset,
	})
}

// handleGrammarNotes serves the guide's grammar section.
func (s *Server) handleGrammarNotes(w http.ResponseWriter, r *http.Request) {
	version := r.URL.Query().Get("version")
	if version == "" {
		version = DefaultCurriculumVersion
	}

	level := 0
	if raw := r.URL.Query().Get("level"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 5 {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"level must be 0 (all levels) or 1 to 5", nil)
			return
		}
		level = n
	}

	notes, err := s.store.GrammarNotes(r.Context(), version, level)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"items": notes, "level": level})
}

// handleReadingPassages serves the reader's passages. Reading records no
// attempts: a passage is read, not answered.
func (s *Server) handleReadingPassages(w http.ResponseWriter, r *http.Request) {
	version := r.URL.Query().Get("version")
	if version == "" {
		version = DefaultCurriculumVersion
	}

	level := 0
	if raw := r.URL.Query().Get("level"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 || n > 5 {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"level must be 0 (all levels) or 1 to 5", nil)
			return
		}
		level = n
	}

	passages, err := s.store.ReadingPassages(r.Context(), version, level)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": passages, "level": level})
}

// handleBookmarkAdd upserts a bookmark: tapping a bookmark twice is a toggle, not an
// error, and the client ref is irrelevant to idempotency here.
func (s *Server) handleBookmarkAdd(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	lexemeID, ok := parseUUIDParam(r, "lexeme_id")
	if !ok || lexemeID == uuid.Nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "lexeme id must be a UUID", nil)
		return
	}
	created, err := s.store.AddBookmark(r.Context(), user.ID, lexemeID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lexeme_id": lexemeID, "bookmarked": true, "created": created})
}

// handleBookmarkRemove deletes a bookmark; removing what is not there succeeds.
func (s *Server) handleBookmarkRemove(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	lexemeID, ok := parseUUIDParam(r, "lexeme_id")
	if !ok || lexemeID == uuid.Nil {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "lexeme id must be a UUID", nil)
		return
	}
	removed, err := s.store.RemoveBookmark(r.Context(), user.ID, lexemeID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"lexeme_id": lexemeID, "bookmarked": false, "removed": removed})
}

// handleBookmarksList lists the learner's bookmarked words.
func (s *Server) handleBookmarksList(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	version := r.URL.Query().Get("version")
	if version == "" {
		version = DefaultCurriculumVersion
	}
	items, err := s.store.BookmarksList(r.Context(), user.ID, version)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}
