package store

import (
	"context"

	"github.com/google/uuid"
)

// ReviewItemByLexeme loads one card in a learner's deck.
func (s *Store) ReviewItemByLexeme(ctx context.Context, userID, lexemeID uuid.UUID) (*ReviewItem, error) {
	var it ReviewItem
	err := s.Pool.QueryRow(ctx, `
		SELECT r.id, r.lexeme_id, x.simplified, x.traditional, x.pinyin, x.senses, x.audio_key,
		       r.interval_index, r.ease::float8, r.reps, r.lapses, r.due_at
		FROM review_items r
		JOIN lexemes x ON x.id = r.lexeme_id
		WHERE r.user_id = $1::uuid AND r.lexeme_id = $2::uuid`, userID, lexemeID).
		Scan(&it.ID, &it.LexemeID, &it.Simplified, &it.Traditional, &it.Pinyin, &it.Senses,
			&it.AudioKey, &it.IntervalIndex, &it.Ease, &it.Reps, &it.Lapses, &it.DueAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &it, nil
}
