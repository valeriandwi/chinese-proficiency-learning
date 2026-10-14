-- +goose Up
-- +goose StatementBegin

-- Content identity.
--
-- Content is re-imported from YAML whenever authors change a lesson. Attempts
-- reference exercises with ON DELETE CASCADE, so a naive "delete the lesson and
-- reinsert" import would silently destroy learner history. Instead every exercise
-- carries a stable content_key within its lesson and the importer upserts on it:
-- exercise ids (and therefore attempts, reviews and progress) survive re-import.

ALTER TABLE exercises ADD COLUMN content_key text;

CREATE UNIQUE INDEX exercises_content_key_idx
    ON exercises (lesson_id, content_key)
    WHERE content_key IS NOT NULL;

-- Passages are authored per curriculum version and level; a title is the stable
-- natural key so re-import updates the text instead of duplicating it.
CREATE UNIQUE INDEX passages_natural_key_idx
    ON passages (curriculum_version, kind, title);

-- Lesson natural keys are already unique ((unit_id, lesson_number)), as are units
-- ((level_id, unit_number)) and levels ((curriculum_id, level_number)), so the
-- importer can upsert the whole tree without deleting anything.

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS passages_natural_key_idx;
DROP INDEX IF EXISTS exercises_content_key_idx;
ALTER TABLE exercises DROP COLUMN IF EXISTS content_key;
-- +goose StatementEnd
