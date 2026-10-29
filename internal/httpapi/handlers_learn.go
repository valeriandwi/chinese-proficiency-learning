package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/attempts"
	"github.com/valeriandwipurnomo/hanpath/internal/progress"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// --- curriculum and path ---------------------------------------------------

func (s *Server) handleCurricula(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.Curricula(r.Context())
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	type entry struct {
		store.Curriculum
		Levels []store.Level `json:"levels"`
	}
	out := make([]entry, 0, len(list))
	for _, c := range list {
		levels, err := s.store.Levels(r.Context(), c.ID)
		if err != nil {
			s.respondStoreError(w, r, err)
			return
		}
		if levels == nil {
			levels = []store.Level{}
		}
		out = append(out, entry{Curriculum: c, Levels: levels})
	}
	writeJSON(w, http.StatusOK, map[string]any{"curricula": out})
}

func (s *Server) handlePath(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	version := chiURLParam(r, "version")
	if version == "" {
		version = s.defaultCurriculumVersion(r)
	}
	levels, err := s.store.PathForUser(r.Context(), version, user.ID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "unknown curriculum version", nil)
			return
		}
		s.respondStoreError(w, r, err)
		return
	}
	states, err := s.deriveStates(r.Context(), user.ID, version)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	// Assign derived lock state. A lesson with no state yet is 'locked' until the
	// walker reaches it, which is exactly what DeriveLessonStates encodes.
	for li := range levels {
		for ui := range levels[li].Units {
			for i := range levels[li].Units[ui].Lessons {
				l := &levels[li].Units[ui].Lessons[i]
				if st, ok := states[l.ID]; ok {
					l.State = st
				} else if l.State == "" {
					l.State = "locked"
				}
			}
		}
	}
	if levels == nil {
		levels = []store.PathLevel{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"version": version, "levels": levels})
}

// deriveStates computes locked | available | in_progress | completed for every
// lesson of a curriculum version, in path order, for one learner.
func (s *Server) deriveStates(ctx context.Context, userID uuid.UUID, version string) (map[uuid.UUID]string, error) {
	c, err := s.store.CurriculumByVersion(ctx, version)
	if err != nil {
		return nil, err
	}
	refs, err := s.store.LessonRefsOrdered(ctx, c.ID)
	if err != nil {
		return nil, err
	}
	prog, completed, err := s.store.LessonProgressMap(ctx, userID)
	if err != nil {
		return nil, err
	}
	comp := make([]bool, len(refs))
	started := make([]bool, len(refs))
	for i, ref := range refs {
		comp[i] = completed[ref.LessonID]
		if p, ok := prog[ref.LessonID]; ok {
			started[i] = p.Attempts > 0
		}
	}
	// A level opened by a proficiency test makes its lessons available without any of
	// them being completed — the one deliberate exception to the linear rule.
	unlockedLevels, err := s.store.UnlockedLevelNumbers(ctx, userID, c.ID)
	if err != nil {
		return nil, err
	}
	opened := make([]bool, len(refs))
	for i, ref := range refs {
		opened[i] = unlockedLevels[ref.LevelNumber]
	}
	derived := progress.DeriveLessonStates(comp, started, opened)
	out := make(map[uuid.UUID]string, len(refs))
	for i, ref := range refs {
		out[ref.LessonID] = derived[i]
	}
	return out, nil
}

// lessonState resolves one lesson's derived state for a learner.
func (s *Server) lessonState(ctx context.Context, userID, lessonID uuid.UUID) (string, error) {
	version, err := s.store.CurriculumVersionForLesson(ctx, lessonID)
	if err != nil {
		return "", err
	}
	states, err := s.deriveStates(ctx, userID, version)
	if err != nil {
		return "", err
	}
	state, ok := states[lessonID]
	if !ok {
		return "", store.ErrNotFound
	}
	return state, nil
}

func (s *Server) handleLesson(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	id, ok := parseUUIDParam(r, "id")
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "lesson id must be a UUID", nil)
		return
	}
	detail, err := s.store.LessonDetail(r.Context(), id)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	states, err := s.deriveStates(r.Context(), user.ID, detail.CurriculumVersion)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	detail.State = states[id]
	if detail.State == "" {
		detail.State = "locked"
	}
	if detail.Exercises == nil {
		detail.Exercises = []store.ExerciseForLearner{}
	}
	if detail.Passages == nil {
		detail.Passages = []store.Passage{}
	}
	if detail.Vocabulary == nil {
		detail.Vocabulary = []store.Lexeme{}
	}
	s.attachAudioURLs(detail.Exercises)
	writeJSON(w, http.StatusOK, detail)
}

// attachAudioURLs adds a playable URL beside every audio_key in a lesson payload.
//
// The client is handed a URL rather than a storage key so the media location stays
// a server concern: MEDIA_URL_PREFIX or a move to object storage changes without a
// frontend release, and the browser never guesses a path. Fields are copied as
// json.RawMessage so the rest of the prompt is returned byte-for-byte as authored
// (re-encoding through map[string]any would turn every integer into a float).
func (s *Server) attachAudioURLs(exercises []store.ExerciseForLearner) {
	if s.media == nil {
		return
	}
	for i := range exercises {
		if len(exercises[i].Prompt) == 0 {
			continue
		}
		var probe struct {
			AudioKey string `json:"audio_key"`
		}
		if err := json.Unmarshal(exercises[i].Prompt, &probe); err != nil || probe.AudioKey == "" {
			continue
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(exercises[i].Prompt, &fields); err != nil {
			continue
		}
		url, err := json.Marshal(s.media.URL(probe.AudioKey))
		if err != nil {
			continue
		}
		fields["audio_url"] = url
		merged, err := json.Marshal(fields)
		if err != nil {
			continue
		}
		exercises[i].Prompt = merged
	}
}

// --- attempts --------------------------------------------------------------

type submitAttemptsRequest struct {
	Mode     string           `json:"mode"`
	Attempts []attempts.Input `json:"attempts"`
}

func (s *Server) handleSubmitAttempts(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	lessonID, ok := parseUUIDParam(r, "id")
	if !ok {
		writeError(w, r, http.StatusBadRequest, "invalid_id", "lesson id must be a UUID", nil)
		return
	}
	if ok, retry := s.writeLimiter.Allow("attempts:" + user.ID.String()); !ok {
		w.Header().Set("Retry-After", "5")
		writeError(w, r, http.StatusTooManyRequests, "rate_limited",
			"too many submissions in a short window, retry shortly", nil)
		_ = retry
		return
	}

	// The path is linear and the server owns progression: a locked lesson must not
	// accept submissions even if a client constructs the request by hand. Reading a
	// locked lesson's payload is allowed (so the UI can preview it), submitting is not.
	state, err := s.lessonState(r.Context(), user.ID, lessonID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	if state == "locked" {
		writeError(w, r, http.StatusForbidden, "lesson_locked",
			"this lesson is locked until the earlier lessons in the path are completed", nil)
		return
	}

	var req submitAttemptsRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Mode == "" {
		req.Mode = "practice"
	}
	if len(req.Attempts) > s.cfg.MaxAttemptsPerRequest {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
			"too many answers in one submission", nil)
		return
	}

	// Guard: every submitted exercise must belong to the lesson in the path,
	// otherwise a client could submit answers against an arbitrary exercise.
	specs, err := s.store.ExercisesForGrading(r.Context(), exerciseIDs(req.Attempts))
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	for _, in := range req.Attempts {
		spec, found := specs[in.ExerciseID]
		if !found {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"unknown exercise id in submission", nil)
			return
		}
		if spec.LessonID != lessonID {
			writeError(w, r, http.StatusForbidden, "forbidden",
				"an exercise in this submission does not belong to the lesson", nil)
			return
		}
	}

	summary, err := s.attempts.Submit(r.Context(), user.ID, req.Mode, req.Attempts)
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

	// Exam mode withholds solutions: the learner should not learn the answer
	// mid-paper. The caller decides by mode, and the server enforces it here.
	if req.Mode == "exam" {
		for i := range summary.Results {
			summary.Results[i].Solution = nil
			summary.Results[i].Feedback = nil
			summary.Results[i].Explanation = ""
			summary.Results[i].Score = 0
		}
	}
	writeJSON(w, http.StatusOK, summary)
}

func exerciseIDs(inputs []attempts.Input) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(inputs))
	for _, in := range inputs {
		out = append(out, in.ExerciseID)
	}
	return out
}

// --- progress --------------------------------------------------------------

func (s *Server) handleProgressOverview(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	overview, err := s.buildOverview(r.Context(), user)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

// handleProgressRefresh recomputes every derived value from the attempt log and
// returns the fresh overview. This exists so "the numbers look stale" always has
// a user-triggerable answer.
func (s *Server) handleProgressRefresh(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	if err := s.store.RecomputeUserProgress(r.Context(), user.ID); err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	overview, err := s.buildOverview(r.Context(), user)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, overview)
}

func (s *Server) buildOverview(ctx context.Context, user *store.User) (*store.ProgressOverview, error) {
	streak, err := s.store.StreakStateFor(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	skills, err := s.store.SkillMasteryFor(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	totalAttempts, totalCorrect, lessonsDone, totalXP, err := s.store.Totals(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	due, err := s.store.CountDue(ctx, user.ID)
	if err != nil {
		return nil, err
	}
	activity, err := store.ActivityDays(ctx, s.store.Pool, user.ID, 30)
	if err != nil {
		return nil, err
	}

	ov := &store.ProgressOverview{
		Streak:        *streak,
		Skills:        skills,
		TotalAttempts: totalAttempts,
		TotalCorrect:  totalCorrect,
		LessonsDone:   lessonsDone,
		TotalXP:       totalXP,
		DueReviews:    due,
		Activity:      activity,
	}
	if ov.Skills == nil {
		ov.Skills = []store.SkillMastery{}
	}
	if ov.Activity == nil {
		ov.Activity = []store.DayActivity{}
	}
	// A new learner has no chosen curriculum yet, and the dashboard must still
	// offer them the first lesson: falling back to the default version is what
	// stops a fresh account from seeing "Path complete" with nothing to start.
	version := DefaultCurriculumVersion
	if user.TargetCurriculum != nil && *user.TargetCurriculum != "" {
		version = *user.TargetCurriculum
	}
	ov.CurriculumVersion = version
	if id, title, err := s.store.NextLessonAfter(ctx, version, user.ID); err == nil {
		ov.NextLessonID = id
		ov.NextLessonTitle = title
	}
	return ov, nil
}

// --- spaced repetition -----------------------------------------------------

func (s *Server) handleReviewQueue(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	limit := parseLimit(r, 20, 100)
	items, err := s.store.DueReviewItems(r.Context(), user.ID, limit)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	if items == nil {
		items = []store.ReviewItem{}
	}
	due, err := s.store.CountDue(r.Context(), user.ID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	forecast, err := s.store.ReviewForecast(r.Context(), user.ID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "due_total": due, "forecast": forecast})
}

type reviewAnswerRequest struct {
	LexemeID uuid.UUID `json:"lexeme_id"`
	Correct  bool      `json:"correct"`
}

func (s *Server) handleReviewAnswer(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	var req reviewAnswerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.LexemeID == uuid.Nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "lexeme_id is required", nil)
		return
	}
	item, err := s.store.ReviewItemByLexeme(r.Context(), user.ID, req.LexemeID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "that word is not in your review deck", nil)
			return
		}
		s.respondStoreError(w, r, err)
		return
	}
	nextIndex, nextEase, dueAt := progress.ScheduleReview(time.Now(), item.IntervalIndex, item.Ease, req.Correct)
	if err := s.store.ApplyReviewResult(r.Context(), user.ID, req.LexemeID, req.Correct, nextIndex, nextEase, dueAt); err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	due, err := s.store.CountDue(r.Context(), user.ID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"lexeme_id":      req.LexemeID,
		"interval_index": nextIndex,
		"ease":           nextEase,
		"due_at":         dueAt,
		"due_total":      due,
	})
}
