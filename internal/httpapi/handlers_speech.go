package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/exercises"
	"github.com/valeriandwipurnomo/hanpath/internal/speech"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// scoringOverhead is what multipart framing adds on top of the audio itself, so a
// recording right at the limit is not rejected for the boundary markers around it.
const scoringOverhead = 64 << 10

// handleScoreRecording assesses a spoken answer and stores the score.
//
// This is the only endpoint that accepts a learner's voice, and it is built around
// three promises the rest of the system depends on:
//
//  1. The audio is never stored. It is read into memory, handed to the scorer, and
//     dropped before the response is written — there is no audio column, no temp
//     file, and nothing to purge. The UI tells the learner this before they record.
//  2. A client can name a recording it owns, but it can never send us a score. The
//     numbers here come from the provider and are read back by grading from the
//     database, so a tampered payload cannot award itself a pass.
//  3. Scoring is the one learner action that costs money per call, so it is rate
//     limited per learner and capped per day, and a failure is reported as a
//     temporary problem rather than as a wrong answer.
func (s *Server) handleScoreRecording(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeError(w, r, http.StatusUnauthorized, "unauthenticated", "sign in first", nil)
		return
	}
	if s.speech == nil {
		// Not an error: a deployment without a pronunciation provider is a supported
		// configuration, and the client falls back to self-assessment.
		writeError(w, r, http.StatusServiceUnavailable, "pronunciation_unavailable",
			"this deployment has no pronunciation provider configured", nil)
		return
	}
	if ok, _ := s.scoringLimiter.Allow("speech:" + user.ID.String()); !ok {
		w.Header().Set("Retry-After", "10")
		writeError(w, r, http.StatusTooManyRequests, "rate_limited",
			"too many recordings scored in a short window, retry shortly", nil)
		return
	}

	maxBytes := int64(s.cfg.SpeechMaxUploadBytes)
	if maxBytes <= 0 {
		maxBytes = 1 << 20
	}

	// The cap is enforced on the wire, before the body is parsed or buffered, so an
	// oversized upload cannot make the server hold it all.
	r.Body = http.MaxBytesReader(w, r.Body, maxBytes+scoringOverhead)
	if err := r.ParseMultipartForm(maxBytes); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, r, http.StatusRequestEntityTooLarge, "recording_too_large",
				"this recording is larger than the server accepts", nil)
			return
		}
		writeError(w, r, http.StatusBadRequest, "invalid_multipart",
			"send the recording as multipart/form-data with an exercise_id field and an audio field", nil)
		return
	}
	defer func() {
		// Releasing the parsed parts also releases the temp files the standard library
		// may have spilled to disk for large parts. Nothing of the learner's voice
		// should outlive the request.
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()

	exerciseID, err := uuid.Parse(strings.TrimSpace(r.FormValue("exercise_id")))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_exercise_id", "exercise_id must be a UUID", nil)
		return
	}

	kind, promptJSON, _, err := s.store.SpeakingExerciseFor(r.Context(), exerciseID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	if !isSpeakingType(kind) {
		writeError(w, r, http.StatusUnprocessableEntity, "not_a_speaking_drill",
			"only speaking drills can be scored", nil)
		return
	}

	reference, ok := referenceFor(kind, promptJSON)
	if !ok {
		// speak_answer asks an open question: there is no single utterance to compare
		// against, and scoring it against the question would mark a correct answer
		// wrong. The drill stays self-assessed rather than being scored dishonestly.
		writeError(w, r, http.StatusUnprocessableEntity, "no_reference_text",
			"this drill has no single reference utterance to score against, so it stays self-assessed", nil)
		return
	}

	file, header, err := r.FormFile("audio")
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "missing_audio", "the audio part is required", nil)
		return
	}
	defer file.Close()

	audio, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "unreadable_audio", "the recording could not be read", nil)
		return
	}
	if int64(len(audio)) > maxBytes {
		writeError(w, r, http.StatusRequestEntityTooLarge, "recording_too_large",
			"this recording is larger than the server accepts", nil)
		return
	}

	// The daily cap is checked before the provider is called, because the cap exists
	// to bound spending. It is a count of scores, so a learner who hits it still gets
	// the tier-1 drill.
	if cap := s.cfg.SpeechDailyCap; cap > 0 {
		used, err := s.store.CountSpeakingScoresSince(r.Context(), user.ID, scoringWindowStart())
		if err != nil {
			s.respondStoreError(w, r, err)
			return
		}
		if used >= cap {
			writeError(w, r, http.StatusTooManyRequests, "daily_limit_reached",
				"the daily limit for scored recordings has been reached; the drill still works without scoring", nil)
			return
		}
	}

	score, err := s.speech.Score(r.Context(), speech.Request{
		Reference:   reference,
		Audio:       audio,
		ContentType: header.Header.Get("Content-Type"),
	})
	if err != nil {
		s.respondScoringError(w, r, err)
		return
	}

	words, err := json.Marshal(score.Words)
	if err != nil {
		s.log.ErrorContext(r.Context(), "encoding word scores", "error", err)
		words = []byte("[]")
	}

	id := uuid.New()
	if err := s.store.InsertSpeakingScore(r.Context(), storeScore(id, user.ID, exerciseID, score, words, len(audio))); err != nil {
		s.respondStoreError(w, r, err)
		return
	}

	// The audio goes out of scope here. Nothing about it is written anywhere.
	writeJSON(w, http.StatusOK, map[string]any{
		"id":           id,
		"provider":     score.Provider,
		"overall":      score.Overall,
		"accuracy":     score.Accuracy,
		"fluency":      score.Fluency,
		"completeness": score.Completeness,
		"prosody":      score.Prosody,
		"words":        score.Words,
		"passing_band": speech.PassingBand,
		"reference":    reference,
	})
}

// respondScoringError separates "the learner's recording was unusable" from "our side
// is broken", because the first is feedback and the second is an outage.
func (s *Server) respondScoringError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, speech.ErrEmptyAudio), errors.Is(err, speech.ErrUnsupportedAudio):
		writeError(w, r, http.StatusUnprocessableEntity, "invalid_audio",
			"that recording could not be read as audio — try recording again", nil)
	case errors.Is(err, speech.ErrNoSpeech):
		writeError(w, r, http.StatusUnprocessableEntity, "no_speech",
			"no speech was recognised in that recording", nil)
	case errors.Is(err, speech.ErrProviderBusy):
		w.Header().Set("Retry-After", "5")
		writeError(w, r, http.StatusServiceUnavailable, "scoring_busy",
			"the pronunciation service is busy; try again shortly", nil)
	case errors.Is(err, speech.ErrProviderAuth):
		// An operator problem: say nothing about credentials to the learner, log the
		// detail, and let them continue with an unscored drill.
		s.log.ErrorContext(r.Context(), "pronunciation provider rejected our credentials", "error", err)
		writeError(w, r, http.StatusBadGateway, "scoring_unavailable",
			"pronunciation scoring is temporarily unavailable", nil)
	default:
		s.log.ErrorContext(r.Context(), "scoring a recording failed", "error", err)
		writeError(w, r, http.StatusBadGateway, "scoring_failed",
			"pronunciation scoring is temporarily unavailable", nil)
	}
}

// isSpeakingType keeps a client from having audio scored against a multiple-choice
// question — wrong, and a way to spend the deployment's scoring budget.
func isSpeakingType(kind string) bool {
	for _, t := range exercises.SpeakingTypes() {
		if t == kind {
			return true
		}
	}
	return false
}

// referenceFor extracts the utterance a drill asks the learner to say.
func referenceFor(kind string, promptJSON []byte) (string, bool) {
	var fields map[string]any
	if err := json.Unmarshal(promptJSON, &fields); err != nil {
		return "", false
	}
	switch kind {
	case "speak_repeat", "read_aloud":
		text, _ := fields["text"].(string)
		if text = strings.TrimSpace(text); text != "" {
			return text, true
		}
		// read_aloud in some content carries only the question field; accept it so a
		// drill with a script is still scoreable.
		if question, _ := fields["question"].(string); strings.TrimSpace(question) != "" {
			return strings.TrimSpace(question), true
		}
		return "", false
	default:
		return "", false
	}
}

// scoringWindowStart bounds the daily cap as a rolling window rather than a calendar
// day: no timezone to get wrong, and no learner who records at 23:55 losing their
// allowance at midnight.
func scoringWindowStart() time.Time { return time.Now().Add(-24 * time.Hour) }

// storeScore translates a provider's verdict into a row. It lives here, not in the
// store package, so the database never has to know what a provider is.
func storeScore(id, userID, exerciseID uuid.UUID, score speech.Score, words []byte, audioBytes int) store.SpeakingScoreInput {
	return store.SpeakingScoreInput{
		ID:           id,
		UserID:       userID,
		ExerciseID:   exerciseID,
		Provider:     score.Provider,
		Model:        score.Model,
		Overall:      score.Overall,
		Accuracy:     score.Accuracy,
		Fluency:      score.Fluency,
		Completeness: score.Completeness,
		Prosody:      score.Prosody,
		Words:        words,
		DurationMs:   0,
	}
}
