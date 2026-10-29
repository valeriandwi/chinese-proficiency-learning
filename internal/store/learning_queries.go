package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/valeriandwipurnomo/hanpath/internal/progress"
)

// querier is satisfied by both *pgxpool.Pool and pgx.Tx, so the recompute routines
// can run inside a caller's transaction or standalone.
type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Tx exposes a transaction to the attempt service, which needs grading and
// persistence to be atomic.
func (s *Store) Tx(ctx context.Context, fn func(pgx.Tx) error) error {
	return s.withTx(ctx, fn)
}

// AttemptInsert is a graded attempt ready to be persisted.
type AttemptInsert struct {
	UserID     uuid.UUID
	ExerciseID uuid.UUID
	Mode       string
	ClientRef  string
	Grader     string
	Response   json.RawMessage
	Feedback   json.RawMessage
	IsCorrect  *bool
	Score      *float64
	DurationMs *int
}

// ErrClientRefConflict means a client_ref that already exists was reused for a
// different exercise. Silently treating that as a replay would discard a real
// answer, so it is reported instead.
var ErrClientRefConflict = errors.New("store: client_ref was already used for a different exercise")

// InsertAttemptTx records one attempt. The returned bool is false when the
// (user, client_ref) pair already existed, which makes a retried batch submit
// idempotent instead of double-counting XP and review reps.
func InsertAttemptTx(ctx context.Context, tx pgx.Tx, in AttemptInsert) (uuid.UUID, bool, error) {
	var clientRef any
	if in.ClientRef != "" {
		clientRef = in.ClientRef
	}
	var id uuid.UUID
	err := tx.QueryRow(ctx, `
		INSERT INTO attempts (id, user_id, exercise_id, mode, client_ref, response, is_correct, score, duration_ms, grader, feedback)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (user_id, client_ref) WHERE client_ref IS NOT NULL DO NOTHING
		RETURNING id`,
		uuid.New(), in.UserID, in.ExerciseID, in.Mode, clientRef, in.Response,
		in.IsCorrect, in.Score, in.DurationMs, in.Grader, in.Feedback).Scan(&id)
	if err == pgx.ErrNoRows {
		// The conflict was with an existing row: it is a genuine replay only if it
		// refers to the same exercise.
		var existing uuid.UUID
		if qerr := tx.QueryRow(ctx, `
			SELECT exercise_id FROM attempts WHERE user_id = $1 AND client_ref = $2`,
			in.UserID, in.ClientRef).Scan(&existing); qerr != nil {
			return uuid.Nil, false, mapErr(qerr)
		}
		if existing != in.ExerciseID {
			return uuid.Nil, false, fmt.Errorf("%w (client_ref %q)", ErrClientRefConflict, in.ClientRef)
		}
		return uuid.Nil, false, nil
	}
	if err != nil {
		return uuid.Nil, false, mapErr(err)
	}
	return id, true, nil
}

// RecomputeProgress rebuilds every derived value for a learner from the attempt
// log. This is the single recompute path: the attempt service calls it after a
// submission, and a "refresh" endpoint calls it on demand, so a stale aggregate
// is always one call away from being correct.
//
// Order matters and is deliberate:
// onPathAttempt is the predicate for "counts toward the path". Two kinds of attempt are
// recorded in the same table so the learner's own record survives, and are deliberately
// not part of the path: tone practice and proficiency papers. Both serve content the
// learner has not reached — a tone drill from any level, a paper drawn across a level
// nobody has walked — so neither may move lesson status, XP, mastery, the daily rollup
// the streak is derived from, or the answer totals the dashboard shows. One predicate,
// applied wherever this file reads attempts, keeps that one idea in one place.
// onPathAttempt is the predicate that decides which attempts a derived value is
// computed from. tone, proficiency and studio practice are all off the path by
// design (migrations 0006–0008): they serve content from levels the learner has
// not reached, so counting them would let practice erode the linear path.
const onPathAttempt = `a.mode NOT IN ('tone', 'proficiency', 'studio')`

// 1. lesson_progress  (from attempts)
// 2. xp_events        (from completed lessons; deduplicated by ref_id)
// 3. review_items     (deck seeded from completed lessons)
// 4. skill_mastery    (from attempts)
// 5. daily_activity   (local calendar days, using the learner's timezone)
// 6. user_streaks     (from daily_activity, computed in Go)
func RecomputeProgress(ctx context.Context, q querier, userID uuid.UUID) error {
	// 1. lesson_progress
	if _, err := q.Exec(ctx, `
		WITH per_exercise AS (
			SELECT e.lesson_id,
			       a.exercise_id,
			       max(a.score)                       AS best,
			       bool_or(a.is_correct)              AS ever_correct,
			       count(*)                           AS n,
			       min(a.created_at)                  AS first_at,
			       max(a.created_at)                  AS last_at
			FROM attempts a
			JOIN exercises e ON e.id = a.exercise_id
			WHERE a.user_id = $1::uuid AND `+onPathAttempt+`
			GROUP BY e.lesson_id, a.exercise_id
		),
		per_lesson AS (
			SELECT p.lesson_id,
			       sum(p.n)::int                                        AS attempts_count,
			       count(*) FILTER (WHERE p.ever_correct)::int          AS correct_count,
			       avg(p.best)::numeric(5,4)                            AS best_score,
			       min(p.first_at)                                      AS first_at,
			       max(p.last_at)                                       AS last_at,
			       (count(*) = (SELECT count(*) FROM exercises ex WHERE ex.lesson_id = p.lesson_id)
			        AND bool_and(coalesce(p.best, 0) >= 0.7))           AS completed
			FROM per_exercise p
			GROUP BY p.lesson_id
		)
		INSERT INTO lesson_progress (user_id, lesson_id, status, attempts_count, correct_count, best_score,
		                             first_started_at, last_attempt_at, completed_at, recomputed_at)
		SELECT $1::uuid, lesson_id,
		       CASE WHEN completed THEN 'completed' ELSE 'in_progress' END,
		       attempts_count, correct_count, best_score, first_at, last_at,
		       CASE WHEN completed THEN last_at ELSE NULL END, now()
		FROM per_lesson
		ON CONFLICT (user_id, lesson_id) DO UPDATE SET
			status          = EXCLUDED.status,
			attempts_count  = EXCLUDED.attempts_count,
			correct_count   = EXCLUDED.correct_count,
			best_score      = EXCLUDED.best_score,
			first_started_at = least(lesson_progress.first_started_at, EXCLUDED.first_started_at),
			last_attempt_at = EXCLUDED.last_attempt_at,
			completed_at    = coalesce(lesson_progress.completed_at, EXCLUDED.completed_at),
			recomputed_at   = now()`, userID); err != nil {
		return mapErr(err)
	}

	// 2. XP for lessons that are complete. The partial unique index on
	// (user_id, source, ref_id) makes this safe to run repeatedly.
	if _, err := q.Exec(ctx, `
		INSERT INTO xp_events (id, user_id, amount, source, ref_id)
		SELECT gen_random_uuid(), $1::uuid, ls.xp_reward, 'lesson_complete', ls.id
		FROM lesson_progress lp
		JOIN lessons ls ON ls.id = lp.lesson_id
		WHERE lp.user_id = $1::uuid AND lp.status = 'completed'
		ON CONFLICT (user_id, source, ref_id) WHERE ref_id IS NOT NULL DO NOTHING`, userID); err != nil {
		return mapErr(err)
	}

	// 3. Keep the denormalised xp_awarded column in step with the ledger.
	if _, err := q.Exec(ctx, `
		UPDATE lesson_progress lp
		SET xp_awarded = ls.xp_reward
		FROM lessons ls
		WHERE ls.id = lp.lesson_id AND lp.user_id = $1::uuid AND lp.status = 'completed'`, userID); err != nil {
		return mapErr(err)
	}

	// 4. Seed the spaced-repetition deck from completed lessons. Newly learned
	// words are first due tomorrow, not immediately.
	if _, err := q.Exec(ctx, `
		INSERT INTO review_items (id, user_id, lexeme_id, due_at)
		SELECT gen_random_uuid(), $1::uuid, el.lexeme_id, now() + interval '1 day'
		FROM exercise_lexemes el
		JOIN exercises e ON e.id = el.exercise_id
		JOIN lesson_progress lp ON lp.lesson_id = e.lesson_id AND lp.user_id = $1::uuid AND lp.status = 'completed'
		GROUP BY el.lexeme_id
		ON CONFLICT (user_id, lexeme_id) DO NOTHING`, userID); err != nil {
		return mapErr(err)
	}

	// 5. skill_mastery: average score per skill, including partial credit.
	if _, err := q.Exec(ctx, `
		INSERT INTO skill_mastery (user_id, skill, attempts_count, correct_count, mastery, last_attempt_at, recomputed_at)
		SELECT $1::uuid, e.skill,
		       count(*)::int,
		       count(*) FILTER (WHERE a.is_correct)::int,
		       coalesce(avg(a.score), 0)::numeric(5,4),
		       max(a.created_at),
		       now()
		FROM attempts a
		JOIN exercises e ON e.id = a.exercise_id
		WHERE a.user_id = $1::uuid AND `+onPathAttempt+`
		GROUP BY e.skill
		ON CONFLICT (user_id, skill) DO UPDATE SET
			attempts_count  = EXCLUDED.attempts_count,
			correct_count   = EXCLUDED.correct_count,
			mastery         = EXCLUDED.mastery,
			last_attempt_at = EXCLUDED.last_attempt_at,
			recomputed_at   = now()`, userID); err != nil {
		return mapErr(err)
	}

	// 6. daily_activity, bucketed by the learner's LOCAL calendar day.
	if _, err := q.Exec(ctx, `
		WITH bounds AS (
			SELECT timezone, daily_goal_kind, daily_goal_value FROM users WHERE id = $1::uuid
		),
		att AS (
			SELECT (a.created_at AT TIME ZONE b.timezone)::date AS day,
			       count(*)::int                                AS attempts_count,
			       count(*) FILTER (WHERE a.is_correct)::int    AS correct_count,
			       (coalesce(sum(a.duration_ms), 0) / 60000)::int AS minutes
			FROM attempts a CROSS JOIN bounds b
			WHERE a.user_id = $1::uuid AND `+onPathAttempt+`
			GROUP BY 1
		),
		xpe AS (
			SELECT (x.created_at AT TIME ZONE b.timezone)::date AS day, sum(x.amount)::int AS xp
			FROM xp_events x CROSS JOIN bounds b
			WHERE x.user_id = $1::uuid
			GROUP BY 1
		),
		done AS (
			SELECT (lp.completed_at AT TIME ZONE b.timezone)::date AS day, count(*)::int AS lessons_completed
			FROM lesson_progress lp CROSS JOIN bounds b
			WHERE lp.user_id = $1::uuid AND lp.status = 'completed' AND lp.completed_at IS NOT NULL
			GROUP BY 1
		),
		days AS (SELECT day FROM att UNION SELECT day FROM xpe UNION SELECT day FROM done)
		INSERT INTO daily_activity (user_id, day, xp, minutes, attempts_count, correct_count,
		                            lessons_completed, goal_met, recomputed_at)
		SELECT $1::uuid, d.day,
		       coalesce(xpe.xp, 0), coalesce(att.minutes, 0), coalesce(att.attempts_count, 0),
		       coalesce(att.correct_count, 0), coalesce(done.lessons_completed, 0),
		       CASE b.daily_goal_kind
		           WHEN 'minutes' THEN coalesce(att.minutes, 0) >= b.daily_goal_value
		           WHEN 'xp'      THEN coalesce(xpe.xp, 0) >= b.daily_goal_value
		           ELSE coalesce(done.lessons_completed, 0) >= b.daily_goal_value
		       END,
		       now()
		FROM days d
		CROSS JOIN bounds b
		LEFT JOIN att  ON att.day  = d.day
		LEFT JOIN xpe  ON xpe.day  = d.day
		LEFT JOIN done ON done.day = d.day
		ON CONFLICT (user_id, day) DO UPDATE SET
			xp                = EXCLUDED.xp,
			minutes           = EXCLUDED.minutes,
			attempts_count    = EXCLUDED.attempts_count,
			correct_count     = EXCLUDED.correct_count,
			lessons_completed = EXCLUDED.lessons_completed,
			goal_met          = EXCLUDED.goal_met,
			recomputed_at     = now()`, userID); err != nil {
		return mapErr(err)
	}

	// 7. Streak. Computed in Go (see internal/progress) from the rolled-up days so
	// the grace rule and the local-day boundary are unit-testable.
	days, err := ActivityDays(ctx, q, userID, 400)
	if err != nil {
		return err
	}
	current, longest, last := computeStreak(days, todayInTimezone(ctx, q, userID))
	if _, err := q.Exec(ctx, `
		INSERT INTO user_streaks (user_id, current_streak, longest_streak, last_active_day, recomputed_at)
		VALUES ($1::uuid, $2, $3, $4, now())
		ON CONFLICT (user_id) DO UPDATE SET
			current_streak = EXCLUDED.current_streak,
			longest_streak = greatest(user_streaks.longest_streak, EXCLUDED.longest_streak),
			last_active_day = EXCLUDED.last_active_day,
			recomputed_at = now()`, userID, current, longest, last); err != nil {
		return mapErr(err)
	}
	return nil
}

// RecomputeUserProgress is the pool-scoped convenience wrapper.
func (s *Store) RecomputeUserProgress(ctx context.Context, userID uuid.UUID) error {
	return RecomputeProgress(ctx, s.Pool, userID)
}

// ActivityDays reads rolled-up activity, oldest first.
func ActivityDays(ctx context.Context, q querier, userID uuid.UUID, limit int) ([]DayActivity, error) {
	rows, err := q.Query(ctx, `
		SELECT day, xp, minutes, attempts_count, correct_count, lessons_completed, goal_met
		FROM daily_activity
		WHERE user_id = $1::uuid
		ORDER BY day DESC
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()

	var out []DayActivity
	for rows.Next() {
		var d DayActivity
		if err := rows.Scan(&d.Day, &d.XP, &d.Minutes, &d.Attempts, &d.Correct, &d.Lessons, &d.GoalMet); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, mapErr(err)
	}
	// Reverse into ascending order: callers and the streak walk expect oldest first.
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	return out, nil
}

// todayInTimezone resolves "today" in the learner's timezone using the database's
// timezone database, so the answer does not depend on the Go binary's tzdata.
func todayInTimezone(ctx context.Context, q querier, userID uuid.UUID) time.Time {
	var today time.Time
	if err := q.QueryRow(ctx, `SELECT (now() AT TIME ZONE timezone)::date FROM users WHERE id = $1::uuid`, userID).Scan(&today); err != nil {
		return time.Now().UTC().Truncate(24 * time.Hour)
	}
	return today
}

// computeStreak adapts stored activity rows to the pure streak walker.
func computeStreak(days []DayActivity, today time.Time) (current, longest int, last *time.Time) {
	pd := make([]progress.Day, 0, len(days))
	for _, d := range days {
		pd = append(pd, progress.Day{Date: d.Day, GoalMet: d.GoalMet})
	}
	s := progress.ComputeStreak(pd, today)
	return s.Current, s.Longest, s.LastActive
}

// StreakStateFor reads the persisted streak plus today's local activity.
func (s *Store) StreakStateFor(ctx context.Context, userID uuid.UUID) (*StreakState, error) {
	st := &StreakState{}
	err := s.Pool.QueryRow(ctx, `
		SELECT u.daily_goal_value, u.daily_goal_kind,
		       coalesce(s.current_streak, 0), coalesce(s.longest_streak, 0), s.last_active_day,
		       coalesce(s.freezes, 0),
		       coalesce(da.goal_met, false), coalesce(da.xp, 0), coalesce(da.minutes, 0)
		FROM users u
		LEFT JOIN user_streaks s ON s.user_id = u.id
		LEFT JOIN daily_activity da ON da.user_id = u.id AND da.day = (now() AT TIME ZONE u.timezone)::date
		WHERE u.id = $1::uuid`, userID).
		Scan(&st.GoalValue, &st.GoalKind, &st.CurrentStreak, &st.LongestStreak, &st.LastActiveDay,
			&st.Freezes, &st.GoalMetToday, &st.TodayXP, &st.TodayMinutes)
	if err != nil {
		return nil, mapErr(err)
	}
	return st, nil
}

// SkillMasteryFor reads the per-skill rollup.
func (s *Store) SkillMasteryFor(ctx context.Context, userID uuid.UUID) ([]SkillMastery, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT skill, attempts_count, correct_count, mastery, last_attempt_at
		FROM skill_mastery WHERE user_id = $1::uuid ORDER BY skill`, userID)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []SkillMastery
	for rows.Next() {
		var m SkillMastery
		if err := rows.Scan(&m.Skill, &m.Attempts, &m.Correct, &m.Mastery, &m.LastAttemptAt); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, m)
	}
	return out, mapErr(rows.Err())
}

// Totals returns the aggregate counters for the dashboard, derived from attempts.
func (s *Store) Totals(ctx context.Context, userID uuid.UUID) (attempts, correct, lessonsDone, totalXP int, err error) {
	err = s.Pool.QueryRow(ctx, `
		SELECT
			(SELECT count(*)::int FROM attempts WHERE user_id = $1::uuid AND mode NOT IN ('tone', 'proficiency', 'studio')),
			(SELECT count(*)::int FROM attempts WHERE user_id = $1::uuid AND mode NOT IN ('tone', 'proficiency', 'studio') AND is_correct IS TRUE),
			(SELECT count(*)::int FROM lesson_progress WHERE user_id = $1::uuid AND status = 'completed'),
			(SELECT coalesce(sum(amount), 0)::int FROM xp_events WHERE user_id = $1::uuid)`,
		userID).Scan(&attempts, &correct, &lessonsDone, &totalXP)
	return attempts, correct, lessonsDone, totalXP, mapErr(err)
}

// --- spaced repetition -----------------------------------------------------

// DueReviewItems returns cards due now, oldest due first.
func (s *Store) DueReviewItems(ctx context.Context, userID uuid.UUID, limit int) ([]ReviewItem, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT r.id, r.lexeme_id, x.simplified, x.traditional, x.pinyin, x.senses, x.audio_key,
		       r.interval_index, r.ease::float8, r.reps, r.lapses, r.due_at
		FROM review_items r
		JOIN lexemes x ON x.id = r.lexeme_id
		WHERE r.user_id = $1::uuid AND r.due_at <= now()
		ORDER BY r.due_at
		LIMIT $2`, userID, limit)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []ReviewItem
	for rows.Next() {
		var it ReviewItem
		if err := rows.Scan(&it.ID, &it.LexemeID, &it.Simplified, &it.Traditional, &it.Pinyin,
			&it.Senses, &it.AudioKey, &it.IntervalIndex, &it.Ease, &it.Reps, &it.Lapses, &it.DueAt); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, it)
	}
	return out, mapErr(rows.Err())
}

// CountDue counts cards due now.
func (s *Store) CountDue(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := s.Pool.QueryRow(ctx, `SELECT count(*)::int FROM review_items WHERE user_id = $1::uuid AND due_at <= now()`, userID).Scan(&n)
	return n, mapErr(err)
}

// ReviewForecast says how many cards fall due in each window from now. The windows are
// relative to now, not to a calendar day, so the forecast needs no time zone and cannot
// disagree with the "due now" count: every card is in exactly one bucket.
type ReviewForecast struct {
	Now   int `json:"now"`   // due already
	Day   int `json:"day"`   // within the next 24 hours
	Days3 int `json:"days3"` // 1 to 3 days from now
	Week  int `json:"week"`  // 3 to 7 days from now
	Later int `json:"later"` // beyond a week
}

func (s *Store) ReviewForecast(ctx context.Context, userID uuid.UUID) (ReviewForecast, error) {
	var f ReviewForecast
	err := s.Pool.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE due_at <= now())::int,
		       count(*) FILTER (WHERE due_at > now() AND due_at <= now() + interval '1 day')::int,
		       count(*) FILTER (WHERE due_at > now() + interval '1 day' AND due_at <= now() + interval '3 days')::int,
		       count(*) FILTER (WHERE due_at > now() + interval '3 days' AND due_at <= now() + interval '7 days')::int,
		       count(*) FILTER (WHERE due_at > now() + interval '7 days')::int
		FROM review_items WHERE user_id = $1::uuid`, userID).Scan(&f.Now, &f.Day, &f.Days3, &f.Week, &f.Later)
	return f, mapErr(err)
}

// ApplyReviewResult advances one card. The scheduler maths lives in
// internal/progress so the interval ladder is unit-tested, not embedded in SQL.
func (s *Store) ApplyReviewResult(ctx context.Context, userID, lexemeID uuid.UUID, correct bool, intervalIndex int, ease float64, dueAt time.Time) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE review_items SET
			interval_index = $3,
			ease           = $4,
			reps           = reps + 1,
			lapses         = lapses + CASE WHEN $5 THEN 0 ELSE 1 END,
			due_at         = $6,
			last_result    = CASE WHEN $5 THEN 'correct' ELSE 'wrong' END,
			updated_at     = now()
		WHERE user_id = $1::uuid AND lexeme_id = $2::uuid`, userID, lexemeID, intervalIndex, ease, correct, dueAt)
	return mapErr(err)
}

// VocabularyByIDs loads lexemes for review-card construction.
func (s *Store) VocabularyByIDs(ctx context.Context, ids []uuid.UUID) ([]Lexeme, error) {
	rows, err := s.Pool.Query(ctx, `
		SELECT x.id, x.simplified, x.traditional, x.pinyin, x.parts_of_speech, x.senses, x.audio_key,
		       (SELECT min(ll.level_number) FROM lexeme_levels ll WHERE ll.lexeme_id = x.id)
		FROM lexemes x WHERE x.id = ANY($1) ORDER BY x.simplified`, ids)
	if err != nil {
		return nil, mapErr(err)
	}
	defer rows.Close()
	var out []Lexeme
	for rows.Next() {
		var x Lexeme
		if err := rows.Scan(&x.ID, &x.Simplified, &x.Traditional, &x.Pinyin, &x.POS, &x.Senses, &x.AudioKey, &x.HSKLevel); err != nil {
			return nil, mapErr(err)
		}
		out = append(out, x)
	}
	return out, mapErr(rows.Err())
}
