-- +goose Up
-- +goose StatementBegin

-- Identity: users, sessions, single-use auth tokens.
-- Notes:
--   * email uniqueness is case-insensitive via a functional index (no citext dependency).
--   * session tokens are never stored in plaintext: only sha256(token) is persisted.

CREATE TABLE users (
    id                    uuid PRIMARY KEY,
    email                 text        NOT NULL,
    display_name          text        NOT NULL,
    password_hash         text        NOT NULL,
    role                  text        NOT NULL DEFAULT 'learner',
    email_verified_at     timestamptz,
    timezone              text        NOT NULL DEFAULT 'Asia/Jakarta',
    daily_goal_value      int         NOT NULL DEFAULT 20,
    daily_goal_kind       text        NOT NULL DEFAULT 'minutes',
    pinyin_default        text        NOT NULL DEFAULT 'always',
    target_curriculum     text,
    target_level          int,
    placement_completed_at timestamptz,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_role_check        CHECK (role IN ('learner', 'admin')),
    CONSTRAINT users_goal_kind_check   CHECK (daily_goal_kind IN ('minutes', 'xp', 'lessons')),
    CONSTRAINT users_goal_value_check  CHECK (daily_goal_value > 0 AND daily_goal_value <= 600),
    CONSTRAINT users_pinyin_check      CHECK (pinyin_default IN ('always', 'on_tap', 'never')),
    CONSTRAINT users_target_level_check CHECK (target_level IS NULL OR (target_level >= 1 AND target_level <= 9))
);

CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

CREATE TABLE sessions (
    id           uuid PRIMARY KEY,
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL UNIQUE,
    csrf_token   text        NOT NULL,
    user_agent   text,
    ip           inet,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL,
    revoked_at   timestamptz
);

CREATE INDEX sessions_active_user_idx ON sessions (user_id) WHERE revoked_at IS NULL;
CREATE INDEX sessions_expiry_idx      ON sessions (expires_at) WHERE revoked_at IS NULL;

CREATE TABLE auth_tokens (
    id         uuid PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    kind       text        NOT NULL,
    token_hash bytea       NOT NULL UNIQUE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT auth_tokens_kind_check CHECK (kind IN ('email_verify', 'password_reset'))
);

CREATE INDEX auth_tokens_user_kind_idx ON auth_tokens (user_id, kind);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS auth_tokens;
DROP TABLE IF EXISTS sessions;
DROP TABLE IF EXISTS users;
-- +goose StatementEnd
