package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// SpeakingScoreInput is a score to persist. It carries primitives rather than
// speech.Score so this package does not depend on the provider layer: the handler
// translates one into the other, and the database stays ignorant of vendors.
type SpeakingScoreInput struct {
	ID           uuid.UUID
	UserID       uuid.UUID
	ExerciseID   uuid.UUID
	Provider     string
	Model        string
	Overall      float64
	Accuracy     float64
	Fluency      float64
	Completeness float64
	Prosody      float64
	// Words is the per-word detail, already marshalled JSON.
	Words      []byte
	DurationMs int
}

// SpeakingScoreRow is a stored score as read back for grading.
type SpeakingScoreRow struct {
	ID           uuid.UUID
	ExerciseID   uuid.UUID
	Provider     string
	Model        string
	Overall      float64
	Accuracy     float64
	Fluency      float64
	Completeness float64
	Prosody      float64
	Words        []byte
	CreatedAt    time.Time
}

// CountSpeakingScoresSince supports the daily cap: scoring costs money per call, so
// a single account cannot spend an unbounded amount of it.
func (s *Store) CountSpeakingScoresSince(ctx context.Context, userID uuid.UUID, since time.Time) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx,
		`SELECT count(*)::int FROM speaking_scores WHERE user_id = $1::uuid AND created_at >= $2`,
		userID, since).Scan(&n)
	if err != nil {
		return 0, mapErr(err)
	}
	return n, nil
}

// InsertSpeakingScore stores a score produced by the configured provider.
func (s *Store) InsertSpeakingScore(ctx context.Context, in SpeakingScoreInput) error {
	if len(in.Words) == 0 {
		in.Words = []byte("[]")
	}
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO speaking_scores (id, user_id, exercise_id, provider, model,
		                             overall, accuracy, fluency, completeness, prosody, words, duration_ms)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`,
		in.ID, in.UserID, in.ExerciseID, in.Provider, in.Model,
		in.Overall, in.Accuracy, in.Fluency, in.Completeness, in.Prosody, in.Words, in.DurationMs)
	return mapErr(err)
}

// SpeakingScoreFor reads a score, but only for the learner who produced it and only
// for the exercise it belongs to.
//
// The ownership predicate is in the SQL rather than checked afterwards: a score is
// another learner's assessment of their own voice, and a lookup that can return it is
// a lookup that can leak it. A miss is ErrNotFound, which the caller reports as a
// stale recording rather than as a missing row.
func (s *Store) SpeakingScoreFor(ctx context.Context, id, userID, exerciseID uuid.UUID) (*SpeakingScoreRow, error) {
	var row SpeakingScoreRow
	err := s.Pool.QueryRow(ctx, `
		SELECT id, exercise_id, provider, model,
		       overall::float8, coalesce(accuracy, 0)::float8, coalesce(fluency, 0)::float8,
		       coalesce(completeness, 0)::float8, coalesce(prosody, 0)::float8, words, created_at
		FROM speaking_scores
		WHERE id = $1::uuid AND user_id = $2::uuid AND exercise_id = $3::uuid`,
		id, userID, exerciseID).Scan(
		&row.ID, &row.ExerciseID, &row.Provider, &row.Model,
		&row.Overall, &row.Accuracy, &row.Fluency, &row.Completeness, &row.Prosody, &row.Words, &row.CreatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &row, nil
}

// AttachSpeakingScore links a score to the attempt that used it, so a score can be
// traced from the attempt log. It is best-effort bookkeeping: a failure here must not
// fail the submission, which is why the caller logs rather than returns.
func (s *Store) AttachSpeakingScore(ctx context.Context, scoreID, attemptID uuid.UUID) error {
	_, err := s.Pool.Exec(ctx,
		`UPDATE speaking_scores SET attempt_id = $2 WHERE id = $1 AND attempt_id IS NULL`,
		scoreID, attemptID)
	return mapErr(err)
}

// SpeakingExerciseFor loads what scoring a recording needs: the exercise's type, its
// prompt (for the reference text) and the lesson it belongs to.
//
// The type matters: a client must not be able to send audio against a multiple-choice
// question and have it scored, which would be both wrong and a way to burn the
// deployment's scoring budget.
func (s *Store) SpeakingExerciseFor(ctx context.Context, exerciseID uuid.UUID) (kind string, prompt []byte, lessonID uuid.UUID, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT e.type, e.prompt, e.lesson_id
		FROM exercises e
		WHERE e.id = $1::uuid`, exerciseID).Scan(&kind, &prompt, &lessonID)
	if err != nil {
		return "", nil, uuid.Nil, mapErr(err)
	}
	return kind, prompt, lessonID, nil
}
