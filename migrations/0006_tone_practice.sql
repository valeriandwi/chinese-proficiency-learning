-- +goose Up
-- Tone practice: a drill on tone alone, outside the path.
--
-- A tone question is self-contained — it asks about one word's tones, needs no lesson
-- around it, and is exactly what a learner wants to drill when their tones are the
-- problem. So the tone page serves drills from anywhere in the curriculum, including
-- levels the learner has not reached.
--
-- That is why this mode exists. RecomputeProgress derives lesson status, XP, mastery,
-- the streak and the review deck from attempts, and it does so with no mode filter
-- before this migration: an ordinary attempt against a drill from a LOCKED lesson
-- flips that lesson to in_progress and counts toward completing it, so practising tone
-- would quietly erode the linear path. A tone attempt is recorded with mode = 'tone'
-- and the derivation excludes it: the attempt log keeps the learner's tone record, and
-- nothing about the path moves.
ALTER TABLE attempts DROP CONSTRAINT attempts_mode_check;

ALTER TABLE attempts ADD CONSTRAINT attempts_mode_check
    CHECK (mode IN ('practice', 'review', 'exam', 'placement', 'tone'));

-- +goose Down
ALTER TABLE attempts DROP CONSTRAINT attempts_mode_check;

ALTER TABLE attempts ADD CONSTRAINT attempts_mode_check
    CHECK (mode IN ('practice', 'review', 'exam', 'placement'));
