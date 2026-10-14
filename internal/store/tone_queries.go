package store

import (
	"context"

	"github.com/google/uuid"
)

// ToneRuns is the SQL predicate that identifies a tone drill by its content key.
//
// Tone drills are generated with keys ending in "-tone<n>" (the tone question for a
// word) and "-pair<n>" (the discrimination drill for two words that differ only in
// tone). They are ordinary `tone_mcq` and `listening_mcq` rows, so the key is what
// distinguishes them from the meaning and dictation drills around them — no new column
// and no new type for the pager to keep in step.
const toneRuns = `e.content_key ~ '-(tone|pair)[0-9]+$' AND e.type IN ('tone_mcq', 'listening_mcq')`

// ToneDrill is one tone question as the learner sees it: the prompt without its answer
// key, plus the level it belongs to so the page can say where a drill comes from.
type ToneDrill struct {
	ExerciseForLearner
	Level int `json:"level"`
}

// ToneLevelStat is what exists at a level and what the learner has done with it.
type ToneLevelStat struct {
	Level    int `json:"level"`
	Drills   int `json:"drills"`
	Answered int `json:"answered"`
	Correct  int `json:"correct"`
}

// ToneQueue returns tone drills for one level (0 means every level).
//
// Deliberately NOT gated by the path: a tone question stands on its own, and the whole
// point of this surface is to practise tone without walking a level first. It IS
// filtered by level, because a learner answering questions about words they have never
// met learns nothing — the caller defaults to the learner's own level and offers the
// rest as a deliberate choice.
//
// The order is a hash of the content key and the learner's id: stable across reloads,
// so a round does not reshuffle under the learner mid-session, and different per
// learner, so two learners sitting side by side do not recite the same sequence.
func (s *Store) ToneQueue(ctx context.Context, userID uuid.UUID, level, limit int) ([]ToneDrill, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id, e.exercise_number, e.type, e.skill, e.difficulty, e.prompt, l.level_number
		FROM exercises e
		JOIN lessons le ON le.id = e.lesson_id
		JOIN units u ON u.id = le.unit_id
		JOIN levels l ON l.id = u.level_id
		WHERE `+toneRuns+`
		  AND ($2 = 0 OR l.level_number = $2)
		ORDER BY md5(e.content_key || $1::text)
		LIMIT $3`, userID, level, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]ToneDrill, 0, limit)
	for rows.Next() {
		var d ToneDrill
		if err := rows.Scan(&d.ID, &d.Number, &d.Type, &d.Skill, &d.Difficulty, &d.Prompt, &d.Level); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, d)
	}
	return out, mapErr(rows.Err())
}

// ToneStats returns, for every level, how many tone drills exist and how the learner
// has done on them. Only tone-mode attempts count: this is the tone record, not the
// path's progress.
func (s *Store) ToneStats(ctx context.Context, userID uuid.UUID) ([]ToneLevelStat, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT l.level_number,
		       count(DISTINCT e.id)::int                                          AS drills,
		       count(a.id) FILTER (WHERE a.mode = 'tone')::int                    AS answered,
		       count(a.id) FILTER (WHERE a.mode = 'tone' AND a.is_correct)::int   AS correct
		FROM exercises e
		JOIN lessons le ON le.id = e.lesson_id
		JOIN units u ON u.id = le.unit_id
		JOIN levels l ON l.id = u.level_id
		LEFT JOIN attempts a ON a.exercise_id = e.id AND a.user_id = $1::uuid
		WHERE `+toneRuns+`
		GROUP BY l.level_number
		ORDER BY l.level_number`, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]ToneLevelStat, 0, 5)
	for rows.Next() {
		var st ToneLevelStat
		if err := rows.Scan(&st.Level, &st.Drills, &st.Answered, &st.Correct); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, st)
	}
	return out, mapErr(rows.Err())
}

// ToneDrillIDs reports which of the given exercises this store considers tone drills.
//
// The answer endpoint asks this before grading, so the two surfaces cannot drift: an
// endpoint that decided for itself what a tone drill is would eventually grade something
// the queue would never have served, and the predicate above would be duplicated in a
// second language.
func (s *Store) ToneDrillIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]bool{}, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT e.id FROM exercises e WHERE e.id = ANY($1) AND `+toneRuns, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]bool, len(ids))
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, mapErr(err)
		}
		out[id] = true
	}
	return out, mapErr(rows.Err())
}
