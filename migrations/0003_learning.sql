-- +goose Up
-- +goose StatementBegin

-- Learning state.
-- attempts is an append-only event log and the single source of truth for every
-- derived value below it (lesson_progress, skill_mastery, daily_activity,
-- user_streaks, review_items). Derived rows carry recomputed_at so a stale
-- aggregate is detectable, and are recomputed from attempts rather than patched
-- incrementally.

CREATE TABLE attempts (
    id           uuid PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    exercise_id  uuid        NOT NULL REFERENCES exercises (id) ON DELETE CASCADE,
    mode         text        NOT NULL DEFAULT 'practice',
    client_ref   text,
    response     jsonb       NOT NULL,
    is_correct   boolean,
    score        numeric(5, 4),
    duration_ms  int,
    grader       text        NOT NULL,
    feedback     jsonb,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT attempts_mode_check CHECK (mode IN ('practice', 'review', 'exam', 'placement')),
    CONSTRAINT attempts_score_check CHECK (score IS NULL OR (score >= 0 AND score <= 1))
);

CREATE INDEX attempts_user_created_idx  ON attempts (user_id, created_at DESC);
CREATE INDEX attempts_user_exercise_idx ON attempts (user_id, exercise_id, created_at DESC);
-- Idempotency: a retried batch submit must not double-count XP or SRS reps.
CREATE UNIQUE INDEX attempts_client_ref_key ON attempts (user_id, client_ref) WHERE client_ref IS NOT NULL;

CREATE TABLE lesson_progress (
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    lesson_id       uuid NOT NULL REFERENCES lessons (id) ON DELETE CASCADE,
    status          text NOT NULL DEFAULT 'in_progress',
    attempts_count  int  NOT NULL DEFAULT 0,
    correct_count   int  NOT NULL DEFAULT 0,
    best_score      numeric(5, 4),
    xp_awarded      int  NOT NULL DEFAULT 0,
    first_started_at timestamptz,
    last_attempt_at timestamptz,
    completed_at    timestamptz,
    recomputed_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, lesson_id),
    CONSTRAINT lesson_progress_status_check CHECK (status IN ('in_progress', 'completed'))
);

CREATE TABLE skill_mastery (
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    skill         text NOT NULL,
    attempts_count int NOT NULL DEFAULT 0,
    correct_count  int NOT NULL DEFAULT 0,
    mastery       numeric(5, 4) NOT NULL DEFAULT 0,
    last_attempt_at timestamptz,
    recomputed_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, skill)
);

-- Spaced repetition. interval_index walks an interval ladder; the column layout is
-- deliberately FSRS-compatible so the scheduler implementation can be swapped
-- without a migration.
CREATE TABLE review_items (
    id             uuid PRIMARY KEY,
    user_id        uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    lexeme_id      uuid NOT NULL REFERENCES lexemes (id) ON DELETE CASCADE,
    interval_index int  NOT NULL DEFAULT 0,
    ease           numeric(4, 2) NOT NULL DEFAULT 2.50,
    reps           int  NOT NULL DEFAULT 0,
    lapses         int  NOT NULL DEFAULT 0,
    due_at         timestamptz NOT NULL,
    last_result    text,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now(),
    UNIQUE (user_id, lexeme_id)
);

CREATE INDEX review_items_due_idx ON review_items (user_id, due_at);

CREATE TABLE xp_events (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    amount     int  NOT NULL,
    source     text NOT NULL,
    ref_id     uuid,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT xp_events_amount_check CHECK (amount <> 0)
);

CREATE UNIQUE INDEX xp_events_dedupe_idx ON xp_events (user_id, source, ref_id) WHERE ref_id IS NOT NULL;

-- day is the user's LOCAL calendar day, resolved at write time from users.timezone.
CREATE TABLE daily_activity (
    user_id             uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    day                 date NOT NULL,
    xp                  int  NOT NULL DEFAULT 0,
    minutes             int  NOT NULL DEFAULT 0,
    attempts_count      int  NOT NULL DEFAULT 0,
    correct_count       int  NOT NULL DEFAULT 0,
    lessons_completed   int  NOT NULL DEFAULT 0,
    goal_met            boolean NOT NULL DEFAULT false,
    recomputed_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, day)
);

CREATE TABLE user_streaks (
    user_id         uuid PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    current_streak  int  NOT NULL DEFAULT 0,
    longest_streak  int  NOT NULL DEFAULT 0,
    last_active_day date,
    freezes         int  NOT NULL DEFAULT 0,
    recomputed_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE achievements (
    key         text PRIMARY KEY,
    title       text NOT NULL,
    description text NOT NULL,
    icon        text NOT NULL DEFAULT 'star',
    criteria    jsonb NOT NULL
);

CREATE TABLE user_achievements (
    user_id         uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    achievement_key text NOT NULL REFERENCES achievements (key) ON DELETE CASCADE,
    unlocked_at     timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, achievement_key)
);

-- Postgres-backed job queue (SELECT ... FOR UPDATE SKIP LOCKED). Chosen over Redis
-- for the MVP: one less stateful dependency at this scale.
CREATE TABLE jobs (
    id           uuid PRIMARY KEY,
    kind         text NOT NULL,
    payload      jsonb NOT NULL DEFAULT '{}'::jsonb,
    status       text NOT NULL DEFAULT 'pending',
    run_at       timestamptz NOT NULL DEFAULT now(),
    attempts     int NOT NULL DEFAULT 0,
    max_attempts int NOT NULL DEFAULT 5,
    locked_by    text,
    locked_at    timestamptz,
    last_error   text,
    created_at   timestamptz NOT NULL DEFAULT now(),
    finished_at  timestamptz,
    CONSTRAINT jobs_status_check CHECK (status IN ('pending', 'running', 'done', 'failed'))
);

CREATE INDEX jobs_claim_idx ON jobs (run_at) WHERE status = 'pending';
CREATE UNIQUE INDEX jobs_dedupe_idx ON jobs (kind, (payload -> 'dedupe_key')) WHERE status = 'pending';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS jobs;
DROP TABLE IF EXISTS user_achievements;
DROP TABLE IF EXISTS achievements;
DROP TABLE IF EXISTS user_streaks;
DROP TABLE IF EXISTS daily_activity;
DROP TABLE IF EXISTS xp_events;
DROP TABLE IF EXISTS review_items;
DROP TABLE IF EXISTS skill_mastery;
DROP TABLE IF EXISTS lesson_progress;
DROP TABLE IF EXISTS attempts;
-- +goose StatementEnd
