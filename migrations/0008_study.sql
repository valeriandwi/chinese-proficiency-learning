-- +goose Up
-- The study surfaces: the writing studio, the speaking studio, the passage reader,
-- and the study guide (vocabulary, grammar, bookmarks).
--
-- Attempts from these surfaces are recorded with mode "studio", the same contract
-- as "tone" (migrations/0006) and "proficiency" (migrations/0007): they are ordinary
-- rows in the append-only attempt log — the record survives — but they are excluded
-- wherever derived state is computed, because the studios serve content from levels
-- the learner has not reached. Without the exclusion, practising a character from a
-- locked level's lesson would flip that lesson to in_progress and let practice erode
-- the linear path. The cost is stated in the UI: studio practice earns no XP, no
-- mastery and no streak credit.

ALTER TABLE attempts DROP CONSTRAINT attempts_mode_check;

ALTER TABLE attempts ADD CONSTRAINT attempts_mode_check
    CHECK (mode IN ('practice', 'review', 'exam', 'placement', 'tone', 'proficiency', 'studio'));

-- Bookmarks: a learner's own list of vocabulary to come back to. One row per
-- (learner, word), so tapping the bookmark twice is a toggle, not a duplicate —
-- the POST is an upsert and DELETE is idempotent by construction.
CREATE TABLE bookmarks (
    id         uuid PRIMARY KEY,
    user_id    uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    lexeme_id  uuid NOT NULL REFERENCES lexemes (id) ON DELETE CASCADE,
    created_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT bookmarks_once UNIQUE (user_id, lexeme_id)
);

CREATE INDEX bookmarks_user_created_idx ON bookmarks (user_id, created_at DESC);

-- +goose Down
ALTER TABLE attempts DROP CONSTRAINT attempts_mode_check;

ALTER TABLE attempts ADD CONSTRAINT attempts_mode_check
    CHECK (mode IN ('practice', 'review', 'exam', 'placement', 'tone', 'proficiency'));

DROP TABLE bookmarks;
