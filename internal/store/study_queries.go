package store

import (
	"context"
	"fmt"

	"github.com/google/uuid"
)

// The study surfaces: the guide (vocabulary, grammar, bookmarks), the writing studio,
// the speaking studio and the passage reader.
//
// Every queue here serves content from ANY level, deliberately ungated by the path —
// the same decision the tone page made (see tone_queries.go): a learner who wants to
// practise 写 or speak before their path reaches it must be able to, and the attempts
// those surfaces record carry mode "studio", excluded from every derived value
// (migrations/0008_study.sql). The cost is stated in the UI: studio practice earns no
// XP, no mastery and no streak credit.
//
// Queues ARE filtered by level, because practising characters from a level the learner
// has never met teaches nothing. The caller defaults to the learner's own level and
// offers the rest as a deliberate choice.

// studioTypes are the exercise types the studios serve and grade. A studio attempt
// against anything else is rejected (see StudioDrillIDs), the same rule the tone
// endpoint enforces with ToneDrillIDs, so an endpoint cannot grade drills its queue
// would never have served.
var studioTypes = []string{
	"hanzi_trace", "hanzi_recall", // writing studio
	"speak_repeat", "read_aloud", "speak_answer", // speaking studio
}

// StudyDrill reports which of the given exercises the studios may serve, and the
// level each one comes from so the page can say where a drill belongs.
type StudyDrill struct {
	ExerciseForLearner
	Level int `json:"level"`
}

// StudioQueue returns writing/speaking drills for one level (0 means every level),
// ordered by a hash of the content key and the learner's id: stable across reloads
// so a session does not reshuffle mid-practice, different per learner.
//
// `prefer` is the learner's own level. When the queue is not confined to a level, that
// level's drills come first and the rest of the round is filled from the others — so a
// beginner practises characters they have met first, and still gets a full round even
// though level 1 is small.
func (s *Store) StudioQueue(ctx context.Context, userID uuid.UUID, types []string, level, limit, prefer int, character string) ([]StudyDrill, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id, e.exercise_number, e.type, e.skill, e.difficulty, e.prompt, l.level_number
		FROM exercises e
		JOIN lessons le ON le.id = e.lesson_id
		JOIN units u ON u.id = le.unit_id
		JOIN levels l ON l.id = u.level_id
		WHERE e.type = ANY($1)
		  AND ($2 = 0 OR l.level_number = $2)
		  AND ($6::text = '' OR e.prompt->>'character' = $6)
		ORDER BY (l.level_number = $5) DESC, md5(e.content_key || $3::text)
		LIMIT $4`, types, level, userID, limit, prefer, character)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]StudyDrill, 0, limit)
	for rows.Next() {
		var d StudyDrill
		if err := rows.Scan(&d.ID, &d.Number, &d.Type, &d.Skill, &d.Difficulty, &d.Prompt, &d.Level); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, d)
	}
	return out, mapErr(rows.Err())
}

// StudioStats returns, for every level that has studio drills of the given types,
// how many exist and what the learner has done with them in the studios. Only
// studio-mode attempts count: this is the practice record, not the path's progress.
func (s *Store) StudioStats(ctx context.Context, userID uuid.UUID, types []string) ([]ToneLevelStat, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT l.level_number,
		       count(DISTINCT e.id)::int                                            AS drills,
		       count(a.id) FILTER (WHERE a.mode = 'studio')::int                    AS answered,
		       count(a.id) FILTER (WHERE a.mode = 'studio' AND a.is_correct)::int   AS correct
		FROM exercises e
		JOIN lessons le ON le.id = e.lesson_id
		JOIN units u ON u.id = le.unit_id
		JOIN levels l ON l.id = u.level_id
		LEFT JOIN attempts a ON a.exercise_id = e.id AND a.user_id = $1::uuid
		WHERE e.type = ANY($2)
		GROUP BY l.level_number
		ORDER BY l.level_number`, userID, types)
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

// StudioDrillIDs reports which of the given exercises the studios consider theirs.
// The attempt endpoint asks this before grading, so the queue and the grader cannot
// drift into disagreeing about what a studio drill is.
func (s *Store) StudioDrillIDs(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]bool, error) {
	if len(ids) == 0 {
		return map[uuid.UUID]bool{}, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT e.id FROM exercises e WHERE e.id = ANY($1) AND e.type = ANY($2)`, ids, studioTypes)
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

// --- vocabulary --------------------------------------------------------

// StudyLexeme is one word as the guide shows it: the lexeme row plus the HSK level
// it sits at in this curriculum and whether the learner bookmarked it.
type StudyLexeme struct {
	Lexeme
	Level       int  `json:"level"`
	Bookmarked  bool `json:"bookmarked"`
}

// VocabularyList returns the guide's vocabulary for a curriculum, filtered by level
// (0 = every level) and an optional search over the simplified form, the pinyin and
// the English senses, ordered by level then frequency (the curriculum's own ordering).
// Page-scoped with limit/offset and a total, so the guide can grow without the
// response growing with it.
func (s *Store) VocabularyList(ctx context.Context, userID uuid.UUID, version string, level, limit, offset int, search string) ([]StudyLexeme, int, error) {
	// Two argument lists, not one: the count does not take the learner, and a parameter
	// that appears in no expression cannot be typed by Postgres ("could not determine data
	// type of parameter $1") — a 500 for a query that is otherwise correct.
	countWhere := `c.version = $1`
	countArgs := []any{version}
	countNext := 2
	if level > 0 {
		countWhere += fmt.Sprintf(` AND ll.level_number = $%d`, countNext)
		countArgs = append(countArgs, level)
		countNext++
	}
	if search != "" {
		countWhere += fmt.Sprintf(` AND (lx.simplified ILIKE $%d OR lx.pinyin ILIKE $%d OR lx.senses::text ILIKE $%d)`, countNext, countNext, countNext)
		countArgs = append(countArgs, "%"+search+"%")
	}

	var total int
	if err := s.Pool.QueryRow(ctx, `
		SELECT count(*)
		FROM lexemes lx
		JOIN lexeme_levels ll ON ll.lexeme_id = lx.id
		JOIN curricula c ON c.id = ll.curriculum_id
		WHERE `+countWhere, countArgs...).Scan(&total); err != nil {
		return nil, 0, mapErr(err)
	}

	// $1 belongs to the list query: it decides whether each word is bookmarked.
	where := `c.version = $2`
	args := []any{userID, version}
	next := 3
	if level > 0 {
		where += fmt.Sprintf(` AND ll.level_number = $%d`, next)
		args = append(args, level)
		next++
	}
	if search != "" {
		where += fmt.Sprintf(` AND (lx.simplified ILIKE $%d OR lx.pinyin ILIKE $%d OR lx.senses::text ILIKE $%d)`, next, next, next)
		args = append(args, "%"+search+"%")
	}

	args = append(args, limit, offset)
	rows, err := s.Pool.Query(ctx, `
		SELECT lx.id, lx.simplified, lx.traditional, lx.pinyin, lx.parts_of_speech, lx.senses, lx.audio_key, ll.level_number,
		       EXISTS (SELECT 1 FROM bookmarks b WHERE b.user_id = $1::uuid AND b.lexeme_id = lx.id)
		FROM lexemes lx
		JOIN lexeme_levels ll ON ll.lexeme_id = lx.id
		JOIN curricula c ON c.id = ll.curriculum_id
		WHERE `+where+`
		ORDER BY ll.level_number, lx.freq_rank NULLS LAST, lx.simplified
		LIMIT $`+placeholders(len(args)-1)+` OFFSET $`+placeholders(len(args)), args...)
	if err != nil {
		return nil, 0, mapErr(err)
	}
	defer rows.Close()

	out := make([]StudyLexeme, 0, limit)
	for rows.Next() {
		var item StudyLexeme
		if err := rows.Scan(&item.ID, &item.Simplified, &item.Traditional, &item.Pinyin, &item.POS, &item.Senses, &item.AudioKey, &item.Level, &item.Bookmarked); err != nil {
			return nil, 0, mapErr(err)
		}
		out = append(out, item)
	}
	return out, total, mapErr(rows.Err())
}

// placeholders renders a one-based placeholder number; the dynamic WHERE above makes
// the LIMIT/OFFSET positions depend on how many filters are active.
func placeholders(n int) string {
	if n <= 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

// --- bookmarks ---------------------------------------------------------

// AddBookmark records a learner's bookmark. Idempotent: re-adding an existing
// bookmark affects no row and returns false.
func (s *Store) AddBookmark(ctx context.Context, userID, lexemeID uuid.UUID) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		INSERT INTO bookmarks (id, user_id, lexeme_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (user_id, lexeme_id) DO NOTHING`, uuid.New(), userID, lexemeID)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() > 0, nil
}

// RemoveBookmark deletes a bookmark. Removing what is not there is a success that
// changed nothing, not an error.
func (s *Store) RemoveBookmark(ctx context.Context, userID, lexemeID uuid.UUID) (bool, error) {
	tag, err := s.Pool.Exec(ctx, `
		DELETE FROM bookmarks WHERE user_id = $1::uuid AND lexeme_id = $2::uuid`, userID, lexemeID)
	if err != nil {
		return false, mapErr(err)
	}
	return tag.RowsAffected() > 0, nil
}

// BookmarksList lists the learner's bookmarked words, newest first, with the HSK
// level each sits at in the given version. A word bookmarked at several levels
// appears once per level row; the guide groups by level anyway.
func (s *Store) BookmarksList(ctx context.Context, userID uuid.UUID, version string) ([]StudyLexeme, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT lx.id, lx.simplified, lx.traditional, lx.pinyin, lx.parts_of_speech, lx.senses, lx.audio_key, ll.level_number,
		       true
		FROM bookmarks b
		JOIN lexemes lx ON lx.id = b.lexeme_id
		JOIN lexeme_levels ll ON ll.lexeme_id = lx.id
		JOIN curricula c ON c.id = ll.curriculum_id
		WHERE b.user_id = $1::uuid AND c.version = $2
		ORDER BY b.created_at DESC, ll.level_number`, userID, version)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]StudyLexeme, 0, 8)
	for rows.Next() {
		var item StudyLexeme
		if err := rows.Scan(&item.ID, &item.Simplified, &item.Traditional, &item.Pinyin, &item.POS, &item.Senses, &item.AudioKey, &item.Level, &item.Bookmarked); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, item)
	}
	return out, mapErr(rows.Err())
}

// --- reading -----------------------------------------------------------

// ReadingPassage is a passage as the reader shows it: the passage row plus the
// level it sits at. Reading records no attempts at all — a passage is read, not
// answered, so there is nothing to exclude and nothing to record.
type ReadingPassage struct {
	Passage
	Level int `json:"level"`
}

// ReadingPassages returns the curriculum's reading passages, filtered by level
// (0 = every level), newest curriculum order. Only passages the importer stored
// with a body appear; nothing is generated here.
func (s *Store) ReadingPassages(ctx context.Context, version string, level int) ([]ReadingPassage, error) {
	where := `p.curriculum_version = $1 AND p.kind = 'reading'`
	args := []any{version}
	if level > 0 {
		args = append(args, level)
		where += ` AND p.level_number = $2`
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT p.id, p.kind, p.title, p.instruction, p.body, p.sentences, p.audio_key, p.word_count, p.minutes, p.level_number
		FROM passages p
		WHERE `+where+`
		ORDER BY p.level_number, p.created_at`, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]ReadingPassage, 0, 4)
	for rows.Next() {
		var rp ReadingPassage
		if err := rows.Scan(&rp.ID, &rp.Kind, &rp.Title, &rp.Instruction, &rp.Body, &rp.Sentences, &rp.AudioKey, &rp.WordCount, &rp.Minutes, &rp.Level); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, rp)
	}
	return out, mapErr(rows.Err())
}

// GrammarNote is one grammar point as the guide shows it: the explanation a person
// authored, plus the example drawn from the same exercise's own prompt. Nothing is
// generated — where a level has no authored grammar exercise, it contributes no note.
type GrammarNote struct {
	ID            uuid.UUID `json:"id"`
	Level         int       `json:"level"`
	UnitTitle     string    `json:"unit_title"`
	LessonTitle   string    `json:"lesson_title"`
	Explanation   string    `json:"explanation"`
	Example       string    `json:"example"`
	ExamplePinyin string    `json:"example_pinyin"`
	ExampleType   string    `json:"example_type"`
}

// GrammarNotes returns every authored grammar point for a curriculum, filtered by
// level (0 = every level), in curriculum order. The example is the exercise's own
// sentence (fill_blank), source (translation) or pinyin — fields the prompt actually
// carries, so no example is invented.
func (s *Store) GrammarNotes(ctx context.Context, version string, level int) ([]GrammarNote, error) {
	where := `c.version = $1`
	args := []any{version}
	if level > 0 {
		args = append(args, level)
		where += ` AND l.level_number = $2`
	}
	rows, err := s.Pool.Query(ctx, `
		SELECT e.id, l.level_number, u.title, le.title,
		       coalesce(e.explanation, ''),
		       coalesce(e.prompt->>'sentence', e.prompt->>'source', ''),
		       coalesce(e.prompt->>'pinyin', ''),
		       e.type
		FROM exercises e
		JOIN lessons le ON le.id = e.lesson_id
		JOIN units u ON u.id = le.unit_id
		JOIN levels l ON l.id = u.level_id
		JOIN curricula c ON c.id = l.curriculum_id
		WHERE e.skill = 'grammar' AND e.explanation IS NOT NULL AND `+where+`
		ORDER BY l.level_number, u.unit_number, le.lesson_number, e.exercise_number`, args...)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]GrammarNote, 0, 16)
	for rows.Next() {
		var n GrammarNote
		if err := rows.Scan(&n.ID, &n.Level, &n.UnitTitle, &n.LessonTitle, &n.Explanation, &n.Example, &n.ExamplePinyin, &n.ExampleType); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, n)
	}
	return out, mapErr(rows.Err())
}

// StudioCharacter is one character the writing studio can drill, with where it comes from.
type StudioCharacter struct {
	Character string `json:"character"`
	Pinyin    string `json:"pinyin"`
	Level     int    `json:"level"`
}

// StudioCharacters lists every distinct character the given studio drill types cover for one
// level (0 means every level), in the order the curriculum teaches them. The queue is capped
// at a round's worth of drills, so it cannot be the source of a picker that shows them all.
func (s *Store) StudioCharacters(ctx context.Context, types []string, level int) ([]StudioCharacter, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT e.prompt->>'character', min(coalesce(e.prompt->>'pinyin', '')), min(l.level_number)
		FROM exercises e
		JOIN lessons le ON le.id = e.lesson_id
		JOIN units u ON u.id = le.unit_id
		JOIN levels l ON l.id = u.level_id
		WHERE e.type = ANY($1)
		  AND coalesce(e.prompt->>'character', '') <> ''
		  AND ($2 = 0 OR l.level_number = $2)
		GROUP BY e.prompt->>'character'
		ORDER BY min(l.level_number), min(u.unit_number), min(le.lesson_number), min(e.exercise_number)`, types, level)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	out := make([]StudioCharacter, 0, 200)
	for rows.Next() {
		var c StudioCharacter
		if err := rows.Scan(&c.Character, &c.Pinyin, &c.Level); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, c)
	}
	return out, mapErr(rows.Err())
}
