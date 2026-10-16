package store

import (
	"context"

	"github.com/google/uuid"
)

// Proficiency tests: the scored way to open a level.
//
// A paper is sampled from one level's existing drills — there is no separate question
// bank to keep in step with the curriculum — and passing it records a level unlock. The
// unlock is deliberately NOT completion: no lesson is marked done and no XP is awarded,
// so the path stays an honest record of what was learned.

// proficiencyTypes are the drill types a paper is drawn from, with how many of each a
// full paper wants. All four are graded mechanically, which is what lets a pass/fail
// decision be a fact rather than a judgement: no rubric items and nothing that needs a
// pronunciation provider, because those cannot decide whether a level is open.
var proficiencyTypes = []struct {
	kind string
	want int
}{
	{"mcq", 10},
	{"listening_mcq", 5},
	{"tone_mcq", 3},
	{"listening_dictation", 2},
}

// ProficiencyPaperSize is how many questions a full paper asks.
const ProficiencyPaperSize = 20

// maxPerUnitType keeps a paper from clustering: at most two questions of one kind come
// from one unit while the quotas can be met that way, so a paper touches several units.
const maxPerUnitType = 2

// proficiencyPoolDepth is how many candidates per (unit, kind) the query fetches. It must
// exceed the spread cap, because a level concentrates some kinds in one unit — every one
// of level 1's 46 tone drills lives in its tone unit — and a pool capped at the spread
// would leave the paper short. The selection below enforces the spread; the fetch must
// not.
const proficiencyPoolDepth = 8

// LevelRef identifies a level inside a curriculum.
type LevelRef struct {
	ID          uuid.UUID
	Number      int
	Title       string
	Stage       string
	Description string
}

// LevelByNumber finds a level of a curriculum version. Every proficiency endpoint is
// addressed by level number, because that is what a learner knows ("HSK 3").
func (s *Store) LevelByNumber(ctx context.Context, version string, number int) (LevelRef, error) {
	var out LevelRef
	err := s.Pool.QueryRow(ctx, `
		SELECT l.id, l.level_number, l.title, l.stage, l.description
		FROM levels l
		JOIN curricula c ON c.id = l.curriculum_id
		WHERE c.version = $1 AND l.level_number = $2`, version, number).
		Scan(&out.ID, &out.Number, &out.Title, &out.Stage, &out.Description)
	return out, mapErr(err)
}

// ProficiencyPaper samples a paper from one level.
//
// The order is a hash of the content key and a per-request seed, so every attempt draws
// a different paper (a retake is not a memorised set) while a single paper is stable
// once drawn. Questions are spread across the level's units per type before the paper is
// assembled, and the final order interleaves the kinds, because twenty meaning questions
// in a row measures something other than the level.
func (s *Store) ProficiencyPaper(ctx context.Context, levelID uuid.UUID, seed string) ([]ExerciseForLearner, error) {
	rows, err := s.Pool.Query(ctx, `
		WITH ranked AS (
			SELECT e.id, e.exercise_number, e.type, e.skill, e.difficulty, e.prompt,
			       u.id AS unit_id, u.unit_number,
			       row_number() OVER (PARTITION BY u.id, e.type ORDER BY md5(e.content_key || $2::text)) AS rn
			FROM exercises e
			JOIN lessons le ON le.id = e.lesson_id
			JOIN units u ON u.id = le.unit_id
			WHERE u.level_id = $1
			  AND e.type IN ('mcq', 'listening_mcq', 'tone_mcq', 'listening_dictation')
		)
		SELECT id, exercise_number, type, skill, difficulty, prompt, unit_id, rn
		FROM ranked
		WHERE rn <= $3
		ORDER BY rn, unit_number, md5(id::text || $2::text)`,
		levelID, seed, proficiencyPoolDepth)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	type candidate struct {
		exercise ExerciseForLearner
		kind     string
		unitID   uuid.UUID
		rank     int
	}
	byKind := map[string][]candidate{}
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.exercise.ID, &c.exercise.Number, &c.exercise.Type, &c.exercise.Skill,
			&c.exercise.Difficulty, &c.exercise.Prompt, &c.unitID, &c.rank); err != nil {
			return nil, mapErr(err)
		}
		c.kind = c.exercise.Type
		byKind[c.kind] = append(byKind[c.kind], c)
	}
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}

	// Take each kind's quota, spreading across units first and only then relaxing: a
	// level that keeps a kind in one unit still gets asked about it.
	chosen := make([]candidate, 0, ProficiencyPaperSize)
	taken := map[uuid.UUID]bool{}
	perKindUnit := map[string]map[uuid.UUID]int{}
	spread := func(kind string, unit uuid.UUID) int {
		if perKindUnit[kind] == nil {
			perKindUnit[kind] = map[uuid.UUID]int{}
		}
		return perKindUnit[kind][unit]
	}
	take := func(kind string, want, capPerUnit int) int {
		got := 0
		for _, c := range byKind[kind] {
			if got == want {
				break
			}
			if taken[c.exercise.ID] || spread(kind, c.unitID) >= capPerUnit {
				continue
			}
			taken[c.exercise.ID] = true
			if perKindUnit[kind] == nil {
				perKindUnit[kind] = map[uuid.UUID]int{}
			}
			perKindUnit[kind][c.unitID]++
			chosen = append(chosen, c)
			got++
		}
		return got
	}

	shortfall := 0
	for _, want := range proficiencyTypes {
		got := take(want.kind, want.want, maxPerUnitType)
		if got < want.want {
			got += take(want.kind, want.want-got, 1<<30)
		}
		shortfall += want.want - got
	}
	// Whatever is still missing comes from any gradable kind — a short paper would make
	// the pass mark mean something different at every level.
	for shortfall > 0 && len(chosen) < ProficiencyPaperSize {
		filled := false
		for _, want := range proficiencyTypes {
			for _, c := range byKind[want.kind] {
				if taken[c.exercise.ID] {
					continue
				}
				taken[c.exercise.ID] = true
				chosen = append(chosen, c)
				shortfall--
				filled = true
				break
			}
			if shortfall == 0 {
				break
			}
		}
		if !filled {
			break
		}
	}

	// Interleave the kinds so the paper alternates rather than running in blocks.
	byType := map[string][]candidate{}
	for _, c := range chosen {
		byType[c.kind] = append(byType[c.kind], c)
	}
	order := make([]candidate, 0, len(chosen))
	for round := 0; len(order) < len(chosen); round++ {
		progressed := false
		for _, want := range proficiencyTypes {
			pool := byType[want.kind]
			if round < len(pool) {
				order = append(order, pool[round])
				progressed = true
			}
		}
		if !progressed {
			break
		}
	}

	out := make([]ExerciseForLearner, 0, len(order))
	for _, c := range order {
		out = append(out, c.exercise)
	}
	return out, nil
}

// ExercisesOfLevel reports which of the given exercises belong to a level. The submit
// endpoint asks this before grading, so a learner cannot sit a paper made of another
// level's questions and have it counted against this one.
func (s *Store) ExercisesOfLevel(ctx context.Context, levelID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]bool{}, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id
		FROM exercises e
		JOIN lessons le ON le.id = e.lesson_id
		JOIN units u ON u.id = le.unit_id
		WHERE u.level_id = $1 AND e.id = ANY($2)`, levelID, ids)
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

// DrawnPaper is a paper the server has handed out.
type DrawnPaper struct {
	ID          uuid.UUID
	LevelID     uuid.UUID
	Seed        string
	ExerciseIDs []uuid.UUID
	SubmittedAt *string
	Score       *float64
	Passed      *bool
}

// DrawPaper records the paper the server just drew, so the submission can be checked
// against it rather than trusted.
func (s *Store) DrawPaper(ctx context.Context, userID, levelID uuid.UUID, seed string, exerciseIDs []uuid.UUID) (uuid.UUID, error) {
	id := uuid.New()
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO proficiency_papers (id, user_id, level_id, seed, exercise_ids)
		VALUES ($1, $2, $3, $4, $5)`, id, userID, levelID, seed, exerciseIDs)
	return id, mapErr(err)
}

// PaperByID loads a paper the caller owns.
func (s *Store) PaperByID(ctx context.Context, userID, paperID uuid.UUID) (DrawnPaper, error) {
	var p DrawnPaper
	err := s.Pool.QueryRow(ctx, `
		SELECT id, level_id, seed, exercise_ids, submitted_at::text, score, passed
		FROM proficiency_papers
		WHERE id = $1 AND user_id = $2`, paperID, userID).
		Scan(&p.ID, &p.LevelID, &p.Seed, &p.ExerciseIDs, &p.SubmittedAt, &p.Score, &p.Passed)
	return p, mapErr(err)
}

// SubmitPaper scores a drawn paper. Only a drawn paper can be sat once: the WHERE clause
// makes a second submission of the same paper a no-op rather than a second result.
func (s *Store) SubmitPaper(ctx context.Context, paperID uuid.UUID, score float64, passed bool, graded, correct int) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		UPDATE proficiency_papers
		SET submitted_at = now(), score = $2, passed = $3, graded_count = $4, correct_count = $5
		WHERE id = $1 AND submitted_at IS NULL`, paperID, score, passed, graded, correct)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}

// LearnerExercises returns the given exercises as the learner sees them, in the order
// asked for — the paper's order, not the database's.
func (s *Store) LearnerExercises(ctx context.Context, ids []uuid.UUID) ([]ExerciseForLearner, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id, e.exercise_number, e.type, e.skill, e.difficulty, e.prompt
		FROM exercises e
		WHERE e.id = ANY($1)
		ORDER BY array_position($1::uuid[], e.id)`, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]ExerciseForLearner, 0, len(ids))
	for rows.Next() {
		var e ExerciseForLearner
		if err := rows.Scan(&e.ID, &e.Number, &e.Type, &e.Skill, &e.Difficulty, &e.Prompt); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, e)
	}
	return out, mapErr(rows.Err())
}

// UnlockLevel records that a level is open, reporting whether this call was the one that
// opened it. The unique key means a retake cannot widen an existing unlock, and the
// caller uses the flag to tell "you unlocked level 3" from "level 3 was already open".
func (s *Store) UnlockLevel(ctx context.Context, userID, levelID, paperID uuid.UUID, score float64) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO level_unlocks (id, user_id, level_id, paper_id, score)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, level_id) DO NOTHING`, uuid.New(), userID, levelID, paperID, score)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() == 1, nil
}

// UnlockedLevelNumbers returns the levels this learner has opened with a test, keyed by
// level number. This is what DeriveLessonStates is handed.
func (s *Store) UnlockedLevelNumbers(ctx context.Context, userID, curriculumID uuid.UUID) (map[int]bool, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT l.level_number
		FROM level_unlocks lu
		JOIN levels l ON l.id = lu.level_id
		WHERE lu.user_id = $1 AND l.curriculum_id = $2`, userID, curriculumID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := map[int]bool{}
	for rows.Next() {
		var number int
		if err := rows.Scan(&number); err != nil {
			return nil, mapErr(err)
		}
		out[number] = true
	}
	return out, mapErr(rows.Err())
}

// ProficiencyCard is what the test-out entry point shows for one level: whether it is
// open, whether a test opened it, and how the learner's papers have gone.
type ProficiencyCard struct {
	Level     int    `json:"level"`
	Title     string `json:"title"`
	Questions int    `json:"questions"`
	// Unlocked means a proficiency test opened this level. Walking to a level creates no
	// row here, so this is always "earned by a test", never "reached by progress".
	Unlocked    bool     `json:"unlocked_by_test"`
	BestScore   *float64 `json:"best_score"`
	PapersTaken int      `json:"papers_taken"`
	Passed      bool     `json:"passed"`
}

// ProficiencyCards summarises every level for the learner, in one query each.
func (s *Store) ProficiencyCards(ctx context.Context, userID uuid.UUID, version string) ([]ProficiencyCard, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT l.level_number,
		       l.title,
		       count(DISTINCT e.id)::int                                             AS questions,
		       (lu.id IS NOT NULL)                                                   AS unlocked,
		       coalesce(p.papers, 0)::int                                            AS papers,
		       p.best,
		       coalesce(p.passed, false)                                             AS passed
		FROM levels l
		JOIN curricula c ON c.id = l.curriculum_id
		LEFT JOIN units u ON u.level_id = l.id
		LEFT JOIN lessons le ON le.unit_id = u.id
		LEFT JOIN exercises e ON e.lesson_id = le.id
		     AND e.type IN ('mcq', 'listening_mcq', 'tone_mcq', 'listening_dictation')
		LEFT JOIN level_unlocks lu ON lu.level_id = l.id AND lu.user_id = $1
		LEFT JOIN (
			SELECT level_id, count(*) AS papers, max(score) AS best, bool_or(passed) AS passed
			FROM proficiency_papers WHERE user_id = $1 GROUP BY level_id
		) p ON p.level_id = l.id
		WHERE c.version = $2
		GROUP BY l.level_number, l.title, lu.id, p.papers, p.best, p.passed
		ORDER BY l.level_number`, userID, version)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]ProficiencyCard, 0, 5)
	for rows.Next() {
		var card ProficiencyCard
		var best *float64
		if err := rows.Scan(&card.Level, &card.Title, &card.Questions, &card.Unlocked, &card.PapersTaken, &best, &card.Passed); err != nil {
			return nil, mapErr(err)
		}
		card.BestScore = best
		out = append(out, card)
	}
	return out, mapErr(rows.Err())
}
