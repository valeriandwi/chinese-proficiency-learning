-- +goose Up
-- +goose StatementBegin

-- Curriculum: versioned HSK curricula as data.
-- A curriculum version (hsk2.0 = 6 levels, hsk3.0 = 3 stages / 9 levels) owns
-- levels -> units -> lessons -> exercises. Nothing about a syllabus is hardcoded
-- in Go; adding HSK 3.0's nine levels is a data import, not a code change.

CREATE TABLE curricula (
    id            uuid PRIMARY KEY,
    version       text        NOT NULL UNIQUE,
    name          text        NOT NULL,
    standard      text        NOT NULL,
    stage_model   text        NOT NULL,
    status        text        NOT NULL DEFAULT 'draft',
    effective_from date,
    notes         text,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT curricula_status_check CHECK (status IN ('draft', 'published', 'archived')),
    CONSTRAINT curricula_stage_model_check CHECK (stage_model IN ('six-levels', 'three-stages-nine-levels'))
);

CREATE TABLE levels (
    id            uuid PRIMARY KEY,
    curriculum_id uuid    NOT NULL REFERENCES curricula (id) ON DELETE CASCADE,
    level_number  int     NOT NULL,
    stage         text,
    title         text    NOT NULL,
    description   text,
    vocab_target  int,
    UNIQUE (curriculum_id, level_number),
    CONSTRAINT levels_stage_check CHECK (stage IS NULL OR stage IN ('elementary', 'intermediate', 'advanced'))
);

CREATE TABLE units (
    id           uuid PRIMARY KEY,
    level_id     uuid    NOT NULL REFERENCES levels (id) ON DELETE CASCADE,
    unit_number  int     NOT NULL,
    title        text    NOT NULL,
    description  text,
    UNIQUE (level_id, unit_number)
);

CREATE TABLE lessons (
    id                 uuid PRIMARY KEY,
    unit_id            uuid    NOT NULL REFERENCES units (id) ON DELETE CASCADE,
    lesson_number      int     NOT NULL,
    title              text    NOT NULL,
    kind               text    NOT NULL DEFAULT 'lesson',
    goal               text,
    xp_reward          int     NOT NULL DEFAULT 10,
    estimated_minutes  int     NOT NULL DEFAULT 8,
    UNIQUE (unit_id, lesson_number),
    CONSTRAINT lessons_kind_check CHECK (kind IN ('lesson', 'review', 'assessment', 'mock_exam'))
);

CREATE INDEX lessons_unit_idx ON lessons (unit_id, lesson_number);

-- Global lexeme table: a word exists once, its HSK placement is versioned.
CREATE TABLE lexemes (
    id            uuid PRIMARY KEY,
    simplified    text NOT NULL,
    traditional   text NOT NULL,
    pinyin        text NOT NULL,
    parts_of_speech text[] NOT NULL DEFAULT '{}',
    senses        jsonb NOT NULL,
    audio_key     text,
    freq_rank     int,
    created_at    timestamptz NOT NULL DEFAULT now(),
    UNIQUE (simplified, pinyin)
);

CREATE INDEX lexemes_simplified_idx ON lexemes (simplified);

CREATE TABLE lexeme_levels (
    lexeme_id     uuid NOT NULL REFERENCES lexemes (id) ON DELETE CASCADE,
    curriculum_id uuid NOT NULL REFERENCES curricula (id) ON DELETE CASCADE,
    level_number  int  NOT NULL,
    PRIMARY KEY (lexeme_id, curriculum_id, level_number)
);

CREATE INDEX lexeme_levels_curriculum_level_idx ON lexeme_levels (curriculum_id, level_number);

-- Exercises: typed payload + answer key.
-- answer_key is server-only state and is never serialised to the client before a
-- submission is graded; see internal/httpapi/lesson_handlers.go.
CREATE TABLE exercises (
    id               uuid PRIMARY KEY,
    lesson_id        uuid    NOT NULL REFERENCES lessons (id) ON DELETE CASCADE,
    exercise_number  int     NOT NULL,
    type             text    NOT NULL,
    skill            text    NOT NULL,
    difficulty       int     NOT NULL DEFAULT 1,
    prompt           jsonb   NOT NULL,
    answer_key       jsonb   NOT NULL,
    explanation      text,
    UNIQUE (lesson_id, exercise_number),
    CONSTRAINT exercises_skill_check CHECK (skill IN ('vocabulary', 'grammar', 'reading', 'listening', 'writing', 'speaking')),
    CONSTRAINT exercises_difficulty_check CHECK (difficulty BETWEEN 1 AND 5)
);

CREATE INDEX exercises_lesson_idx ON exercises (lesson_id, exercise_number);

CREATE TABLE exercise_lexemes (
    exercise_id uuid NOT NULL REFERENCES exercises (id) ON DELETE CASCADE,
    lexeme_id   uuid NOT NULL REFERENCES lexemes (id) ON DELETE CASCADE,
    PRIMARY KEY (exercise_id, lexeme_id)
);

CREATE TABLE passages (
    id           uuid PRIMARY KEY,
    kind         text NOT NULL,
    curriculum_version text NOT NULL,
    level_number int  NOT NULL,
    title        text NOT NULL,
    instruction  text,
    body         text NOT NULL,
    sentences    jsonb NOT NULL,
    audio_key    text,
    word_count   int  NOT NULL DEFAULT 0,
    minutes      int  NOT NULL DEFAULT 3,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT passages_kind_check CHECK (kind IN ('reading', 'listening'))
);

CREATE INDEX passages_version_level_idx ON passages (curriculum_version, level_number, kind);

CREATE TABLE lesson_passages (
    lesson_id  uuid NOT NULL REFERENCES lessons (id) ON DELETE CASCADE,
    passage_id uuid NOT NULL REFERENCES passages (id) ON DELETE CASCADE,
    position   int  NOT NULL DEFAULT 1,
    PRIMARY KEY (lesson_id, passage_id)
);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS lesson_passages;
DROP TABLE IF EXISTS passages;
DROP TABLE IF EXISTS exercise_lexemes;
DROP TABLE IF EXISTS exercises;
DROP TABLE IF EXISTS lexeme_levels;
DROP TABLE IF EXISTS lexemes;
DROP TABLE IF EXISTS lessons;
DROP TABLE IF EXISTS units;
DROP TABLE IF EXISTS levels;
DROP TABLE IF EXISTS curricula;
-- +goose StatementEnd
