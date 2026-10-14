package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"testing"

	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"net/http/httptest"

	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// Tone practice, end to end — including the property the whole design exists for.
//
// Tone drills are served from any level, so they include lessons the learner has not
// reached. Submitting one must therefore NOT move the path: lesson status, XP, mastery,
// the daily rollup and the streak all stay exactly as they were. RecomputeProgress
// derives lesson state from attempts with no mode filter, so this test is what proves
// the exclusion actually holds rather than merely being intended.
//
// Gated on HANPATH_TEST_DATABASE_URL like the rest of this package; run
// `make test-integration`.
func TestTonePracticeEndToEnd(t *testing.T) {
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
	t.Cleanup(func() { st.Close() })

	// Level 1 and its tone unit: the authored level plus the drills generated into a
	// second file. Level 2 would need ~150 audio assets synthesised for no extra
	// property, and the tone unit is locked for a fresh learner, which is the case that
	// matters here.
	if _, err := curriculum.Import(ctx, st, loadContent(t), provisionerFor(t)); err != nil {
		t.Fatalf("importing content: %v", err)
	}
	if _, err := curriculum.Import(ctx, st, loadToneFile(t), provisionerFor(t)); err != nil {
		t.Fatalf("importing tone content: %v", err)
	}

	cfg := &config.Config{
		AppEnv: "test", LogLevel: "error",
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

	var reg struct {
		User store.User `json:"user"`
	}
	if code := c.do(http.MethodPost, "/api/v1/auth/register", map[string]any{
		"email":        fmt.Sprintf("integration-tone-%d@example.com", time.Now().UnixNano()),
		"display_name": "Tone Driller",
		"password":     "a-long-enough-passphrase",
		"timezone":     "Asia/Jakarta",
	}, &reg); code != http.StatusCreated {
		t.Fatalf("register: status %d", code)
	}
	userID := reg.User.ID
	t.Cleanup(func() {
		if _, err := st.Pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, userID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	type toneItem struct {
		ID     uuid.UUID      `json:"id"`
		Type   string         `json:"type"`
		Skill  string         `json:"skill"`
		Level  int            `json:"level"`
		Prompt map[string]any `json:"prompt"`
	}
	type toneQueue struct {
		Items []toneItem `json:"items"`
		Level int        `json:"level"`
		Stats []struct {
			Level    int `json:"level"`
			Drills   int `json:"drills"`
			Answered int `json:"answered"`
			Correct  int `json:"correct"`
		} `json:"stats"`
	}

	// --- the queue is not gated by the path --------------------------------
	var queue toneQueue
	if code := c.do(http.MethodGet, "/api/v1/tones/queue?limit=50", nil, &queue); code != http.StatusOK {
		t.Fatalf("tone queue: status %d", code)
	}
	if len(queue.Items) == 0 {
		t.Fatal("a brand-new learner got no tone drills: the queue must not wait for the path")
	}
	if queue.Level != 1 {
		t.Errorf("default level = %d, want 1 for a learner who has not said which level they are on", queue.Level)
	}
	withAudio, withoutAudio := 0, 0
	for _, item := range queue.Items {
		if item.Type != "tone_mcq" && item.Type != "listening_mcq" {
			t.Errorf("the tone queue served a %s drill", item.Type)
		}
		if item.Level != 1 {
			t.Errorf("level filter ignored: got a level %d drill", item.Level)
		}
		// The answer key is the server's, and a written tone question must not carry the
		// pinyin either — there the pinyin IS the answer.
		for _, forbidden := range []string{"answer_key", "correct_id", "tones", "pinyin"} {
			if _, leaked := item.Prompt[forbidden]; leaked {
				t.Errorf("the queue leaked %q in a prompt", forbidden)
			}
		}
		if _, ok := item.Prompt["audio_url"]; ok {
			withAudio++
		} else {
			withoutAudio++
		}
	}
	// Both shapes have to appear: asked by sound for one syllable, asked as written for
	// more. Serving only one of them would be a silent half-feature.
	if withAudio == 0 || withoutAudio == 0 {
		t.Errorf("expected both kinds of tone drill, got %d with audio and %d without", withAudio, withoutAudio)
	}
	t.Logf("queue: %d drills (%d asked by sound, %d asked as written), level %d", len(queue.Items), withAudio, withoutAudio, queue.Level)

	// --- the level filter, and the default that follows the learner ---------
	//
	// Assert the rule, not the population: whether a level has content in this database
	// depends on what earlier runs imported, so the property worth checking is that
	// every drill served belongs to the level that was asked for.
	var levelTwo toneQueue
	if code := c.do(http.MethodGet, "/api/v1/tones/queue?level=2&limit=50", nil, &levelTwo); code != http.StatusOK {
		t.Fatalf("level 2 queue: status %d", code)
	}
	if levelTwo.Level != 2 {
		t.Errorf("asked for level 2, the queue reported level %d", levelTwo.Level)
	}
	for _, item := range levelTwo.Items {
		if item.Level != 2 {
			t.Errorf("the level filter served a level %d drill: %s", item.Level, item.ID)
		}
	}
	if code := c.do(http.MethodGet, "/api/v1/tones/queue?level=9", nil, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("level 9 should be rejected, got status %d", code)
	}

	// A learner who HAS said which level they are on gets that level, not a hard-coded 1.
	// users.target_level is nullable, and the unset path is the one that breaks silently.
	if code := c.do(http.MethodPatch, "/api/v1/me", map[string]any{"target_level": 2}, nil); code != http.StatusOK {
		t.Fatalf("setting target_level: status %d", code)
	}
	var followed toneQueue
	if code := c.do(http.MethodGet, "/api/v1/tones/queue?limit=50", nil, &followed); code != http.StatusOK {
		t.Fatalf("queue after setting a level: status %d", code)
	}
	if followed.Level != 2 {
		t.Errorf("the default did not follow the learner's level: level %d", followed.Level)
	}
	for _, item := range followed.Items {
		if item.Level != 2 {
			t.Errorf("the default served a level %d drill", item.Level)
		}
	}
	if code := c.do(http.MethodPatch, "/api/v1/me", map[string]any{"target_level": 1}, nil); code != http.StatusOK {
		t.Fatalf("restoring target_level: status %d", code)
	}

	// --- a drill that lives in a LOCKED lesson ------------------------------
	var exerciseID, lessonID uuid.UUID
	var correctOption, correctTones, otherOption string
	if err := st.Pool.QueryRow(ctx, `
		SELECT e.id, e.lesson_id, e.answer_key->>'correct_id', e.answer_key->>'tones',
		       (SELECT o->>'id' FROM jsonb_array_elements(e.prompt->'options') o
		         WHERE o->>'id' <> e.answer_key->>'correct_id' LIMIT 1)
		FROM exercises e
		WHERE e.type = 'tone_mcq'
		ORDER BY e.content_key
		LIMIT 1`).Scan(&exerciseID, &lessonID, &correctOption, &correctTones, &otherOption); err != nil {
		t.Fatalf("finding a tone drill: %v", err)
	}

	var lesson store.LessonDetail
	if code := c.do(http.MethodGet, "/api/v1/lessons/"+lessonID.String(), nil, &lesson); code != http.StatusOK {
		t.Fatalf("reading the tone lesson: status %d", code)
	}
	if lesson.State != "locked" {
		t.Fatalf("this test needs a tone drill inside a locked lesson; %q is %q", lesson.Title, lesson.State)
	}
	if code := c.do(http.MethodPost, "/api/v1/lessons/"+lessonID.String()+"/attempts", map[string]any{
		"attempts": []map[string]any{{"exercise_id": exerciseID, "response": map[string]any{"option_id": correctOption}}},
	}, nil); code != http.StatusForbidden {
		t.Fatalf("a locked lesson accepted a lesson submission: status %d", code)
	}

	// --- grading, and idempotency -----------------------------------------
	type answersResponse struct {
		Results []struct {
			ExerciseID string         `json:"exercise_id"`
			IsCorrect  *bool          `json:"is_correct"`
			Score      float64        `json:"score"`
			Feedback   map[string]any `json:"feedback"`
			Duplicate  bool           `json:"duplicate"`
		} `json:"results"`
		ToneTally []struct {
			Tone   string `json:"tone"`
			Label  string `json:"label"`
			Asked  int    `json:"asked"`
			Missed int    `json:"missed"`
		} `json:"tone_tally"`
		Stats []struct {
			Level    int `json:"level"`
			Answered int `json:"answered"`
			Correct  int `json:"correct"`
		} `json:"stats"`
	}
	submit := func(ref, option string) answersResponse {
		t.Helper()
		var out answersResponse
		if code := c.do(http.MethodPost, "/api/v1/tones/answers", map[string]any{
			"answers": []map[string]any{{
				"exercise_id": exerciseID, "client_ref": ref, "option_id": option,
			}},
		}, &out); code != http.StatusOK {
			t.Fatalf("tone answer: status %d", code)
		}
		if len(out.Results) != 1 {
			t.Fatalf("tone answer returned %d results", len(out.Results))
		}
		return out
	}

	wrongRef := "tone-wrong-" + uuid.NewString()
	wrong := submit(wrongRef, otherOption)
	if wrong.Results[0].IsCorrect == nil || *wrong.Results[0].IsCorrect {
		t.Errorf("a wrong tone was judged %v", wrong.Results[0].IsCorrect)
	}
	if got, _ := wrong.Results[0].Feedback["tones"].(string); got != correctTones {
		t.Errorf("the tones came back as %v, want %q", wrong.Results[0].Feedback["tones"], correctTones)
	}
	if len(wrong.ToneTally) == 0 {
		t.Fatal("no tone tally: the page cannot tell the learner which tone they missed")
	}
	if wrong.ToneTally[0].Asked != 1 || wrong.ToneTally[0].Missed != 1 {
		t.Errorf("tally for a missed answer = asked %d, missed %d", wrong.ToneTally[0].Asked, wrong.ToneTally[0].Missed)
	}

	// The same client_ref for the same drill is a replay, not a second attempt.
	if again := submit(wrongRef, otherOption); !again.Results[0].Duplicate {
		t.Error("resubmitting the same client_ref was not treated as a duplicate")
	}
	right := submit("tone-right-"+uuid.NewString(), correctOption)
	if right.Results[0].IsCorrect == nil || !*right.Results[0].IsCorrect {
		t.Errorf("the right tone was judged %v", right.Results[0].IsCorrect)
	}
	if len(right.Stats) == 0 || right.Stats[0].Answered != 2 || right.Stats[0].Correct != 1 {
		t.Errorf("lifetime tone record = %+v, want 2 answered and 1 correct", right.Stats)
	}

	// --- THE PROPERTY: the path did not move -------------------------------
	var after store.LessonDetail
	if code := c.do(http.MethodGet, "/api/v1/lessons/"+lessonID.String(), nil, &after); code != http.StatusOK {
		t.Fatalf("re-reading the tone lesson: status %d", code)
	}
	if after.State != "locked" {
		t.Errorf("tone practice unlocked a lesson: state went from %q to %q", lesson.State, after.State)
	}

	var progressRows, dailyRows, xpRows, toneAttempts int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM lesson_progress WHERE user_id = $1 AND lesson_id = $2`,
		userID, lessonID).Scan(&progressRows); err != nil {
		t.Fatalf("reading lesson_progress: %v", err)
	}
	if progressRows != 0 {
		t.Errorf("tone practice created %d lesson_progress row(s) for a locked lesson", progressRows)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM daily_activity WHERE user_id = $1`, userID).Scan(&dailyRows); err != nil {
		t.Fatalf("reading daily_activity: %v", err)
	}
	if dailyRows != 0 {
		t.Errorf("tone practice fed the daily rollup (%d rows): it would move the streak", dailyRows)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM xp_events WHERE user_id = $1`, userID).Scan(&xpRows); err != nil {
		t.Fatalf("reading xp_events: %v", err)
	}
	if xpRows != 0 {
		t.Errorf("tone practice awarded %d XP", xpRows)
	}
	// And yet the tone record itself survives — that is the point of recording it.
	if err := st.Pool.QueryRow(ctx, `SELECT count(*) FROM attempts WHERE user_id = $1 AND mode = 'tone'`,
		userID).Scan(&toneAttempts); err != nil {
		t.Fatalf("reading tone attempts: %v", err)
	}
	if toneAttempts != 2 {
		t.Errorf("recorded %d tone attempts, want 2 (the duplicate must not add a third)", toneAttempts)
	}

	// --- this endpoint grades tone drills only -----------------------------
	var otherExercise uuid.UUID
	if err := st.Pool.QueryRow(ctx, `SELECT id FROM exercises WHERE type = 'mcq' LIMIT 1`).Scan(&otherExercise); err != nil {
		t.Fatalf("finding a non-tone exercise: %v", err)
	}
	if code := c.do(http.MethodPost, "/api/v1/tones/answers", map[string]any{
		"answers": []map[string]any{{"exercise_id": otherExercise, "option_id": "a"}},
	}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("a non-tone exercise should be refused with 422, got %d", code)
	}

	t.Logf("tone practice verified: %d drills served, wrong=%v (%v), right=%v, locked lesson stayed %q, no XP and no rollup",
		len(queue.Items), *wrong.Results[0].IsCorrect, wrong.Results[0].Feedback["tones"],
		*right.Results[0].IsCorrect, after.State)
}
