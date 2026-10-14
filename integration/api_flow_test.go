// Package integration drives the real HTTP API against a real PostgreSQL
// database: content import, a full lesson completed with correct answers read
// from the answer keys, and every derived value that depends on it.
//
// It is gated on HANPATH_TEST_DATABASE_URL so that `go test ./...` on a machine
// without a database does not fail. Run it through `make test-integration`, which
// sets the variable — a bare `go test ./...` skips this file and proves nothing
// about grading or progress.
package integration

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/attempts"
	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/exercises"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

func TestLearnerFlowEndToEnd(t *testing.T) {
	dbURL := os.Getenv("HANPATH_TEST_DATABASE_URL")
	if dbURL == "" {
		t.Skip("HANPATH_TEST_DATABASE_URL is not set; run `make test-integration` so this actually executes")
	}
	ctx := context.Background()
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if err := store.Migrate(ctx, dbURL, log); err != nil {
		t.Fatalf("migrating: %v", err)
	}
	st, err := store.New(ctx, dbURL, log)
	if err != nil {
		t.Fatalf("connecting: %v", err)
	}
	// Registered before the per-user cleanup below, so that cleanup runs first
	// (t.Cleanup is LIFO) and the delete still has a live pool.
	t.Cleanup(func() { st.Close() })

	// --- content -----------------------------------------------------------
	doc := loadContent(t)
	report, err := curriculum.Import(ctx, st, doc, provisionerFor(t))
	if err != nil {
		t.Fatalf("importing content: %v", err)
	}
	t.Logf("content imported: %d levels, %d units, %d lessons, %d lexemes, %d exercises",
		report.Levels, report.Units, report.Lessons, report.Lexemes, report.Exercises)
	if report.Exercises < 20 {
		t.Fatalf("expected the authored level to have exercises, got %d", report.Exercises)
	}

	// Import must be idempotent: a second import of unchanged content must not
	// duplicate anything, because authors re-import after every edit.
	second, err := curriculum.Import(ctx, st, doc, provisionerFor(t))
	if err != nil {
		t.Fatalf("re-importing content: %v", err)
	}
	if second.Lessons != report.Lessons || second.Exercises != report.Exercises {
		t.Fatalf("re-import changed the content counts: %d lessons (want %d), %d exercises (want %d)",
			second.Lessons, report.Lessons, second.Exercises, report.Exercises)
	}
	t.Log("re-import is idempotent (same lesson and exercise counts)")

	// --- server ------------------------------------------------------------
	cfg := &config.Config{
		AppEnv:                "test",
		LogLevel:              "error",
		PublicBase:            "http://127.0.0.1:5174",
		DatabaseURL:           dbURL,
		SessionCookieName:     "hanpath_test_session",
		CSRFCookieName:        "hanpath_test_csrf",
		CSRFHeaderName:        "X-CSRF-Token",
		SessionTTL:            time.Hour,
		MaxAttemptsPerRequest: 100,
	}
	srv := httptest.NewServer(serverFor(t, cfg, log, st).Routes())
	defer srv.Close()

	c := newClient(t, srv.URL)

	// --- register ----------------------------------------------------------
	email := fmt.Sprintf("integration-%d@example.com", time.Now().UnixNano())
	var reg struct {
		User      store.User `json:"user"`
		CSRFToken string     `json:"csrf_token"`
	}
	if code := c.do(http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email": email, "display_name": "Integration Learner",
		"password": "a-long-enough-passphrase", "timezone": "Asia/Jakarta",
	}, &reg); code != http.StatusCreated {
		t.Fatalf("register: status %d", code)
	}
	userID := reg.User.ID
	// Leave no test data behind: every dependent row cascades from users.
	t.Cleanup(func() {
		if _, err := st.Pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	// A goal measured in lessons makes the daily-goal and streak pipeline
	// observable from one completed lesson.
	if code := c.do(http.MethodPatch, "/api/v1/me", map[string]any{
		"daily_goal_kind": "lessons", "daily_goal_value": 1,
	}, nil); code != http.StatusOK {
		t.Fatalf("setting the daily goal: status %d", code)
	}

	// --- path and lesson ---------------------------------------------------
	var path struct {
		Levels []struct {
			Units []struct {
				Lessons []struct {
					ID    uuid.UUID `json:"id"`
					State string    `json:"state"`
				} `json:"lessons"`
			} `json:"units"`
		} `json:"levels"`
	}
	if code := c.do(http.MethodGet, "/api/v1/curriculum/hsk2.0/path", nil, &path); code != http.StatusOK {
		t.Fatalf("path: status %d", code)
	}
	if len(path.Levels) == 0 || len(path.Levels[0].Units) == 0 {
		t.Fatal("path is empty: is the content imported?")
	}
	first := path.Levels[0].Units[0].Lessons[0]
	if first.State != "available" {
		t.Fatalf("the first lesson should be available on day one, got %q", first.State)
	}
	t.Logf("path: %d lessons across %d units, first lesson state=%s", countLessons(path.Levels), len(path.Levels[0].Units), first.State)

	var lesson store.LessonDetail
	if code := c.do(http.MethodGet, "/api/v1/lessons/"+first.ID.String(), nil, &lesson); code != http.StatusOK {
		t.Fatalf("lesson: status %d", code)
	}
	if len(lesson.Exercises) == 0 {
		t.Fatal("lesson has no exercises")
	}
	t.Logf("lesson %q: %d exercises, %d passages, %d vocabulary items",
		lesson.Title, len(lesson.Exercises), len(lesson.Passages), len(lesson.Vocabulary))

	// --- complete the lesson with correct answers ---------------------------
	inputs := correctAnswers(t, ctx, st, first.ID, lesson.Exercises)
	var summary attempts.Summary
	if code := c.do(http.MethodPost, "/api/v1/lessons/"+first.ID.String()+"/attempts", map[string]any{
		"mode": "practice", "attempts": inputs,
	}, &summary); code != http.StatusOK {
		t.Fatalf("submitting attempts: status %d", code)
	}

	for i, res := range summary.Results {
		spec := lesson.Exercises[i]
		// Two families are deliberately not asserted correct: free-form writing is
		// judged by a keyword rubric, and speaking is not judged at all (there is no
		// speech recognition in this build). Everything else — listening included,
		// since the learner chooses or types something comparable to an answer key —
		// must be graded correct when the answer key is submitted.
		if exercises.IsMechanicallyGradable(spec.Type) && spec.Type != "short_answer" {
			if res.IsCorrect == nil || !*res.IsCorrect {
				t.Errorf("%s (exercise %d): correct answer graded as %v (score %.2f, feedback %v)",
					spec.Type, spec.Number, res.IsCorrect, res.Score, res.Feedback)
			}
			continue
		}
		if res.IsCorrect != nil {
			t.Errorf("%s: an answer that cannot be judged mechanically was graded %v", spec.Type, *res.IsCorrect)
		}
		if !res.NeedsReview {
			t.Errorf("%s: an unjudgeable answer was not flagged for review", spec.Type)
		}
		if res.Score <= 0 {
			t.Errorf("%s: a complete answer scored %.2f, so the attempt did not register", spec.Type, res.Score)
		}
		if spec.Type == "speak_repeat" || spec.Type == "read_aloud" || spec.Type == "speak_answer" {
			if res.Feedback["limitation"] == nil || res.Feedback["has_speech_recognition"] != false {
				t.Errorf("%s: the result must state that no speech recognition was used, got %v", spec.Type, res.Feedback)
			}
		}
	}

	if len(summary.Lessons) != 1 {
		t.Fatalf("expected one lesson outcome, got %d", len(summary.Lessons))
	}
	outcome := summary.Lessons[0]
	if outcome.Status != "completed" || !outcome.CompletedNow {
		t.Fatalf("lesson should have completed on this submission: %+v", outcome)
	}
	if outcome.XPAwarded != lesson.XPReward {
		t.Errorf("XP awarded = %d, want %d", outcome.XPAwarded, lesson.XPReward)
	}
	t.Logf("lesson completed: %d/%d correct, %d XP", outcome.Correct, outcome.Total, outcome.XPAwarded)

	// --- derived state: streak, activity, review deck ----------------------
	streak := readStreak(t, ctx, st, userID)
	if streak != 1 {
		t.Errorf("streak after completing the day's goal = %d, want 1", streak)
	} else {
		t.Log("daily goal met by the completed lesson: streak is 1")
	}

	var goalMet bool
	var minutes, xp int
	if err := st.Pool.QueryRow(ctx, `
		SELECT goal_met, minutes, xp FROM daily_activity
		WHERE user_id = $1 AND day = (now() AT TIME ZONE 'Asia/Jakarta')::date`, userID).
		Scan(&goalMet, &minutes, &xp); err != nil {
		t.Fatalf("reading daily_activity: %v", err)
	}
	if !goalMet || xp <= 0 {
		t.Errorf("daily_activity row looks wrong: goal_met=%v xp=%d minutes=%d", goalMet, xp, minutes)
	}
	t.Logf("daily rollup: goal_met=%v xp=%d minutes=%d", goalMet, xp, minutes)

	var reviewItems int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM review_items WHERE user_id = $1`, userID).Scan(&reviewItems); err != nil {
		t.Fatalf("counting review items: %v", err)
	}
	if reviewItems != len(lesson.Vocabulary) {
		t.Errorf("review deck has %d items, want one per vocabulary item (%d)", reviewItems, len(lesson.Vocabulary))
	}
	t.Logf("spaced-repetition deck seeded with %d words", reviewItems)

	// The next lesson must have unlocked.
	var after struct {
		Levels []struct {
			Units []struct {
				Lessons []struct {
					ID    uuid.UUID `json:"id"`
					State string    `json:"state"`
				} `json:"lessons"`
			} `json:"units"`
		} `json:"levels"`
	}
	if code := c.do(http.MethodGet, "/api/v1/curriculum/hsk2.0/path", nil, &after); code != http.StatusOK {
		t.Fatalf("path after completion: status %d", code)
	}
	states := []string{}
	for _, lv := range after.Levels {
		for _, u := range lv.Units {
			for _, l := range u.Lessons {
				states = append(states, l.State)
			}
		}
	}
	want := []string{"completed", "available", "locked", "locked", "locked", "locked"}
	if len(states) < len(want) {
		t.Fatalf("path shrank after completion: %v", states)
	}
	for i := range want {
		if states[i] != want[i] {
			t.Fatalf("lesson %d state = %q, want %q (full path: %v)", i, states[i], want[i], states)
		}
	}
	t.Logf("path advanced: %v", states[:len(want)])

	// --- idempotent replay -------------------------------------------------
	var replay attempts.Summary
	if code := c.do(http.MethodPost, "/api/v1/lessons/"+first.ID.String()+"/attempts", map[string]any{
		"mode": "practice", "attempts": inputs,
	}, &replay); code != http.StatusOK {
		t.Fatalf("replaying attempts: status %d", code)
	}
	for _, res := range replay.Results {
		if !res.Duplicate {
			t.Errorf("replayed attempt was recorded again: %+v", res)
		}
	}
	var attemptCount int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM attempts WHERE user_id = $1`, userID).Scan(&attemptCount); err != nil {
		t.Fatalf("counting attempts: %v", err)
	}
	if attemptCount != len(inputs) {
		t.Errorf("attempts table has %d rows after a replay, want %d", attemptCount, len(inputs))
	}
	var xpAfterReplay int
	if err := st.Pool.QueryRow(ctx, `SELECT coalesce(sum(amount),0) FROM xp_events WHERE user_id = $1`, userID).Scan(&xpAfterReplay); err != nil {
		t.Fatalf("summing XP: %v", err)
	}
	if xpAfterReplay != outcome.XPAwarded {
		t.Errorf("XP after replay = %d, want %d (XP must not be double-counted)", xpAfterReplay, outcome.XPAwarded)
	}
	t.Logf("replay is idempotent: %d attempts, %d XP", attemptCount, xpAfterReplay)

	// --- spaced repetition round trip --------------------------------------
	if _, err := st.Pool.Exec(ctx, `
		UPDATE review_items SET due_at = now() - interval '1 minute' WHERE user_id = $1`, userID); err != nil {
		t.Fatalf("making review cards due: %v", err)
	}
	var queue struct {
		Items    []store.ReviewItem `json:"items"`
		DueTotal int                `json:"due_total"`
	}
	if code := c.do(http.MethodGet, "/api/v1/reviews/queue?limit=5", nil, &queue); code != http.StatusOK {
		t.Fatalf("review queue: status %d", code)
	}
	if len(queue.Items) != 5 || queue.DueTotal != reviewItems {
		t.Fatalf("review queue returned %d of %d due cards", len(queue.Items), queue.DueTotal)
	}
	var answer struct {
		IntervalIndex int       `json:"interval_index"`
		DueAt         time.Time `json:"due_at"`
		DueTotal      int       `json:"due_total"`
	}
	if code := c.do(http.MethodPost, "/api/v1/reviews/answer", map[string]any{
		"lexeme_id": queue.Items[0].LexemeID, "correct": true,
	}, &answer); code != http.StatusOK {
		t.Fatalf("answering a review card: status %d", code)
	}
	if answer.IntervalIndex != 1 {
		t.Errorf("a correct review should advance to interval 1, got %d", answer.IntervalIndex)
	}
	if answer.DueTotal != reviewItems-1 {
		t.Errorf("due total = %d after answering one of %d cards", answer.DueTotal, reviewItems)
	}
	if hours := time.Until(answer.DueAt).Hours(); hours < 23 || hours > 25 {
		t.Errorf("a card advanced to interval 1 should be due in about 24 hours, got %.1fh", hours)
	}
	t.Logf("review round trip: card advanced to interval %d, next due in %.1fh, %d still due",
		answer.IntervalIndex, time.Until(answer.DueAt).Hours(), answer.DueTotal)

	// --- exam mode hides the solution --------------------------------------
	var exam attempts.Summary
	if code := c.do(http.MethodPost, "/api/v1/lessons/"+first.ID.String()+"/attempts", map[string]any{
		"mode": "exam",
		"attempts": []map[string]any{{
			"exercise_id": lesson.Exercises[0].ID, "client_ref": "integration-exam-1",
			"response": map[string]any{"option_id": "zzz"}, "duration_ms": 900,
		}},
	}, &exam); code != http.StatusOK {
		t.Fatalf("exam submission: status %d", code)
	}
	if len(exam.Results) != 1 {
		t.Fatalf("expected 1 exam result, got %d", len(exam.Results))
	}
	if exam.Results[0].Solution != nil || exam.Results[0].Feedback != nil {
		t.Error("exam mode leaked the solution or the feedback detail")
	}
	t.Log("exam mode withheld the solution and the per-answer feedback")

	// --- overview ----------------------------------------------------------
	var overview store.ProgressOverview
	if code := c.do(http.MethodGet, "/api/v1/progress/overview", nil, &overview); code != http.StatusOK {
		t.Fatalf("overview: status %d", code)
	}
	if overview.TotalXP != outcome.XPAwarded || overview.LessonsDone != 1 {
		t.Errorf("overview mismatch: xp=%d lessons=%d", overview.TotalXP, overview.LessonsDone)
	}
	if len(overview.Skills) == 0 {
		t.Error("overview reported no skill mastery despite attempts")
	}
	t.Logf("overview: xp=%d lessons=%d attempts=%d skills=%d streak=%d",
		overview.TotalXP, overview.LessonsDone, overview.TotalAttempts, len(overview.Skills), overview.Streak.CurrentStreak)
}

// --- helpers ---------------------------------------------------------------

func loadContent(t *testing.T) *curriculum.File {
	t.Helper()
	f, err := os.Open("../content/hsk2.0/level-1.yaml")
	if err != nil {
		t.Fatalf("opening content: %v", err)
	}
	defer f.Close()
	doc, err := curriculum.Parse(f)
	if err != nil {
		t.Fatalf("parsing content: %v", err)
	}
	if problems := doc.Validate(); len(problems) > 0 {
		t.Fatalf("authored content is invalid: %v", problems)
	}
	return doc
}

// correctAnswers reads the answer keys straight out of the database and builds a
// response that every grader should accept. This is what makes the test a real
// check of the graders rather than of the test's own idea of the answers.
func correctAnswers(t *testing.T, ctx context.Context, st *store.Store, lessonID uuid.UUID, exercises []store.ExerciseForLearner) []map[string]any {
	t.Helper()
	rows, err := st.Pool.Query(ctx, `
		SELECT exercise_number, type, answer_key FROM exercises WHERE lesson_id = $1 ORDER BY exercise_number`, lessonID)
	if err != nil {
		t.Fatalf("reading answer keys: %v", err)
	}
	defer rows.Close()

	keys := map[int]json.RawMessage{}
	types := map[int]string{}
	for rows.Next() {
		var n int
		var typ string
		var key json.RawMessage
		if err := rows.Scan(&n, &typ, &key); err != nil {
			t.Fatalf("scanning answer keys: %v", err)
		}
		keys[n] = key
		types[n] = typ
	}

	out := make([]map[string]any, 0, len(exercises))
	for i, ex := range exercises {
		raw, ok := keys[ex.Number]
		if !ok {
			t.Fatalf("no answer key for exercise %d", ex.Number)
		}
		if types[ex.Number] != ex.Type {
			t.Fatalf("exercise %d type mismatch: %s vs %s", ex.Number, types[ex.Number], ex.Type)
		}
		resp := responseFor(t, ex.Type, raw)
		out = append(out, map[string]any{
			"exercise_id": ex.ID,
			// Scoped to the lesson: a client_ref must be unique per attempt, so
			// reusing one across lessons is a client bug the API now rejects.
			"client_ref":  fmt.Sprintf("integration-%s-%d", lessonID.String()[:8], i+1),
			"response":    resp,
			"duration_ms": 3000 + i,
		})
	}
	return out
}

func responseFor(t *testing.T, exerciseType string, answerKey json.RawMessage) map[string]any {
	t.Helper()
	switch exerciseType {
	case "mcq":
		var k struct {
			CorrectID string `json:"correct_id"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"option_id": k.CorrectID}
	case "mcq_multi":
		var k struct {
			CorrectIDs []string `json:"correct_ids"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"option_ids": k.CorrectIDs}
	case "true_false":
		var k struct {
			Correct bool `json:"correct"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"value": k.Correct}
	case "fill_blank":
		var k struct {
			Accepted []string `json:"accepted"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"text": k.Accepted[0]}
	case "sentence_order":
		var k struct {
			Order []int `json:"order"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"order": k.Order}
	case "translation":
		var k struct {
			Accepted []string `json:"accepted"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"text": k.Accepted[0]}
	case "hanzi_trace", "hanzi_recall":
		var k struct {
			Character   string `json:"character"`
			StrokeCount int    `json:"stroke_count"`
		}
		mustUnmarshal(t, answerKey, &k)
		strokes := make([]map[string]any, 0, k.StrokeCount)
		for i := 0; i < k.StrokeCount; i++ {
			strokes = append(strokes, map[string]any{"index": i, "mistaken": false})
		}
		// The exact shape web/src/lib/answers.ts hanziPayload sends, character and
		// stroke_count included. Leaving those two out is what let every writing
		// submission from the real client fail with a 500 while this suite stayed
		// green: the API was only ever asked with a payload the client never sends.
		return map[string]any{
			"character":    k.Character,
			"stroke_count": k.StrokeCount,
			"strokes":      strokes,
			"completed":    true,
			"hints_used":   0,
		}
	case "listening_mcq":
		var k struct {
			CorrectID string `json:"correct_id"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"option_id": k.CorrectID}
	case "listening_dictation":
		var k struct {
			Accepted []string `json:"accepted"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"text": k.Accepted[0]}
	case "speak_repeat", "read_aloud", "speak_answer":
		// There is no correct answer to look up: a speaking result is scored on what
		// can be checked (a recording of a believable length) and never asserts that
		// the learner pronounced anything correctly.
		return map[string]any{"duration_ms": 2600, "self_rating": 4}
	case "short_answer":
		var k struct {
			Reference string `json:"reference"`
		}
		mustUnmarshal(t, answerKey, &k)
		return map[string]any{"text": k.Reference}
	default:
		t.Fatalf("no response builder for exercise type %q", exerciseType)
		return nil
	}
}

func readStreak(t *testing.T, ctx context.Context, st *store.Store, userID uuid.UUID) int {
	t.Helper()
	var current int
	if err := st.Pool.QueryRow(ctx, `SELECT coalesce(current_streak, 0) FROM user_streaks WHERE user_id = $1`, userID).Scan(&current); err != nil {
		t.Fatalf("reading the streak: %v", err)
	}
	return current
}

func countLessons(levels []struct {
	Units []struct {
		Lessons []struct {
			ID    uuid.UUID `json:"id"`
			State string    `json:"state"`
		} `json:"lessons"`
	} `json:"units"`
}) int {
	n := 0
	for _, lv := range levels {
		for _, u := range lv.Units {
			n += len(u.Lessons)
		}
	}
	return n
}

func mustUnmarshal(t *testing.T, raw json.RawMessage, dst any) {
	t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		t.Fatalf("decoding answer key %s: %v", raw, err)
	}
}

type client struct {
	t    *testing.T
	base string
	http *http.Client
	csrf string
}

func newClient(t *testing.T, base string) *client {
	t.Helper()
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("building a cookie jar: %v", err)
	}
	return &client{t: t, base: base, http: &http.Client{Jar: jar, Timeout: 15 * time.Second}}
}

// do issues a request, sending the CSRF header when one has been captured, and
// returns the status code. The CSRF token is re-read from the cookie jar after
// every call, mirroring what the SPA does.
func (c *client) do(method, path string, body any, out any) int {
	c.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			c.t.Fatalf("encoding request body: %v", err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.base+path, reader)
	if err != nil {
		c.t.Fatalf("building request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.csrf != "" {
		req.Header.Set("X-CSRF-Token", c.csrf)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		c.t.Fatalf("reading response: %v", err)
	}
	for _, cookie := range c.http.Jar.Cookies(req.URL) {
		if len(cookie.Name) >= 4 && cookie.Name[len(cookie.Name)-4:] == "csrf" {
			c.csrf = cookie.Value
		}
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: decoding response %s: %v", method, path, raw, err)
		}
	}
	return resp.StatusCode
}
