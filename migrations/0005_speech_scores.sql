-- +goose Up
-- Pronunciation scores for speaking drills.
--
-- Retention: the recording itself is NEVER stored. The upload endpoint reads the
-- audio into memory, hands it to the configured scorer and drops it — only these
-- numbers survive. That is a deliberate promise to the learner (the UI says so
-- before they record), so there is no audio column here and nothing to purge later.
--
-- A row exists before its attempt does: the client scores a recording, then submits
-- the attempt referencing it by id. Scoring is therefore server-derived — a client
-- can name a recording it owns, but it can never send us a score.
CREATE TABLE speaking_scores (
    id           uuid PRIMARY KEY,
    user_id      uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    exercise_id  uuid NOT NULL REFERENCES exercises (id) ON DELETE CASCADE,
    attempt_id   uuid REFERENCES attempts (id) ON DELETE SET NULL,
    provider     text NOT NULL,
    model        text NOT NULL DEFAULT '',
    overall      numeric(5,4) NOT NULL,
    accuracy     numeric(5,4),
    fluency      numeric(5,4),
    completeness numeric(5,4),
    prosody      numeric(5,4),
    words        jsonb NOT NULL DEFAULT '[]'::jsonb,
    duration_ms  int NOT NULL DEFAULT 0,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT speaking_scores_overall_check CHECK (overall >= 0 AND overall <= 1)
);

-- Ownership lookups (grading resolves a recording for one learner and one exercise).
CREATE INDEX speaking_scores_user_exercise_idx ON speaking_scores (user_id, exercise_id);
CREATE INDEX speaking_scores_attempt_idx ON speaking_scores (attempt_id) WHERE attempt_id IS NOT NULL;
-- The daily per-user cap counts rows created in a window.
CREATE INDEX speaking_scores_user_created_idx ON speaking_scores (user_id, created_at DESC);

-- +goose Down
DROP TABLE speaking_scores;
