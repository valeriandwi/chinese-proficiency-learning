package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// Curricula lists every registered syllabus version with content counts.
func (s *Store) Curricula(ctx context.Context) ([]Curriculum, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT c.id, c.version, c.name, c.standard, c.stage_model, c.status,
		       to_char(c.effective_from, 'YYYY-MM-DD'), coalesce(c.notes, ''),
		       (SELECT count(*) FROM levels l WHERE l.curriculum_id = c.id),
		       (SELECT count(*) FROM units u JOIN levels l ON l.id = u.level_id WHERE l.curriculum_id = c.id),
		       (SELECT count(*) FROM lessons ls JOIN units u ON u.id = ls.unit_id JOIN levels l ON l.id = u.level_id WHERE l.curriculum_id = c.id),
		       (SELECT count(*) FROM exercises e JOIN lessons ls ON ls.id = e.lesson_id JOIN units u ON u.id = ls.unit_id JOIN levels l ON l.id = u.level_id WHERE l.curriculum_id = c.id)
		FROM curricula c
		ORDER BY c.version`)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	var out []Curriculum
	for rows.Next() {
		var c Curriculum
		if err := rows.Scan(&c.ID, &c.Version, &c.Name, &c.Standard, &c.StageModel, &c.Status,
			&c.EffectiveFrom, &c.Notes, &c.LevelCount, &c.UnitCount, &c.LessonCount, &c.ExerciseCount); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, c)
	}
	return out, mapErr(rows.Err())
}

// CurriculumByVersion loads one syllabus by its version slug.
func (s *Store) CurriculumByVersion(ctx context.Context, version string) (*Curriculum, error) {
	var c Curriculum
	err := s.Pool.QueryRow(ctx, `
		SELECT id, version, name, standard, stage_model, status, to_char(effective_from, 'YYYY-MM-DD'), coalesce(notes, ''), 0, 0, 0, 0
		FROM curricula WHERE version = $1`, version).
		Scan(&c.ID, &c.Version, &c.Name, &c.Standard, &c.StageModel, &c.Status, &c.EffectiveFrom, &c.Notes,
			&c.LevelCount, &c.UnitCount, &c.LessonCount, &c.ExerciseCount)
	if err != nil {
		return nil, mapErr(err)
	}
	return &c, nil
}

// Levels lists the levels of a curriculum.
func (s *Store) Levels(ctx context.Context, curriculumID uuid.UUID) ([]Level, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT l.id, l.level_number, l.stage, l.title, coalesce(l.description, ''), coalesce(l.vocab_target, 0),
		       (SELECT count(*) FROM units u WHERE u.level_id = l.id),
		       (SELECT count(*) FROM lessons ls JOIN units u ON u.id = ls.unit_id WHERE u.level_id = l.id)
		FROM levels l WHERE l.curriculum_id = $1 ORDER BY l.level_number`, curriculumID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	var out []Level
	for rows.Next() {
		var l Level
		if err := rows.Scan(&l.ID, &l.LevelNumber, &l.Stage, &l.Title, &l.Description, &l.VocabTarget, &l.UnitCount, &l.LessonCount); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, l)
	}
	return out, mapErr(rows.Err())
}

// LessonRefsOrdered returns every lesson of a curriculum in path order. This
// ordering is what lock state is derived from.
func (s *Store) LessonRefsOrdered(ctx context.Context, curriculumID uuid.UUID) ([]LessonRef, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT ls.id, u.id, u.unit_number, ls.lesson_number, ls.kind, ls.title, l.level_number
		FROM lessons ls
		JOIN units u ON u.id = ls.unit_id
		JOIN levels l ON l.id = u.level_id
		WHERE l.curriculum_id = $1
		ORDER BY l.level_number, u.unit_number, ls.lesson_number`, curriculumID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	var out []LessonRef
	for rows.Next() {
		var r LessonRef
		if err := rows.Scan(&r.LessonID, &r.UnitID, &r.UnitNumber, &r.LessonNumber, &r.Kind, &r.Title, &r.LevelNumber); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, r)
	}
	return out, mapErr(rows.Err())
}

// LessonProgressMap returns the learner's per-lesson derived rows.
func (s *Store) LessonProgressMap(ctx context.Context, userID uuid.UUID) (map[uuid.UUID]PathLesson, map[uuid.UUID]bool, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT lesson_id, status, attempts_count, best_score, completed_at
		FROM lesson_progress WHERE user_id = $1`, userID)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	defer rows.Close()

	progress := map[uuid.UUID]PathLesson{}
	completed := map[uuid.UUID]bool{}
	for rows.Next() {
		var pl PathLesson
		var id uuid.UUID
		var status string
		if err := rows.Scan(&id, &status, &pl.Attempts, &pl.BestScore, &pl.CompletedAt); err != nil {
			return nil, nil, mapErr(err)
		}
		pl.State = status
		progress[id] = pl
		if status == "completed" {
			completed[id] = true
		}
	}
	return progress, completed, mapErr(rows.Err())
}

// PathForUser assembles the full level -> unit -> lesson tree for one learner and
// one curriculum version, with exercise counts and derived progress merged in.
func (s *Store) PathForUser(ctx context.Context, curriculumVersion string, userID uuid.UUID) ([]PathLevel, error) {
	c, err := s.CurriculumByVersion(ctx, curriculumVersion)
	if err != nil {
		return nil, err
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT l.id, l.level_number, l.stage, l.title, coalesce(l.description, ''),
		       u.id, u.unit_number, u.title, coalesce(u.description, ''),
		       ls.id, ls.lesson_number, ls.title, ls.kind, coalesce(ls.goal, ''), ls.xp_reward, ls.estimated_minutes,
		       (SELECT count(*) FROM exercises e WHERE e.lesson_id = ls.id) AS exercise_count,
		       lp.status, lp.best_score, coalesce(lp.attempts_count, 0), lp.completed_at
		FROM levels l
		JOIN units u ON u.level_id = l.id
		JOIN lessons ls ON ls.unit_id = u.id
		LEFT JOIN lesson_progress lp ON lp.lesson_id = ls.id AND lp.user_id = $2
		WHERE l.curriculum_id = $1
		ORDER BY l.level_number, u.unit_number, ls.lesson_number`, c.ID, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	var levels []PathLevel
	levelIdx := map[uuid.UUID]int{}
	unitIdx := map[uuid.UUID]int{}

	for rows.Next() {
		var levelID, unitID, lessonID uuid.UUID
		var levelNum int
		var stage *string
		var levelTitle, levelDesc string
		var unitNum int
		var unitTitle, unitDesc string
		var lesson PathLesson
		var st *string

		if err := rows.Scan(&levelID, &levelNum, &stage, &levelTitle, &levelDesc,
			&unitID, &unitNum, &unitTitle, &unitDesc,
			&lessonID, &lesson.LessonNumber, &lesson.Title, &lesson.Kind, &lesson.Goal,
			&lesson.XPReward, &lesson.EstimatedMinutes, &lesson.ExerciseCount,
			&st, &lesson.BestScore, &lesson.Attempts, &lesson.CompletedAt); err != nil {
			return nil, mapErr(err)
		}
		lesson.ID = lessonID
		if st != nil {
			lesson.State = *st
		}

		li, ok := levelIdx[levelID]
		if !ok {
			levels = append(levels, PathLevel{ID: levelID, LevelNumber: levelNum, Stage: stage,
				Title: levelTitle, Description: levelDesc})
			li = len(levels) - 1
			levelIdx[levelID] = li
		}
		ui, ok := unitIdx[unitID]
		if !ok {
			levels[li].Units = append(levels[li].Units, PathUnit{ID: unitID, UnitNumber: unitNum,
				Title: unitTitle, Description: unitDesc})
			ui = len(levels[li].Units) - 1
			unitIdx[unitID] = ui
		}
		levels[li].Units[ui].Lessons = append(levels[li].Units[ui].Lessons, lesson)
		levels[li].Total++
		if lesson.State == "completed" {
			levels[li].Completed++
		}
	}
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}
	return levels, nil
}

// LessonDetail loads everything the player needs. Exercise answer keys are not
// part of the returned payload: ExerciseForLearner has no field for them.
func (s *Store) LessonDetail(ctx context.Context, lessonID uuid.UUID) (*LessonDetail, error) {
	d := &LessonDetail{}
	err := s.Pool.QueryRow(ctx, `
		SELECT ls.id, ls.title, ls.kind, coalesce(ls.goal, ''), ls.xp_reward, ls.estimated_minutes,
		       u.title, l.level_number, c.version
		FROM lessons ls
		JOIN units u ON u.id = ls.unit_id
		JOIN levels l ON l.id = u.level_id
		JOIN curricula c ON c.id = l.curriculum_id
		WHERE ls.id = $1`, lessonID).
		Scan(&d.ID, &d.Title, &d.Kind, &d.Goal, &d.XPReward, &d.EstimatedMinutes,
			&d.UnitTitle, &d.LevelNumber, &d.CurriculumVersion)
	if err != nil {
		return nil, mapErr(err)
	}

	exRows, err := s.Pool.Query(ctx, `
		SELECT id, exercise_number, type, skill, difficulty, prompt
		FROM exercises WHERE lesson_id = $1 ORDER BY exercise_number`, lessonID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer exRows.Close()
	for exRows.Next() {
		var e ExerciseForLearner
		if err := exRows.Scan(&e.ID, &e.Number, &e.Type, &e.Skill, &e.Difficulty, &e.Prompt); err != nil {
			return nil, mapErr(err)
		}
		d.Exercises = append(d.Exercises, e)
	}
	if err := exRows.Err(); err != nil {
		return nil, mapErr(err)
	}

	d.Passages, err = s.passagesForLesson(ctx, lessonID)
	if err != nil {
		return nil, err
	}
	d.Vocabulary, err = s.lessonVocabulary(ctx, lessonID)
	if err != nil {
		return nil, err
	}
	return d, nil
}

func (s *Store) passagesForLesson(ctx context.Context, lessonID uuid.UUID) ([]Passage, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, p.kind, p.title, coalesce(p.instruction, ''), p.body, p.sentences, p.audio_key, p.word_count, p.minutes
		FROM passages p
		JOIN lesson_passages lp ON lp.passage_id = p.id
		WHERE lp.lesson_id = $1
		ORDER BY lp.position`, lessonID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	var out []Passage
	for rows.Next() {
		var p Passage
		if err := rows.Scan(&p.ID, &p.Kind, &p.Title, &p.Instruction, &p.Body, &p.Sentences, &p.AudioKey, &p.WordCount, &p.Minutes); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, p)
	}
	return out, mapErr(rows.Err())
}

func (s *Store) lessonVocabulary(ctx context.Context, lessonID uuid.UUID) ([]Lexeme, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT x.id, x.simplified, x.traditional, x.pinyin, x.parts_of_speech, x.senses, x.audio_key,
		       (SELECT min(ll.level_number) FROM lexeme_levels ll WHERE ll.lexeme_id = x.id)
		FROM exercise_lexemes el
		JOIN exercises e ON e.id = el.exercise_id
		JOIN lexemes x ON x.id = el.lexeme_id
		WHERE e.lesson_id = $1
		ORDER BY x.simplified`, lessonID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	var out []Lexeme
	for rows.Next() {
		var x Lexeme
		if err := rows.Scan(&x.ID, &x.Simplified, &x.Traditional, &x.Pinyin, &x.POS, &x.Senses, &x.AudioKey, &x.HSKLevel); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, x)
	}
	return out, mapErr(rows.Err())
}

// ExercisesForGrading loads the server-only side of the given exercises.
func (s *Store) ExercisesForGrading(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]ExerciseForGrading, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT id, lesson_id, type, skill, prompt, answer_key, explanation
		FROM exercises WHERE id = ANY($1)`, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make(map[uuid.UUID]ExerciseForGrading, len(ids))
	for rows.Next() {
		var e ExerciseForGrading
		if err := rows.Scan(&e.ID, &e.LessonID, &e.Type, &e.Skill, &e.Prompt, &e.AnswerKey, &e.Explanation); err != nil {
			return nil, mapErr(err)
		}
		out[e.ID] = e
	}
	return out, mapErr(rows.Err())
}

// ExerciseCount returns how many exercises a lesson has (used to decide whether a
// lesson can be completed).
func (s *Store) ExerciseCount(ctx context.Context, lessonID uuid.UUID) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM exercises WHERE lesson_id = $1`, lessonID).Scan(&n)
	return n, mapErr(err)
}

// LedgerOfLessonLexemes returns the distinct lexemes taught by a lesson, which
// becomes the lesson's spaced-repetition deck once the lesson is completed.
func (s *Store) LedgerOfLessonLexemes(ctx context.Context, lessonID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT DISTINCT el.lexeme_id
		FROM exercise_lexemes el
		JOIN exercises e ON e.id = el.exercise_id
		WHERE e.lesson_id = $1`, lessonID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, id)
	}
	return out, mapErr(rows.Err())
}

// VocabularySample returns a spread of lexemes from a curriculum level, used by
// the placement check and by review decks.
func (s *Store) VocabularySample(ctx context.Context, curriculumVersion string, level int, limit int) ([]Lexeme, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT x.id, x.simplified, x.traditional, x.pinyin, x.parts_of_speech, x.senses, x.audio_key, $1::int
		FROM lexemes x
		JOIN lexeme_levels ll ON ll.lexeme_id = x.id
		JOIN curricula c ON c.id = ll.curriculum_id
		WHERE c.version = $2 AND ll.level_number = $3
		ORDER BY x.freq_rank NULLS LAST, x.simplified
		LIMIT $4`, level, curriculumVersion, level, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []Lexeme
	for rows.Next() {
		var x Lexeme
		if err := rows.Scan(&x.ID, &x.Simplified, &x.Traditional, &x.Pinyin, &x.POS, &x.Senses, &x.AudioKey, &x.HSKLevel); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, x)
	}
	return out, mapErr(rows.Err())
}

// NextLessonAfter returns the first lesson in path order that the learner has not
// completed, which is what the dashboard's "continue" card shows.
func (s *Store) NextLessonAfter(ctx context.Context, curriculumVersion string, userID uuid.UUID) (*uuid.UUID, string, error) {
	var id uuid.UUID
	var title string
	err := s.Pool.QueryRow(ctx, `
		SELECT ls.id, ls.title
		FROM lessons ls
		JOIN units u ON u.id = ls.unit_id
		JOIN levels l ON l.id = u.level_id
		JOIN curricula c ON c.id = l.curriculum_id
		LEFT JOIN lesson_progress lp ON lp.lesson_id = ls.id AND lp.user_id = $2
		WHERE c.version = $1 AND coalesce(lp.status, '') <> 'completed'
		ORDER BY l.level_number, u.unit_number, ls.lesson_number
		LIMIT 1`, curriculumVersion, userID).Scan(&id, &title)
	if err != nil {
		return nil, "", mapErr(err)
	}
	return &id, title, nil
}

// LessonExists reports whether a lesson id is real.
func (s *Store) LessonExists(ctx context.Context, id uuid.UUID) (bool, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM lessons WHERE id = $1`, id).Scan(&n)
	if err != nil {
		return false, mapErr(err)
	}
	return n > 0, nil
}

// CurriculumVersionForLesson resolves the curriculum version that owns a lesson.
func (s *Store) CurriculumVersionForLesson(ctx context.Context, lessonID uuid.UUID) (string, error) {
	var version string
	err := s.Pool.QueryRow(ctx, `
		SELECT c.version
		FROM lessons ls
		JOIN units u ON u.id = ls.unit_id
		JOIN levels l ON l.id = u.level_id
		JOIN curricula c ON c.id = l.curriculum_id
		WHERE ls.id = $1`, lessonID).Scan(&version)
	if err != nil {
		return "", mapErr(err)
	}
	return version, nil
}

var _ = fmt.Sprintf
