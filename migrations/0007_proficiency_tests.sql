-- +goose Up
-- Proficiency tests: a scored paper that can open a level.
--
-- Placement is a self-report heuristic — it recommends a level and says so. This is its
-- scored counterpart: a learner who already knows HSK 3 takes twenty questions drawn
-- across that level's units and, at 80% or better, level 3 opens.
--
-- What an unlock is NOT: it is not completion. No lesson is marked done, no XP is
-- awarded, no mastery moves. A test-opened level's lessons are AVAILABLE, and the record
-- of how they became available says a test did it — which is the difference between
-- "you can start here" and "you already learned this".
CREATE TABLE level_unlocks (
    id          uuid PRIMARY KEY,
    user_id     uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    level_id    uuid NOT NULL REFERENCES levels (id) ON DELETE CASCADE,
    paper_id    uuid NOT NULL,
    score       numeric(5,4) NOT NULL,
    unlocked_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT level_unlocks_score_check CHECK (score >= 0 AND score <= 1),
    -- One unlock per level: the first pass is the one that counts, and a retake cannot
    -- produce a second row.
    CONSTRAINT level_unlocks_once UNIQUE (user_id, level_id)
);

-- A paper is DRAWN by the server and stored before it is sat, so the submission can be
-- checked against it. Without that, a client could submit the twenty questions it knows
-- best and pass a paper it was never given — the same shape of hole as letting a client
-- send its own score for a recording.
--
-- A row exists in two states: drawn (submitted_at NULL) and sat (scored). A failed paper
-- is kept: it is what the learner's history shows and what a retake is measured against.
CREATE TABLE proficiency_papers (
    id            uuid PRIMARY KEY,
    user_id       uuid NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    level_id      uuid NOT NULL REFERENCES levels (id) ON DELETE CASCADE,
    seed          text NOT NULL,
    exercise_ids  uuid[] NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    submitted_at  timestamptz,
    score         numeric(5,4),
    passed        boolean,
    graded_count  int,
    correct_count int,
    CONSTRAINT proficiency_papers_answers_check CHECK (cardinality(exercise_ids) > 0),
    -- Drawn or sat, never half of each.
    CONSTRAINT proficiency_papers_state_check CHECK (
        (submitted_at IS NULL AND score IS NULL AND passed IS NULL AND graded_count IS NULL AND correct_count IS NULL)
        OR
        (submitted_at IS NOT NULL AND score IS NOT NULL AND passed IS NOT NULL AND graded_count IS NOT NULL AND correct_count IS NOT NULL)
    ),
    CONSTRAINT proficiency_papers_score_check CHECK (score IS NULL OR (score >= 0 AND score <= 1)),
    CONSTRAINT proficiency_papers_counts_check CHECK (graded_count IS NULL OR (graded_count > 0 AND correct_count >= 0 AND correct_count <= graded_count))
);

CREATE INDEX proficiency_papers_user_level_idx ON proficiency_papers (user_id, level_id, created_at DESC);

-- A paper's answers are ordinary attempts, recorded with their own mode so they can be
-- excluded from the path's derived state: a paper samples locked lessons by design, and
-- recomputing lesson progress from it would open those lessons without a pass.
ALTER TABLE attempts DROP CONSTRAINT attempts_mode_check;

ALTER TABLE attempts ADD CONSTRAINT attempts_mode_check
    CHECK (mode IN ('practice', 'review', 'exam', 'placement', 'tone', 'proficiency'));

-- +goose Down
ALTER TABLE attempts DROP CONSTRAINT attempts_mode_check;

ALTER TABLE attempts ADD CONSTRAINT attempts_mode_check
    CHECK (mode IN ('practice', 'review', 'exam', 'placement', 'tone'));

DROP TABLE proficiency_papers;
DROP TABLE level_unlocks;
