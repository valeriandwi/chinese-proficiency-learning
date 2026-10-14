// Package attempts is the attempt pipeline: it grades a batch of learner answers,
// persists them atomically, and recomputes the learner's derived state.
//
// It is the only writer of the attempts table, which keeps the "attempts are the
// source of truth, everything else is derived" invariant enforceable in one place.
package attempts

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/valeriandwipurnomo/hanpath/internal/exercises"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// MaxBatch bounds one submission. A lesson has ~10 exercises; the limit exists so
// a malformed client cannot stream an unbounded batch into a single transaction.
const MaxBatch = 100

// Service grades and records attempts.
type Service struct {
	store *store.Store
}

func New(s *store.Store) *Service { return &Service{store: s} }

// Input is one submitted answer.
type Input struct {
	ExerciseID uuid.UUID       `json:"exercise_id"`
	ClientRef  string          `json:"client_ref"`
	Response   json.RawMessage `json:"response"`
	DurationMs int             `json:"duration_ms"`
}

// Result is the graded outcome of one answer, including the solution the client
// only learns after submitting.
type Result struct {
	ExerciseID  uuid.UUID       `json:"exercise_id"`
	ClientRef   string          `json:"client_ref"`
	IsCorrect   *bool           `json:"is_correct"`
	Score       float64         `json:"score"`
	Grader      string          `json:"grader"`
	Feedback    map[string]any  `json:"feedback"`
	Solution    json.RawMessage `json:"solution,omitempty"`
	Explanation string          `json:"explanation,omitempty"`
	// Duplicate marks an idempotent replay of an already-recorded attempt.
	Duplicate bool `json:"duplicate"`
	// NeedsReview marks outcomes no mechanical grader can settle (free-form
	// writing, non-exact translations) so the UI can say so instead of lying.
	NeedsReview bool `json:"needs_review"`
}

// LessonOutcome reports how the submission changed a lesson's state.
type LessonOutcome struct {
	LessonID     uuid.UUID `json:"lesson_id"`
	Status       string    `json:"status"`
	CompletedNow bool      `json:"completed_now"`
	XPAwarded    int       `json:"xp_awarded"`
	Correct      int       `json:"correct"`
	Total        int       `json:"total"`
}

// Summary is the whole response to a submission.
type Summary struct {
	Results     []Result        `json:"results"`
	Lessons     []LessonOutcome `json:"lessons"`
	ProgressURL string          `json:"-"`
}

var (
	// ErrEmptyBatch rejects a submission with no answers.
	ErrEmptyBatch = errors.New("attempts: batch is empty")
	// ErrBatchTooLarge rejects a submission beyond MaxBatch.
	ErrBatchTooLarge = fmt.Errorf("attempts: batch exceeds %d answers", MaxBatch)
	// ErrUnknownExercise rejects answers for exercises that do not exist.
	ErrUnknownExercise = errors.New("attempts: unknown exercise id in batch")
	// ErrBadResponse marks a learner payload that does not match its exercise type.
	ErrBadResponse = errors.New("attempts: the submitted answer does not match this exercise")
	// ErrBadMode rejects a mode outside the allowed set.
	ErrBadMode = errors.New("attempts: mode must be one of practice, review, exam, placement")
)

// ValidModes mirrors the attempts.mode CHECK constraint.
var ValidModes = map[string]bool{"practice": true, "review": true, "exam": true, "placement": true}

// Submit grades and records a batch, then recomputes derived state in the same
// transaction so a submission can never leave progress stale.
func (s *Service) Submit(ctx context.Context, userID uuid.UUID, mode string, inputs []Input) (*Summary, error) {
	if len(inputs) == 0 {
		return nil, ErrEmptyBatch
	}
	if len(inputs) > MaxBatch {
		return nil, ErrBatchTooLarge
	}
	if !ValidModes[mode] {
		return nil, ErrBadMode
	}

	ids := make([]uuid.UUID, 0, len(inputs))
	seen := map[uuid.UUID]bool{}
	for _, in := range inputs {
		if in.ExerciseID == uuid.Nil {
			return nil, fmt.Errorf("attempts: exercise_id is required")
		}
		if !seen[in.ExerciseID] {
			seen[in.ExerciseID] = true
			ids = append(ids, in.ExerciseID)
		}
	}

	specs, err := s.store.ExercisesForGrading(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if _, ok := specs[id]; !ok {
			return nil, fmt.Errorf("%w: %s", ErrUnknownExercise, id)
		}
	}

	summary := &Summary{Results: make([]Result, 0, len(inputs))}
	rows := make([]store.AttemptInsert, 0, len(inputs))
	lessonOf := map[uuid.UUID]uuid.UUID{} // exercise -> lesson

	for _, in := range inputs {
		spec := specs[in.ExerciseID]
		lessonOf[in.ExerciseID] = spec.LessonID

		graded, err := exercises.Grade(spec.Type, spec.Prompt, spec.AnswerKey, in.Response)
		if err != nil {
			// A payload that does not match its exercise type is the caller's fault.
			// Classifying it here keeps it out of the 500 path, where it looked like
			// a server fault and hid a broken writing drill for as long as it did.
			if errors.Is(err, exercises.ErrBadResponse) {
				return nil, fmt.Errorf("%w: exercise %s (%s): %v", ErrBadResponse, spec.ID, spec.Type, err)
			}
			return nil, fmt.Errorf("attempts: grading exercise %s (%s): %w", spec.ID, spec.Type, err)
		}
		fb, err := json.Marshal(graded.Feedback)
		if err != nil {
			return nil, fmt.Errorf("attempts: encoding feedback: %w", err)
		}
		score := graded.Score
		dur := in.DurationMs
		if dur < 0 {
			dur = 0
		}

		rows = append(rows, store.AttemptInsert{
			UserID:     userID,
			ExerciseID: in.ExerciseID,
			Mode:       mode,
			ClientRef:  in.ClientRef,
			Grader:     graded.Grader,
			Response:   in.Response,
			Feedback:   fb,
			IsCorrect:  graded.IsCorrect,
			Score:      &score,
			DurationMs: &dur,
		})

		// In exam mode the solution is withheld until the paper is submitted;
		// the caller enforces that by passing mode=exam and ignoring solutions.
		res := Result{
			ExerciseID: in.ExerciseID,
			ClientRef:  in.ClientRef,
			IsCorrect:  graded.IsCorrect,
			Score:      graded.Score,
			Grader:     graded.Grader,
			Feedback:   graded.Feedback,
			Solution:   spec.AnswerKey,
		}
		if spec.Explanation != nil {
			res.Explanation = *spec.Explanation
		}
		if nr, ok := graded.Feedback["needs_review"].(bool); ok && nr {
			res.NeedsReview = true
		}
		summary.Results = append(summary.Results, res)
	}

	// Persist and recompute in one transaction, then report how each affected
	// lesson changed. Before/after is read inside the transaction so the numbers
	// cannot drift between the insert and the read.
	err = s.store.Tx(ctx, func(tx pgx.Tx) error {
		before := map[uuid.UUID]string{}
		lessonIDs := map[uuid.UUID]bool{}
		for _, l := range lessonOf {
			lessonIDs[l] = true
		}
		for id := range lessonIDs {
			var status *string
			if err := tx.QueryRow(ctx, `SELECT status FROM lesson_progress WHERE user_id = $1::uuid AND lesson_id = $2::uuid`, userID, id).Scan(&status); err != nil && err != pgx.ErrNoRows {
				return err
			}
			if status != nil {
				before[id] = *status
			}
		}

		for i, row := range rows {
			id, inserted, err := store.InsertAttemptTx(ctx, tx, row)
			if err != nil {
				return err
			}
			if !inserted {
				summary.Results[i].Duplicate = true
				continue
			}
			_ = id
		}

		if err := store.RecomputeProgress(ctx, tx, userID); err != nil {
			return err
		}

		// Report per-lesson outcomes for the lessons this batch touched.
		for lessonID := range lessonIDs {
			var status string
			var xp int
			var completedAt *string
			if err := tx.QueryRow(ctx, `
				SELECT coalesce(status, 'in_progress'), coalesce(xp_awarded, 0), completed_at::text
				FROM lesson_progress WHERE user_id = $1::uuid AND lesson_id = $2::uuid`,
				userID, lessonID).Scan(&status, &xp, &completedAt); err != nil {
				if err == pgx.ErrNoRows {
					continue
				}
				return err
			}
			var attemptsCount, correctCount, total int
			if err := tx.QueryRow(ctx, `
				SELECT count(*)::int, count(*) FILTER (WHERE is_correct)::int
				FROM attempts a WHERE a.user_id = $1::uuid AND a.exercise_id IN (SELECT id FROM exercises WHERE lesson_id = $2::uuid)`,
				userID, lessonID).Scan(&attemptsCount, &correctCount); err != nil {
				return err
			}
			if err := tx.QueryRow(ctx, `SELECT count(*)::int FROM exercises WHERE lesson_id = $1::uuid`, lessonID).Scan(&total); err != nil {
				return err
			}
			summary.Lessons = append(summary.Lessons, LessonOutcome{
				LessonID:     lessonID,
				Status:       status,
				CompletedNow: status == "completed" && before[lessonID] != "completed",
				XPAwarded:    xp,
				Correct:      correctCount,
				Total:        total,
			})
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return summary, nil
}
