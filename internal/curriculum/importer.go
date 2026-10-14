// Package curriculum parses, validates and imports authored HSK content.
//
// Content is data, not code: a curriculum version, its levels, units, lessons,
// vocabulary, passages and exercises all live in YAML under content/ and are
// imported with cmd/contentctl. Adding HSK 3.0's nine levels is therefore an
// import, not a code change.
//
// Import is idempotent and non-destructive: every authored node is upserted on a
// natural key, and exercises are identified by content_key rather than by
// position, so re-importing edited content preserves exercise ids — and with them
// every learner attempt that references them.
package curriculum

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"gopkg.in/yaml.v3"

	"github.com/valeriandwipurnomo/hanpath/internal/exercises"
	"github.com/valeriandwipurnomo/hanpath/internal/media"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// authoringAudioKey is a placeholder that exists only during shape validation.
// The real audio key is a content hash of the spoken sentence, which an author
// cannot write by hand, so it is resolved at import time from `audio_text`.
const authoringAudioKey = "pending-audio"

// AudioProvisioner supplies the store and synthesiser that turn a listening
// exercise's `audio_text` into a real asset.
type AudioProvisioner struct {
	Store media.Store
	TTS   media.TTSProvider
}

// prepareExercisePrompt separates authoring metadata from the stored prompt.
//
// `audio_text` is what the synthesiser speaks; `audio_key` is what the browser
// fetches. Keeping them apart means authors never handle hashes, and the registry
// never sees an authoring-only field (its decoders reject unknown fields, which is
// how a transcript accidentally pasted into a prompt is caught).
func prepareExercisePrompt(e ExerciseDef) (prompt map[string]any, audioText string, err error) {
	prompt = make(map[string]any, len(e.Prompt)+1)
	for k, v := range e.Prompt {
		prompt[k] = v
	}
	if raw, ok := prompt["audio_text"]; ok {
		delete(prompt, "audio_text")
		if s, ok := raw.(string); ok {
			audioText = strings.TrimSpace(s)
		}
	}

	needsAudio := e.Type == "speak_repeat"
	for _, t := range exercises.ListeningTypes() {
		if t == e.Type {
			needsAudio = true
		}
	}
	if needsAudio {
		if audioText == "" {
			return nil, "", fmt.Errorf("a %s exercise needs audio_text: the sentence the learner must hear", e.Type)
		}
		prompt["audio_key"] = authoringAudioKey
	} else if audioText != "" {
		// speak_answer and friends may carry optional audio (a spoken question).
		prompt["audio_key"] = authoringAudioKey
	}
	return prompt, audioText, nil
}

// File is one authored curriculum document.
type File struct {
	Version       string       `yaml:"version"`
	Name          string       `yaml:"name"`
	Standard      string       `yaml:"standard"`
	StageModel    string       `yaml:"stage_model"`
	Status        string       `yaml:"status"`
	EffectiveFrom string       `yaml:"effective_from"`
	Notes         string       `yaml:"notes"`
	Vocabulary    []VocabEntry `yaml:"vocabulary"`
	Passages      []PassageDef `yaml:"passages"`
	Levels        []LevelDef   `yaml:"levels"`
}

// VocabEntry is one authored lexeme.
type VocabEntry struct {
	Key           string              `yaml:"key"`
	Simplified    string              `yaml:"simplified"`
	Traditional   string              `yaml:"traditional"`
	Pinyin        string              `yaml:"pinyin"`
	PartsOfSpeech []string            `yaml:"parts_of_speech"`
	Senses        []map[string]string `yaml:"senses"`
	AudioKey      string              `yaml:"audio_key"`
	FreqRank      int                 `yaml:"freq_rank"`
	Levels        map[string]int      `yaml:"levels"`
}

// LevelDef is a level within the curriculum.
type LevelDef struct {
	Level       int       `yaml:"level"`
	Stage       string    `yaml:"stage"`
	Title       string    `yaml:"title"`
	Description string    `yaml:"description"`
	VocabTarget int       `yaml:"vocab_target"`
	Units       []UnitDef `yaml:"units"`
}

// UnitDef groups lessons.
type UnitDef struct {
	Unit        int         `yaml:"unit"`
	Title       string      `yaml:"title"`
	Description string      `yaml:"description"`
	Lessons     []LessonDef `yaml:"lessons"`
}

// LessonDef is one lesson and its content.
type LessonDef struct {
	Lesson           int           `yaml:"lesson"`
	Title            string        `yaml:"title"`
	Kind             string        `yaml:"kind"`
	Goal             string        `yaml:"goal"`
	XPReward         int           `yaml:"xp_reward"`
	EstimatedMinutes int           `yaml:"estimated_minutes"`
	Vocabulary       []string      `yaml:"vocabulary"`
	Passages         []string      `yaml:"passages"`
	Exercises        []ExerciseDef `yaml:"exercises"`
}

// ExerciseDef is one authored exercise. Prompt and AnswerKey are decoded again by
// the exercise registry, which is the authority on their shape.
type ExerciseDef struct {
	Key         string         `yaml:"key"`
	Type        string         `yaml:"type"`
	Skill       string         `yaml:"skill"`
	Difficulty  int            `yaml:"difficulty"`
	Explanation string         `yaml:"explanation"`
	Prompt      map[string]any `yaml:"prompt"`
	AnswerKey   map[string]any `yaml:"answer_key"`
}

// PassageDef is an authored reading or listening passage.
type PassageDef struct {
	Kind        string           `yaml:"kind"`
	Title       string           `yaml:"title"`
	Level       int              `yaml:"level"`
	Instruction string           `yaml:"instruction"`
	Body        string           `yaml:"body"`
	Sentences   []map[string]any `yaml:"sentences"`
	AudioKey    string           `yaml:"audio_key"`
	WordCount   int              `yaml:"word_count"`
	Minutes     int              `yaml:"minutes"`
}

// Parse reads a document strictly: an unknown YAML key is an error, so a typo in
// authored content is caught at import instead of silently dropping a field.
func Parse(r io.Reader) (*File, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	var f File
	if err := dec.Decode(&f); err != nil {
		return nil, fmt.Errorf("curriculum: parsing YAML: %w", err)
	}
	return &f, nil
}

// Validate checks the document without touching a database.
func (f *File) Validate() []string {
	var problems []string
	add := func(format string, args ...any) { problems = append(problems, fmt.Sprintf(format, args...)) }

	if f.Version == "" {
		add("version is required")
	}
	if f.Name == "" {
		add("name is required")
	}
	switch f.StageModel {
	case "six-levels", "three-stages-nine-levels":
	default:
		add("stage_model must be six-levels or three-stages-nine-levels, got %q", f.StageModel)
	}
	switch f.Status {
	case "draft", "published", "archived":
	default:
		add("status must be draft, published or archived, got %q", f.Status)
	}
	if f.EffectiveFrom != "" {
		if _, err := time.Parse("2006-01-02", f.EffectiveFrom); err != nil {
			add("effective_from must be YYYY-MM-DD, got %q", f.EffectiveFrom)
		}
	}

	// Vocabulary keys must be unique and complete.
	vocabKeys := map[string]bool{}
	for i, v := range f.Vocabulary {
		where := fmt.Sprintf("vocabulary[%d] (%s)", i, v.Simplified)
		if v.Key == "" {
			add("%s: key is required", where)
		} else if vocabKeys[v.Key] {
			add("%s: duplicate key %q", where, v.Key)
		}
		vocabKeys[v.Key] = true
		for field, val := range map[string]string{"simplified": v.Simplified, "pinyin": v.Pinyin} {
			if strings.TrimSpace(val) == "" {
				add("%s: %s is required", where, field)
			}
		}
		if len(v.Senses) == 0 {
			add("%s: at least one sense is required", where)
		}
		if len(v.Levels) == 0 {
			add("%s: levels must map at least one curriculum version to a level number", where)
		}
	}
	if len(f.Vocabulary) == 0 {
		add("vocabulary must not be empty: lessons reference vocabulary by key")
	}

	// Passages must be unique by (kind, title), which is the natural key used to
	// upsert them.
	passageByTitle := map[string]bool{}
	for i, p := range f.Passages {
		where := fmt.Sprintf("passages[%d] (%s)", i, p.Title)
		if p.Title == "" {
			add("%s: title is required", where)
		}
		if p.Kind != "reading" && p.Kind != "listening" {
			add("%s: kind must be reading or listening", where)
		}
		if strings.TrimSpace(p.Body) == "" {
			add("%s: body is required", where)
		}
		if len(p.Sentences) == 0 {
			add("%s: sentences must not be empty", where)
		}
		key := p.Kind + "/" + p.Title
		if passageByTitle[key] {
			add("%s: duplicate passage for this kind and title", where)
		}
		passageByTitle[key] = true
	}

	levelNumbers := map[int]bool{}
	for li, l := range f.Levels {
		where := fmt.Sprintf("levels[%d]", li)
		if l.Level <= 0 {
			add("%s: level must be a positive number", where)
		}
		if levelNumbers[l.Level] {
			add("%s: duplicate level %d", where, l.Level)
		}
		levelNumbers[l.Level] = true
		if l.Title == "" {
			add("%s: title is required", where)
		}
		switch l.Stage {
		case "", "elementary", "intermediate", "advanced":
		default:
			add("%s: stage must be elementary, intermediate or advanced", where)
		}

		unitNumbers := map[int]bool{}
		for ui, u := range l.Units {
			uwhere := fmt.Sprintf("%s units[%d]", where, ui)
			if u.Unit <= 0 {
				add("%s: unit must be a positive number", uwhere)
			}
			if unitNumbers[u.Unit] {
				add("%s: duplicate unit %d", uwhere, u.Unit)
			}
			unitNumbers[u.Unit] = true
			if u.Title == "" {
				add("%s: title is required", uwhere)
			}

			lessonNumbers := map[int]bool{}
			for si, s := range u.Lessons {
				swhere := fmt.Sprintf("%s lessons[%d]", uwhere, si)
				if s.Lesson <= 0 {
					add("%s: lesson must be a positive number", swhere)
				}
				if lessonNumbers[s.Lesson] {
					add("%s: duplicate lesson %d", swhere, s.Lesson)
				}
				lessonNumbers[s.Lesson] = true
				if s.Title == "" {
					add("%s: title is required", swhere)
				}
				switch s.Kind {
				case "", "lesson", "review", "assessment", "mock_exam":
				default:
					add("%s: kind must be lesson, review, assessment or mock_exam", swhere)
				}
				if len(s.Exercises) == 0 {
					add("%s: a lesson needs at least one exercise", swhere)
				}
				for _, key := range s.Vocabulary {
					if !vocabKeys[key] {
						add("%s: vocabulary key %q is not defined at the top level", swhere, key)
					}
				}
				for _, title := range s.Passages {
					found := false
					for _, p := range f.Passages {
						if p.Title == title {
							found = true
							break
						}
					}
					if !found {
						add("%s: passage %q is not defined at the top level", swhere, title)
					}
				}

				exKeys := map[string]bool{}
				for ei, e := range s.Exercises {
					ewhere := fmt.Sprintf("%s exercises[%d]", swhere, ei)
					key := e.Key
					if key == "" {
						key = fmt.Sprintf("ex-%d", ei+1)
					}
					if exKeys[key] {
						add("%s: duplicate exercise key %q", ewhere, key)
					}
					exKeys[key] = true

					switch e.Skill {
					case "vocabulary", "grammar", "reading", "listening", "writing", "speaking":
					default:
						add("%s: skill %q is not one of the six HSK skills", ewhere, e.Skill)
					}
					if e.Difficulty < 1 || e.Difficulty > 5 {
						add("%s: difficulty must be between 1 and 5", ewhere)
					}

					prompt, _, err := prepareExercisePrompt(e)
					if err != nil {
						add("%s: %v", ewhere, err)
						continue
					}
					promptJSON, err := json.Marshal(prompt)
					if err != nil {
						add("%s: prompt is not encodable: %v", ewhere, err)
						continue
					}
					answerJSON, err := json.Marshal(e.AnswerKey)
					if err != nil {
						add("%s: answer_key is not encodable: %v", ewhere, err)
						continue
					}
					// The exercise registry validates the payload shape, so a
					// mistake is a content error, never a runtime surprise.
					if err := exercises.Validate(e.Type, promptJSON, answerJSON); err != nil {
						add("%s: %v", ewhere, err)
					}
				}
			}
		}
	}
	if len(f.Levels) == 0 {
		add("levels must not be empty")
	}
	sort.Strings(problems)
	return problems
}

// Report summarises an import.
type Report struct {
	Curriculum     string   `json:"curriculum"`
	Levels         int      `json:"levels"`
	Units          int      `json:"units"`
	Lessons        int      `json:"lessons"`
	Lexemes        int      `json:"lexemes"`
	Exercises      int      `json:"exercises"`
	Passages       int      `json:"passages"`
	AudioGenerated int      `json:"audio_generated"`
	Listening      int      `json:"listening_exercises"`
	Speaking       int      `json:"speaking_exercises"`
	Warnings       []string `json:"warnings,omitempty"`
}

// Import upserts the document. Everything runs in one transaction: a content file
// that fails halfway leaves the database exactly as it was.
//
// Audio is generated before the exercise row is written, so a listening exercise
// cannot be stored without the asset it plays. A missing synthesiser therefore
// fails the import rather than producing a silent lesson — the invariant is
// machine-checked, not a convention.
func Import(ctx context.Context, st *store.Store, f *File, provisioner AudioProvisioner) (*Report, error) {
	if problems := f.Validate(); len(problems) > 0 {
		return nil, fmt.Errorf("curriculum: refusing to import invalid content:\n  - %s", strings.Join(problems, "\n  - "))
	}
	report := &Report{Curriculum: f.Version}

	err := st.Tx(ctx, func(tx pgx.Tx) error {
		curriculumID, err := upsertCurriculum(ctx, tx, f)
		if err != nil {
			return err
		}
		lexemeIDs, err := upsertVocabulary(ctx, tx, f)
		if err != nil {
			return err
		}
		report.Lexemes = len(lexemeIDs)

		passageIDs, err := upsertPassages(ctx, tx, f)
		if err != nil {
			return err
		}
		report.Passages = len(passageIDs)

		for _, level := range f.Levels {
			levelID, err := upsertLevel(ctx, tx, curriculumID, level)
			if err != nil {
				return err
			}
			report.Levels++
			for _, unit := range level.Units {
				unitID, err := upsertUnit(ctx, tx, levelID, unit)
				if err != nil {
					return err
				}
				report.Units++
				for _, lesson := range unit.Lessons {
					lessonID, err := upsertLesson(ctx, tx, unitID, lesson)
					if err != nil {
						return err
					}
					report.Lessons++

					// Reordering a lesson is a legitimate content edit, and the unique
					// (lesson_id, exercise_number) constraint makes it impossible to apply
					// row by row: moving an exercise to position 5 collides with whatever
					// still holds 5, and the import dies with a duplicate-key error.
					// Parking this lesson's numbers out of range first lets every row land
					// on its final number. It happens inside the import transaction, so no
					// reader can observe the temporary state.
					parked, err := parkExerciseNumbers(ctx, tx, lessonID)
					if err != nil {
						return err
					}

					authored := map[string]bool{}
					for i, ex := range lesson.Exercises {
						key := ex.Key
						if key == "" {
							key = fmt.Sprintf("ex-%d", i+1)
						}
						authored[key] = true

						prompt, audioText, err := prepareExercisePrompt(ex)
						if err != nil {
							return fmt.Errorf("curriculum: lesson %q, exercise %q: %w", lesson.Title, key, err)
						}
						if audioText != "" {
							audioKey, generated, err := media.EnsureAudio(ctx, provisioner.Store, provisioner.TTS, audioText)
							if err != nil {
								return fmt.Errorf("curriculum: audio for %q (lesson %q, exercise %q): %w",
									audioText, lesson.Title, key, err)
							}
							if generated {
								report.AudioGenerated++
							}
							prompt["audio_key"] = audioKey
						}

						if err := upsertExercise(ctx, tx, lessonID, i+1, key, ex, prompt, lexemeIDs, lesson.Vocabulary); err != nil {
							return err
						}
						report.Exercises++
						switch ex.Skill {
						case "listening":
							report.Listening++
						case "speaking":
							report.Speaking++
						}
					}
					// Exercises removed from the file are reported, never deleted:
					// deleting one would cascade away every attempt against it. They
					// also get their position back when it is still free, so an import
					// that does not reorder leaves them exactly where they were.
					if err := restoreOrphanNumbers(ctx, tx, lessonID, authored, parked); err != nil {
						return err
					}
					orphans, err := orphanExercises(ctx, tx, lessonID, authored)
					if err != nil {
						return err
					}
					if len(orphans) > 0 {
						report.Warnings = append(report.Warnings,
							fmt.Sprintf("lesson %q still has %d exercise(s) not present in the file (%s): they keep their learner attempts and were left untouched",
								lesson.Title, len(orphans), strings.Join(orphans, ", ")))
					}

					if err := linkPassages(ctx, tx, lessonID, lesson.Passages, passageIDs); err != nil {
						return err
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	return report, nil
}

func upsertCurriculum(ctx context.Context, tx pgx.Tx, f *File) (uuid.UUID, error) {
	var effective *time.Time
	if f.EffectiveFrom != "" {
		t, err := time.Parse("2006-01-02", f.EffectiveFrom)
		if err != nil {
			return uuid.Nil, err
		}
		effective = &t
	}
	status := f.Status
	if status == "" {
		status = "draft"
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO curricula (id, version, name, standard, stage_model, status, effective_from, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (version) DO UPDATE SET
			name = EXCLUDED.name, standard = EXCLUDED.standard, stage_model = EXCLUDED.stage_model,
			status = EXCLUDED.status, effective_from = EXCLUDED.effective_from, notes = EXCLUDED.notes,
			updated_at = now()
		RETURNING id`,
		uuid.New(), f.Version, f.Name, f.Standard, f.StageModel, status, effective, f.Notes).Scan(&id)
	return id, err
}

func upsertVocabulary(ctx context.Context, tx pgx.Tx, f *File) (map[string]uuid.UUID, error) {
	ids := make(map[string]uuid.UUID, len(f.Vocabulary))
	for _, v := range f.Vocabulary {
		senses, err := json.Marshal(v.Senses)
		if err != nil {
			return nil, fmt.Errorf("curriculum: encoding senses for %s: %w", v.Simplified, err)
		}
		traditional := v.Traditional
		if traditional == "" {
			traditional = v.Simplified
		}
		var audioKey *string
		if v.AudioKey != "" {
			audioKey = &v.AudioKey
		}
		var freqRank *int
		if v.FreqRank > 0 {
			freqRank = &v.FreqRank
		}
		pos := v.PartsOfSpeech
		if pos == nil {
			pos = []string{}
		}

		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO lexemes (id, simplified, traditional, pinyin, parts_of_speech, senses, audio_key, freq_rank)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			ON CONFLICT (simplified, pinyin) DO UPDATE SET
				traditional = EXCLUDED.traditional, parts_of_speech = EXCLUDED.parts_of_speech,
				senses = EXCLUDED.senses, audio_key = EXCLUDED.audio_key, freq_rank = EXCLUDED.freq_rank
			RETURNING id`,
			uuid.New(), v.Simplified, traditional, v.Pinyin, pos, senses, audioKey, freqRank).Scan(&id); err != nil {
			return nil, fmt.Errorf("curriculum: upserting lexeme %s: %w", v.Simplified, err)
		}
		ids[v.Key] = id

		for version, levelNumber := range v.Levels {
			var curriculumID uuid.UUID
			if err := tx.QueryRow(ctx, `SELECT id FROM curricula WHERE version = $1`, version).Scan(&curriculumID); err != nil {
				if err == pgx.ErrNoRows {
					// A word can be placed in a curriculum that is not being imported
					// in this file (for example hsk2.0 placement inside the hsk3.0 file);
					// skip quietly rather than failing the import.
					continue
				}
				return nil, err
			}
			if _, err := tx.Exec(ctx, `
				INSERT INTO lexeme_levels (lexeme_id, curriculum_id, level_number)
				VALUES ($1, $2, $3) ON CONFLICT DO NOTHING`, id, curriculumID, levelNumber); err != nil {
				return nil, err
			}
		}
	}
	return ids, nil
}

func upsertPassages(ctx context.Context, tx pgx.Tx, f *File) (map[string]uuid.UUID, error) {
	ids := make(map[string]uuid.UUID, len(f.Passages))
	for _, p := range f.Passages {
		sentences, err := json.Marshal(p.Sentences)
		if err != nil {
			return nil, fmt.Errorf("curriculum: encoding passage %s: %w", p.Title, err)
		}
		audioKey := optional(p.AudioKey)
		instruction := optional(p.Instruction)
		var id uuid.UUID
		if err := tx.QueryRow(ctx, `
			INSERT INTO passages (id, kind, curriculum_version, level_number, title, instruction, body, sentences, audio_key, word_count, minutes)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
			ON CONFLICT (curriculum_version, kind, title) DO UPDATE SET
				level_number = EXCLUDED.level_number, instruction = EXCLUDED.instruction, body = EXCLUDED.body,
				sentences = EXCLUDED.sentences, audio_key = EXCLUDED.audio_key,
				word_count = EXCLUDED.word_count, minutes = EXCLUDED.minutes
			RETURNING id`,
			uuid.New(), p.Kind, f.Version, p.Level, p.Title, instruction, p.Body, sentences, audioKey,
			p.WordCount, max(p.Minutes, 1)).Scan(&id); err != nil {
			return nil, fmt.Errorf("curriculum: upserting passage %s: %w", p.Title, err)
		}
		ids[p.Title] = id
	}
	return ids, nil
}

func upsertLevel(ctx context.Context, tx pgx.Tx, curriculumID uuid.UUID, l LevelDef) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO levels (id, curriculum_id, level_number, stage, title, description, vocab_target)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (curriculum_id, level_number) DO UPDATE SET
			stage = EXCLUDED.stage, title = EXCLUDED.title, description = EXCLUDED.description,
			vocab_target = EXCLUDED.vocab_target
		RETURNING id`,
		uuid.New(), curriculumID, l.Level, optional(l.Stage), l.Title, optional(l.Description),
		nullableInt(l.VocabTarget)).Scan(&id)
	return id, err
}

func upsertUnit(ctx context.Context, tx pgx.Tx, levelID uuid.UUID, u UnitDef) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO units (id, level_id, unit_number, title, description)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (level_id, unit_number) DO UPDATE SET
			title = EXCLUDED.title, description = EXCLUDED.description
		RETURNING id`,
		uuid.New(), levelID, u.Unit, u.Title, optional(u.Description)).Scan(&id)
	return id, err
}

func upsertLesson(ctx context.Context, tx pgx.Tx, unitID uuid.UUID, l LessonDef) (uuid.UUID, error) {
	kind := l.Kind
	if kind == "" {
		kind = "lesson"
	}
	xp := l.XPReward
	if xp <= 0 {
		xp = 10
	}
	minutes := l.EstimatedMinutes
	if minutes <= 0 {
		minutes = 8
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO lessons (id, unit_id, lesson_number, title, kind, goal, xp_reward, estimated_minutes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (unit_id, lesson_number) DO UPDATE SET
			title = EXCLUDED.title, kind = EXCLUDED.kind, goal = EXCLUDED.goal,
			xp_reward = EXCLUDED.xp_reward, estimated_minutes = EXCLUDED.estimated_minutes
		RETURNING id`,
		uuid.New(), unitID, l.Lesson, l.Title, kind, optional(l.Goal), xp, minutes).Scan(&id)
	return id, err
}

func upsertExercise(ctx context.Context, tx pgx.Tx, lessonID uuid.UUID, number int, key string, e ExerciseDef, prompt map[string]any, lexemeIDs map[string]uuid.UUID, lessonVocab []string) error {
	promptJSON, err := json.Marshal(prompt)
	if err != nil {
		return fmt.Errorf("curriculum: encoding prompt for %s/%s: %w", lessonID, key, err)
	}
	answerJSON, err := json.Marshal(e.AnswerKey)
	if err != nil {
		return fmt.Errorf("curriculum: encoding answer key for %s/%s: %w", lessonID, key, err)
	}
	var id uuid.UUID
	if err := tx.QueryRow(ctx, `
		INSERT INTO exercises (id, lesson_id, exercise_number, content_key, type, skill, difficulty, prompt, answer_key, explanation)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (lesson_id, content_key) WHERE content_key IS NOT NULL DO UPDATE SET
			exercise_number = EXCLUDED.exercise_number, type = EXCLUDED.type, skill = EXCLUDED.skill,
			difficulty = EXCLUDED.difficulty, prompt = EXCLUDED.prompt, answer_key = EXCLUDED.answer_key,
			explanation = EXCLUDED.explanation
		RETURNING id`,
		uuid.New(), lessonID, number, key, e.Type, e.Skill, e.Difficulty, promptJSON, answerJSON,
		optional(e.Explanation)).Scan(&id); err != nil {
		return fmt.Errorf("curriculum: upserting exercise %s: %w", key, err)
	}

	// Vocabulary links decide which words a lesson teaches, which in turn seeds the
	// spaced-repetition deck. They are metadata, so replacing them is safe.
	if _, err := tx.Exec(ctx, `DELETE FROM exercise_lexemes WHERE exercise_id = $1`, id); err != nil {
		return err
	}
	for _, vkey := range lessonVocab {
		lexemeID, ok := lexemeIDs[vkey]
		if !ok {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO exercise_lexemes (exercise_id, lexeme_id) VALUES ($1, $2)
			ON CONFLICT DO NOTHING`, id, lexemeID); err != nil {
			return err
		}
	}
	return nil
}

// parkedExercise remembers a row whose position was moved out of the way so a
// reorder could be applied.
type parkedExercise struct {
	id         uuid.UUID
	contentKey string
	number     int
}

// parkNumberOffset keeps parked positions far above any real one.
const parkNumberOffset = 100000

func parkExerciseNumbers(ctx context.Context, tx pgx.Tx, lessonID uuid.UUID) ([]parkedExercise, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, coalesce(content_key, ''), exercise_number
		FROM exercises WHERE lesson_id = $1 ORDER BY exercise_number`, lessonID)
	if err != nil {
		return nil, fmt.Errorf("curriculum: reading existing exercise positions: %w", err)
	}
	var parked []parkedExercise
	for rows.Next() {
		var p parkedExercise
		if err := rows.Scan(&p.id, &p.contentKey, &p.number); err != nil {
			rows.Close()
			return nil, fmt.Errorf("curriculum: reading existing exercise positions: %w", err)
		}
		parked = append(parked, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("curriculum: reading existing exercise positions: %w", err)
	}
	if len(parked) == 0 {
		return nil, nil
	}
	if _, err := tx.Exec(ctx,
		`UPDATE exercises SET exercise_number = exercise_number + $2 WHERE lesson_id = $1`,
		lessonID, parkNumberOffset); err != nil {
		return nil, fmt.Errorf("curriculum: parking exercise positions: %w", err)
	}
	return parked, nil
}

// restoreOrphanNumbers gives back the position of every exercise that is no longer
// in the content file, so "left untouched" stays true for the usual case where the
// file did not reorder. When its old position has been taken by an authored
// exercise, the orphan moves to the end of the lesson rather than colliding.
func restoreOrphanNumbers(ctx context.Context, tx pgx.Tx, lessonID uuid.UUID, authored map[string]bool, parked []parkedExercise) error {
	used := map[int]bool{}
	rows, err := tx.Query(ctx, `SELECT exercise_number FROM exercises WHERE lesson_id = $1`, lessonID)
	if err != nil {
		return fmt.Errorf("curriculum: reading assigned positions: %w", err)
	}
	highest := 0
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return fmt.Errorf("curriculum: reading assigned positions: %w", err)
		}
		if n < parkNumberOffset {
			used[n] = true
			if n > highest {
				highest = n
			}
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return fmt.Errorf("curriculum: reading assigned positions: %w", err)
	}

	for _, p := range parked {
		if p.contentKey != "" && authored[p.contentKey] {
			continue // this one was renumbered by the file, on purpose
		}
		target := p.number
		if used[target] {
			highest++
			target = highest
		}
		used[target] = true
		if _, err := tx.Exec(ctx,
			`UPDATE exercises SET exercise_number = $2 WHERE id = $1`, p.id, target); err != nil {
			return fmt.Errorf("curriculum: restoring position %d for an exercise no longer in the file: %w", p.number, err)
		}
	}
	return nil
}

func orphanExercises(ctx context.Context, tx pgx.Tx, lessonID uuid.UUID, authored map[string]bool) ([]string, error) {
	rows, err := tx.Query(ctx, `
		SELECT content_key FROM exercises
		WHERE lesson_id = $1 AND content_key IS NOT NULL
		ORDER BY exercise_number`, lessonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orphans []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, err
		}
		if !authored[key] {
			orphans = append(orphans, key)
		}
	}
	return orphans, rows.Err()
}

func linkPassages(ctx context.Context, tx pgx.Tx, lessonID uuid.UUID, titles []string, passageIDs map[string]uuid.UUID) error {
	if _, err := tx.Exec(ctx, `DELETE FROM lesson_passages WHERE lesson_id = $1`, lessonID); err != nil {
		return err
	}
	for i, title := range titles {
		id, ok := passageIDs[title]
		if !ok {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO lesson_passages (lesson_id, passage_id, position) VALUES ($1, $2, $3)
			ON CONFLICT (lesson_id, passage_id) DO UPDATE SET position = EXCLUDED.position`,
			lessonID, id, i+1); err != nil {
			return err
		}
	}
	return nil
}

func optional(s string) *string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return &s
}

func nullableInt(n int) *int {
	if n <= 0 {
		return nil
	}
	return &n
}
