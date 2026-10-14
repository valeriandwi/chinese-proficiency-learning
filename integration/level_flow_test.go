package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
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

// TestCompleteEntireLevel walks the whole authored level in path order, which is
// what exercises the parts a single lesson cannot: every exercise type in the
// registry (including the free-form rubric grader), the unlock progression across
// units, and the aggregate XP and review-deck totals.
//
// Gated on HANPATH_TEST_DATABASE_URL like the rest of this package; run
// `make test-integration`.
func TestCompleteEntireLevel(t *testing.T) {
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

	doc := loadContent(t)
	if _, err := curriculum.Import(ctx, st, doc, provisionerFor(t)); err != nil {
		t.Fatalf("importing content: %v", err)
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
		"email":        fmt.Sprintf("integration-level-%d@example.com", time.Now().UnixNano()),
		"display_name": "Level Walker",
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

	ordered := pathLessonIDs(t, c)
	t.Logf("path has %d lessons; walking them in order", len(ordered))

	exercisesSeen := map[string]bool{}
	for i, lessonID := range ordered {
		var lesson store.LessonDetail
		if code := c.do(http.MethodGet, "/api/v1/lessons/"+lessonID.String(), nil, &lesson); code != http.StatusOK {
			t.Fatalf("lesson %d: status %d", i, code)
		}
		// Before its turn, every later lesson must still be locked.
		if i > 0 && lesson.State != "available" && lesson.State != "in_progress" {
			t.Fatalf("lesson %d (%s) should be available, got %q", i, lesson.Title, lesson.State)
		}

		inputs := correctAnswers(t, ctx, st, lessonID, lesson.Exercises)
		var summary attempts.Summary
		if code := c.do(http.MethodPost, "/api/v1/lessons/"+lessonID.String()+"/attempts", map[string]any{
			"mode": "practice", "attempts": inputs,
		}, &summary); code != http.StatusOK {
			t.Fatalf("lesson %d submissions: status %d", i, code)
		}
		for j, res := range summary.Results {
			typ := lesson.Exercises[j].Type
			exercisesSeen[typ] = true
			// Free-form writing is rubric-judged and speaking is not judged at all
			// (no speech recognition in this build). Both must come back with a nil
			// is_correct and a review flag, while every other type — listening
			// included — must be graded correct when its answer key is submitted.
			if exercises.IsMechanicallyGradable(typ) && typ != "short_answer" {
				if res.IsCorrect == nil || !*res.IsCorrect {
					t.Errorf("lesson %d exercise %d (%s): correct answer graded as %v (score %.2f)",
						i, lesson.Exercises[j].Number, typ, res.IsCorrect, res.Score)
				}
				continue
			}
			if res.IsCorrect != nil {
				t.Errorf("lesson %d: %s was graded %v although it cannot be judged mechanically", i, typ, *res.IsCorrect)
			}
			if !res.NeedsReview {
				t.Errorf("lesson %d: %s was not flagged needs_review", i, typ)
			}
		}
		if len(summary.Lessons) != 1 || summary.Lessons[0].Status != "completed" {
			t.Fatalf("lesson %d (%s) did not complete: %+v", i, lesson.Title, summary.Lessons)
		}
		t.Logf("  ✓ %-32s %d exercises, %d XP", lesson.Title, len(lesson.Exercises), summary.Lessons[0].XPAwarded)
	}

	// Every registry type must have been exercised, otherwise this test is
	// silently covering less than it claims.
	for _, typ := range []string{"mcq", "mcq_multi", "true_false", "fill_blank", "sentence_order", "translation", "hanzi_trace", "hanzi_recall", "short_answer"} {
		if !exercisesSeen[typ] {
			t.Errorf("exercise type %q was never exercised by the authored content", typ)
		}
	}
	t.Logf("exercise types exercised: %d", len(exercisesSeen))

	// --- final path state --------------------------------------------------
	var final struct {
		Levels []struct {
			Units []struct {
				Lessons []struct {
					State string `json:"state"`
				} `json:"lessons"`
			} `json:"units"`
		} `json:"levels"`
	}
	if code := c.do(http.MethodGet, "/api/v1/curriculum/hsk2.0/path", nil, &final); code != http.StatusOK {
		t.Fatalf("final path: status %d", code)
	}
	for _, lv := range final.Levels {
		for _, u := range lv.Units {
			for _, l := range u.Lessons {
				if l.State != "completed" {
					t.Errorf("after walking the whole path a lesson is still %q", l.State)
				}
			}
		}
	}

	// --- aggregates --------------------------------------------------------
	var expectedXP int
	if err := st.Pool.QueryRow(ctx, `
		SELECT coalesce(sum(ls.xp_reward), 0)::int
		FROM lessons ls
		JOIN units u ON u.id = ls.unit_id
		JOIN levels l ON l.id = u.level_id
		JOIN curricula c ON c.id = l.curriculum_id
		WHERE c.version = 'hsk2.0'`).Scan(&expectedXP); err != nil {
		t.Fatalf("summing authored XP: %v", err)
	}
	var actualXP int
	if err := st.Pool.QueryRow(ctx, `SELECT coalesce(sum(amount), 0)::int FROM xp_events WHERE user_id = $1`, userID).
		Scan(&actualXP); err != nil {
		t.Fatalf("summing awarded XP: %v", err)
	}
	if actualXP != expectedXP {
		t.Errorf("awarded XP = %d, want %d (one award per completed lesson)", actualXP, expectedXP)
	}

	// Every authored word must be reachable: no lexeme may sit in the curriculum
	// without a lesson teaching it, or it can never enter a learner's review deck.
	var unreachable int
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM lexemes x
		JOIN lexeme_levels ll ON ll.lexeme_id = x.id
		JOIN curricula c ON c.id = ll.curriculum_id
		WHERE c.version = 'hsk2.0'
		  AND NOT EXISTS (SELECT 1 FROM exercise_lexemes el WHERE el.lexeme_id = x.id)`).Scan(&unreachable); err != nil {
		t.Fatalf("checking for unreachable vocabulary: %v", err)
	}
	if unreachable > 0 {
		t.Errorf("%d authored lexeme(s) are not taught by any lesson and can never be reviewed", unreachable)
	}

	var deck int
	if err := st.Pool.QueryRow(ctx, `SELECT count(*)::int FROM review_items WHERE user_id = $1`, userID).Scan(&deck); err != nil {
		t.Fatalf("counting the review deck: %v", err)
	}
	var levelWords int
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM lexemes x
		JOIN lexeme_levels ll ON ll.lexeme_id = x.id
		JOIN curricula c ON c.id = ll.curriculum_id
		WHERE c.version = 'hsk2.0' AND ll.level_number = 1`).Scan(&levelWords); err != nil {
		t.Fatalf("counting level vocabulary: %v", err)
	}
	if deck != levelWords {
		t.Errorf("review deck holds %d words, want all %d taught at this level", deck, levelWords)
	}

	// --- a client_ref reused for a different exercise is rejected ------------
	if code := c.do(http.MethodPost, "/api/v1/lessons/"+ordered[1].String()+"/attempts", map[string]any{
		"mode": "practice",
		"attempts": []map[string]any{{
			"exercise_id": firstExerciseOf(t, c, ordered[1]),
			"client_ref":  fmt.Sprintf("integration-%s-1", ordered[0].String()[:8]),
			"response":    map[string]any{"option_id": "a"},
			"duration_ms": 1200,
		}},
	}, nil); code != http.StatusConflict {
		t.Errorf("reusing a client_ref for another exercise returned %d, want 409", code)
	} else {
		t.Log("a client_ref reused for a different exercise is rejected with 409")
	}

	var overview store.ProgressOverview
	if code := c.do(http.MethodGet, "/api/v1/progress/overview", nil, &overview); code != http.StatusOK {
		t.Fatalf("overview: status %d", code)
	}
	if overview.LessonsDone != len(ordered) {
		t.Errorf("overview reports %d completed lessons, want %d", overview.LessonsDone, len(ordered))
	}
	t.Logf("aggregates: %d XP, %d completed lessons, %d review cards, %d skills, streak %d",
		actualXP, overview.LessonsDone, deck, len(overview.Skills), overview.Streak.CurrentStreak)

	// The default goal is 20 minutes and a walk-through records only a couple of
	// minutes, so the day's goal is unmet and the streak is correctly 0: a streak
	// counts days whose goal was met, not days with any activity at all.
	if overview.Streak.GoalMetToday || overview.Streak.CurrentStreak != 0 {
		t.Errorf("with a 20-minute goal and ~2 minutes recorded, expected goal_met=false and streak 0, got goal_met=%v streak=%d",
			overview.Streak.GoalMetToday, overview.Streak.CurrentStreak)
	}

	// Switching the goal to something the day has already satisfied must flip both
	// the flag and the streak. This is the check that the goal setting is actually
	// read by the rollup rather than merely stored and echoed back.
	if code := c.do(http.MethodPatch, "/api/v1/me", map[string]any{
		"daily_goal_kind": "xp", "daily_goal_value": 50,
	}, nil); code != http.StatusOK {
		t.Fatalf("switching the daily goal to 50 XP: status %d", code)
	}
	var afterGoal store.ProgressOverview
	if code := c.do(http.MethodGet, "/api/v1/progress/overview", nil, &afterGoal); code != http.StatusOK {
		t.Fatalf("overview after the goal change: status %d", code)
	}
	if !afterGoal.Streak.GoalMetToday || afterGoal.Streak.CurrentStreak < 1 {
		t.Errorf("after a 50 XP goal with %d XP banked today: goal_met=%v streak=%d, want true and >= 1",
			afterGoal.Streak.TodayXP, afterGoal.Streak.GoalMetToday, afterGoal.Streak.CurrentStreak)
	}
	t.Logf("goal change recomputed the rollup: %d XP today now meets the goal, streak %d",
		afterGoal.Streak.TodayXP, afterGoal.Streak.CurrentStreak)
}

// pathLessonIDs returns every lesson id in path order.
func pathLessonIDs(t *testing.T, c *client) []uuid.UUID {
	t.Helper()
	var path struct {
		Levels []struct {
			Units []struct {
				Lessons []struct {
					ID uuid.UUID `json:"id"`
				} `json:"lessons"`
			} `json:"units"`
		} `json:"levels"`
	}
	if code := c.do(http.MethodGet, "/api/v1/curriculum/hsk2.0/path", nil, &path); code != http.StatusOK {
		t.Fatalf("path: status %d", code)
	}
	var out []uuid.UUID
	for _, lv := range path.Levels {
		for _, u := range lv.Units {
			for _, l := range u.Lessons {
				out = append(out, l.ID)
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("the path is empty: is the content imported?")
	}
	return out
}

// firstExerciseOf returns the first exercise id of a lesson.
func firstExerciseOf(t *testing.T, c *client, lessonID uuid.UUID) uuid.UUID {
	t.Helper()
	var lesson store.LessonDetail
	if code := c.do(http.MethodGet, "/api/v1/lessons/"+lessonID.String(), nil, &lesson); code != http.StatusOK {
		t.Fatalf("lesson %s: status %d", lessonID, code)
	}
	if len(lesson.Exercises) == 0 {
		t.Fatalf("lesson %s has no exercises", lessonID)
	}
	return lesson.Exercises[0].ID
}
