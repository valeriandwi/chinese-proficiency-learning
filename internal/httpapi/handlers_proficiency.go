package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/attempts"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// Proficiency tests: a scored paper that opens a level.
//
// Placement recommends and says so; this decides. A learner who already knows HSK 3
// sits twenty questions drawn from level 3 and, at the pass mark, level 3 opens — which
// matters because the server refuses a locked lesson's submissions, so a recommendation
// alone can never let anyone past the gate.
//
// Three properties make the pass a fact rather than a claim:
//
//   - The paper is DRAWN AND STORED before it is sat. The submission is checked against
//     the drawn set, so a client cannot answer the twenty questions it likes best and
//     pass a paper it was never given — every drawn question must be answered, and
//     nothing outside the paper is graded.
//   - Only mechanically gradable types are drawn (meaning, listening, tone, dictation).
//     A rubric item or an unassisted speaking prompt could not decide whether a level
//     opens, so neither is in a paper.
//   - A pass records an UNLOCK, never completions. No lesson is marked done, no XP is
//     awarded and no mastery moves; the paper's own attempts are recorded with mode
//     "proficiency" and excluded from every derived value, because a paper samples
//     lessons the learner has not reached.
const proficiencyPassMark = 0.80

func proficiencyPercent(score float64) int { return int(score*100 + 0.5) }

// handleProficiencyCards lists every level with what a learner needs to decide which
// test to sit: how many questions, whether it is already open, and their best paper.
func (s *Server) handleProficiencyCards(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	version := r.URL.Query().Get("version")
	if version == "" {
		version = DefaultCurriculumVersion
	}
	cards, err := s.store.ProficiencyCards(r.Context(), user.ID, version)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"cards":          cards,
		"pass_threshold": proficiencyPassMark,
		"paper_size":     store.ProficiencyPaperSize,
	})
}

// handleProficiencyPaper draws a paper, records it, and hands the questions over.
func (s *Server) handleProficiencyPaper(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	levelNumber, ok := parseLevelParam(w, r)
	if !ok {
		return
	}

	level, err := s.store.LevelByNumber(r.Context(), DefaultCurriculumVersion, levelNumber)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	seed := uuid.NewString()
	questions, err := s.store.ProficiencyPaper(r.Context(), level.ID, seed)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	if len(questions) == 0 {
		writeError(w, r, http.StatusNotFound, "not_found",
			"this level has no gradable questions to build a paper from", nil)
		return
	}
	ids := make([]uuid.UUID, 0, len(questions))
	for _, q := range questions {
		ids = append(ids, q.ID)
	}
	paperID, err := s.store.DrawPaper(r.Context(), user.ID, level.ID, seed, ids)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	// The prompts are the learner's; the answer keys stay here. Audio is attached the
	// same way a lesson payload gets it.
	s.attachAudioURLs(questions)

	writeJSON(w, http.StatusOK, map[string]any{
		"paper_id":       paperID,
		"level":          map[string]any{"number": level.Number, "title": level.Title},
		"questions":      questions,
		"pass_threshold": proficiencyPassMark,
	})
}

type proficiencyAnswer struct {
	ExerciseID uuid.UUID `json:"exercise_id"`
	ClientRef  string    `json:"client_ref"`
	// Response is the same shape a lesson submission uses, because the questions are
	// ordinary drills: {"option_id": …} or {"text": …}.
	Response map[string]any `json:"response"`
}

type proficiencySubmitRequest struct {
	PaperID uuid.UUID           `json:"paper_id"`
	Answers []proficiencyAnswer `json:"answers"`
}

// handleProficiencySubmit grades a drawn paper, decides the pass, and opens the level.
func (s *Server) handleProficiencySubmit(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	levelNumber, ok := parseLevelParam(w, r)
	if !ok {
		return
	}

	// Its own budget, so a paper cannot lock a learner out of the lessons it opens.
	if ok, _ := s.writeLimiter.Allow("proficiency:" + user.ID.String()); !ok {
		w.Header().Set("Retry-After", "5")
		writeError(w, r, http.StatusTooManyRequests, "rate_limited",
			"too many submissions in a short window, retry shortly", nil)
		return
	}

	var req proficiencySubmitRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.PaperID == uuid.Nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "paper_id is required", nil)
		return
	}
	if len(req.Answers) == 0 || len(req.Answers) > s.cfg.MaxAttemptsPerRequest {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
			"a paper needs between 1 and the configured maximum answers", nil)
		return
	}

	paper, err := s.store.PaperByID(r.Context(), user.ID, req.PaperID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			writeError(w, r, http.StatusNotFound, "not_found", "no such paper for this learner", nil)
			return
		}
		s.respondStoreError(w, r, err)
		return
	}
	if paper.SubmittedAt != nil {
		writeError(w, r, http.StatusConflict, "already_submitted", "this paper has already been sat", nil)
		return
	}
	level, err := s.store.LevelByNumber(r.Context(), DefaultCurriculumVersion, levelNumber)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	if level.ID != paper.LevelID {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
			"this paper is not for that level", nil)
		return
	}

	// Every drawn question, and only drawn questions. Answering a subset is how a
	// learner would otherwise pass a paper they were never given.
	drawn := make(map[uuid.UUID]bool, len(paper.ExerciseIDs))
	for _, id := range paper.ExerciseIDs {
		drawn[id] = true
	}
	if len(req.Answers) != len(paper.ExerciseIDs) {
		writeError(w, r, http.StatusUnprocessableEntity, "incomplete_paper",
			"answer every question in the paper before submitting it", nil)
		return
	}
	answered := make(map[uuid.UUID]bool, len(req.Answers))
	inputs := make([]attempts.Input, 0, len(req.Answers))
	for _, a := range req.Answers {
		if !drawn[a.ExerciseID] {
			writeError(w, r, http.StatusUnprocessableEntity, "not_in_paper",
				"an answer in this submission is not part of the paper", nil)
			return
		}
		if answered[a.ExerciseID] {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"the same question was answered twice", nil)
			return
		}
		answered[a.ExerciseID] = true
		response, err := json.Marshal(a.Response)
		if err != nil {
			writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
				"could not read one of the answers", nil)
			return
		}
		inputs = append(inputs, attempts.Input{ExerciseID: a.ExerciseID, ClientRef: a.ClientRef, Response: response})
	}

	// Off-path: a paper samples lessons the learner has not reached, so grading it must
	// not move lesson status, XP, mastery, the daily rollup or the streak.
	summary, err := s.attempts.SubmitOffPath(r.Context(), user.ID, "proficiency", inputs)
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

	graded, correct := 0, 0
	for _, res := range summary.Results {
		if res.IsCorrect == nil {
			continue
		}
		graded++
		if *res.IsCorrect {
			correct++
		}
	}
	if graded == 0 {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
			"nothing in this paper could be graded", nil)
		return
	}
	score := float64(correct) / float64(graded)
	passed := score >= proficiencyPassMark

	sat, err := s.store.SubmitPaper(r.Context(), paper.ID, score, passed, graded, correct)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	if !sat {
		writeError(w, r, http.StatusConflict, "already_submitted", "this paper has already been sat", nil)
		return
	}

	unlockedNow := false
	if passed {
		// The first pass is the one that opens the level; a retake cannot produce a
		// second unlock, and unlockNow tells the page which one this was.
		unlockedNow, err = s.store.UnlockLevel(r.Context(), user.ID, level.ID, paper.ID, score)
		if err != nil {
			s.respondStoreError(w, r, err)
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"results": summary.Results,
		"paper": map[string]any{
			"id":            paper.ID,
			"level":         level.Number,
			"score":         score,
			"percent":       proficiencyPercent(score),
			"passed":        passed,
			"graded_count":  graded,
			"correct_count": correct,
			"pass_mark":     proficiencyPassMark,
		},
		"unlocked_now": unlockedNow,
	})
}

// parseLevelParam reads the {level} path parameter, rejecting anything outside the
// syllabus rather than letting a bad number reach the store. It writes its own error,
// like parseUUIDParam, so every caller reports a bad path the same way.
func parseLevelParam(w http.ResponseWriter, r *http.Request) (int, bool) {
	raw := chi.URLParam(r, "level")
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > 9 {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed",
			"level must be a number between 1 and 9", nil)
		return 0, false
	}
	return n, true
}
