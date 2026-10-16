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

	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// Proficiency tests, end to end.
//
// Two properties carry this feature, and both are asserted here rather than assumed:
//
//   - A paper moves NOTHING. It is drawn from a level the learner has not walked, so its
//     attempts must leave lesson status, XP, the daily rollup and the streak exactly as
//     they were, and a locked lesson must stay locked even though questions from it were
//     in the paper that was just sat.
//   - A pass opens the level. Its lessons become available, and a lesson that refused a
//     submission a moment ago accepts one — the inverse of the lock test in the level
//     walk, which is the whole point of the feature.
//
// Gated on HANPATH_TEST_DATABASE_URL like the rest of this package; run
// `make test-integration`.
func TestProficiencyTestOpensALevel(t *testing.T) {
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

	// Level 1 and its tone unit: enough gradable questions (meaning, listening and tone)
	// to fill a twenty-question paper, and all of it importable in seconds.
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
		"email":        fmt.Sprintf("integration-proficiency-%d@example.com", time.Now().UnixNano()),
		"display_name": "Paper Sitter",
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

	// --- the cards a learner chooses from ---------------------------------
	type cardsResponse struct {
		Cards []struct {
			Level     int      `json:"level"`
			Questions int      `json:"questions"`
			Unlocked  bool     `json:"unlocked_by_test"`
			BestScore *float64 `json:"best_score"`
			Passed    bool     `json:"passed"`
			Papers    int      `json:"papers_taken"`
		} `json:"cards"`
		PassThreshold float64 `json:"pass_threshold"`
	}
	var cards cardsResponse
	if code := c.do(http.MethodGet, "/api/v1/proficiency/cards", nil, &cards); code != http.StatusOK {
		t.Fatalf("cards: status %d", code)
	}
	if cards.PassThreshold < 0.79 || cards.PassThreshold > 0.81 {
		t.Errorf("pass threshold = %v, want 0.8", cards.PassThreshold)
	}
	var levelOneQuestions int
	for _, card := range cards.Cards {
		if card.Level == 1 {
			levelOneQuestions = card.Questions
			if card.Unlocked {
				t.Error("a fresh learner's level 1 is already marked opened by a test")
			}
		}
	}
	if levelOneQuestions == 0 {
		t.Fatal("level 1 reports no gradable questions, so no paper can be drawn")
	}

	// --- a locked lesson to watch, and the paper that samples it -----------
	var lockedLesson uuid.UUID
	if err := st.Pool.QueryRow(ctx, `
		SELECT ls.id
		FROM lessons ls
		JOIN units u ON u.id = ls.unit_id
		JOIN levels l ON l.id = u.level_id
		WHERE l.level_number = 1 AND u.unit_number = (SELECT max(unit_number) FROM units u2 JOIN levels l2 ON l2.id = u2.level_id WHERE l2.level_number = 1)
		ORDER BY ls.lesson_number
		LIMIT 1`).Scan(&lockedLesson); err != nil {
		t.Fatalf("finding a locked lesson: %v", err)
	}
	var lessonBefore store.LessonDetail
	if code := c.do(http.MethodGet, "/api/v1/lessons/"+lockedLesson.String(), nil, &lessonBefore); code != http.StatusOK {
		t.Fatalf("reading the last unit's lesson: status %d", code)
	}
	if lessonBefore.State != "locked" {
		t.Fatalf("this test needs a locked lesson; %q is %q", lessonBefore.Title, lessonBefore.State)
	}
	// A question that BELONGS to that lesson: the lesson endpoint refuses an exercise from
	// another lesson with 422, so using one of the paper's questions here would make the
	// lock check pass for the wrong reason.
	var lessonExercise uuid.UUID
	if err := st.Pool.QueryRow(ctx, `SELECT id FROM exercises WHERE lesson_id = $1 ORDER BY exercise_number LIMIT 1`,
		lockedLesson).Scan(&lessonExercise); err != nil {
		t.Fatalf("finding a question in the locked lesson: %v", err)
	}

	type paperResponse struct {
		PaperID   uuid.UUID `json:"paper_id"`
		Questions []struct {
			ID     uuid.UUID      `json:"id"`
			Type   string         `json:"type"`
			Prompt map[string]any `json:"prompt"`
		} `json:"questions"`
		PassThreshold float64 `json:"pass_threshold"`
	}
	drawPaper := func() paperResponse {
		t.Helper()
		var paper paperResponse
		if code := c.do(http.MethodGet, "/api/v1/proficiency/1", nil, &paper); code != http.StatusOK {
			t.Fatalf("drawing a paper: status %d", code)
		}
		if len(paper.Questions) == 0 {
			t.Fatal("the paper is empty")
		}
		return paper
	}
	type paperResult struct {
		Passed       bool `json:"passed"`
		Percent      int  `json:"percent"`
		CorrectCount int  `json:"correct_count"`
		GradedCount  int  `json:"graded_count"`
	}
	type submitResponse struct {
		Paper       paperResult `json:"paper"`
		UnlockedNow bool        `json:"unlocked_now"`
	}

	// answer builds a submission for a whole paper. `correct` decides which answers are
	// right, from the stored keys — never from the page.
	answer := func(paper paperResponse, correct bool) []map[string]any {
		t.Helper()
		ids := make([]uuid.UUID, 0, len(paper.Questions))
		for _, q := range paper.Questions {
			ids = append(ids, q.ID)
		}
		specs, err := st.ExercisesForGrading(ctx, ids)
		if err != nil {
			t.Fatalf("loading answer keys: %v", err)
		}
		out := make([]map[string]any, 0, len(paper.Questions))
		for _, q := range paper.Questions {
			spec, ok := specs[q.ID]
			if !ok {
				t.Fatalf("the paper contains exercise %s, which the database does not know", q.ID)
			}
			var response map[string]any
			if correct {
				response = responseFor(t, spec.Type, spec.AnswerKey)
			} else {
				switch spec.Type {
				case "mcq", "listening_mcq", "tone_mcq":
					response = map[string]any{"option_id": "not-an-option"}
				default:
					response = map[string]any{"text": "不对"}
				}
			}
			out = append(out, map[string]any{
				"exercise_id": q.ID,
				"client_ref":  fmt.Sprintf("%s-%s-%v", paper.PaperID, q.ID, correct),
				"response":    response,
			})
		}
		return out
	}

	first := drawPaper()
	if len(first.Questions) != store.ProficiencyPaperSize {
		t.Errorf("paper has %d questions, want %d", len(first.Questions), store.ProficiencyPaperSize)
	}
	seen := map[uuid.UUID]bool{}
	for _, q := range first.Questions {
		if seen[q.ID] {
			t.Errorf("the paper asks the same question twice: %s", q.ID)
		}
		seen[q.ID] = true
		for _, forbidden := range []string{"answer_key", "correct_id", "tones"} {
			if _, leaked := q.Prompt[forbidden]; leaked {
				t.Errorf("the paper leaked %q in a prompt", forbidden)
			}
		}
	}
	// The paper samples content the learner has not reached — that is what makes the
	// off-path rule necessary rather than decorative.
	// Counted over the level's LAST UNIT, not one lesson: the tone drills are spread across
	// that unit's eight lessons, so pinning one lesson asks a question the paper never
	// promised to answer.
	var sampledLocked int
	if err := st.Pool.QueryRow(ctx, `
		SELECT count(*)::int
		FROM exercises e
		JOIN lessons ls ON ls.id = e.lesson_id
		JOIN units u ON u.id = ls.unit_id
		WHERE e.id = ANY($1)
		  AND u.id = (SELECT unit_id FROM lessons WHERE id = $2)`, idsOf(first.Questions), lockedLesson).Scan(&sampledLocked); err != nil {
		t.Fatalf("counting sampled locked questions: %v", err)
	}
	// Not decorative: the off-path rule only matters because a paper asks about lessons
	// the learner has not reached. In level 1 that is guaranteed — every tone drill lives
	// in the level's last (locked) unit and the paper asks for three of them.
	if sampledLocked == 0 {
		t.Error("the paper asked nothing from a locked lesson, so it does not exercise the off-path rule")
	}
	t.Logf("paper: %d questions, %d of them from a locked lesson", len(first.Questions), sampledLocked)

	// --- a failed paper changes nothing -------------------------------------
	var failed submitResponse
	if code := c.do(http.MethodPost, "/api/v1/proficiency/1/submit", map[string]any{
		"paper_id": first.PaperID,
		"answers":  answer(first, false),
	}, &failed); code != http.StatusOK {
		t.Fatalf("submitting a failed paper: status %d", code)
	}
	if failed.Paper.Passed {
		t.Error("a paper with no correct answers passed")
	}
	if failed.UnlockedNow {
		t.Error("a failed paper opened the level")
	}

	// The same paper cannot be sat twice.
	if code := c.do(http.MethodPost, "/api/v1/proficiency/1/submit", map[string]any{
		"paper_id": first.PaperID,
		"answers":  answer(first, true),
	}, nil); code != http.StatusConflict {
		t.Errorf("sitting a paper twice: status %d, want 409", code)
	}

	// Nothing moved: not the path, not the derived tables.
	var afterFail store.LessonDetail
	if code := c.do(http.MethodGet, "/api/v1/lessons/"+lockedLesson.String(), nil, &afterFail); code != http.StatusOK {
		t.Fatalf("re-reading the locked lesson: status %d", code)
	}
	if afterFail.State != "locked" {
		t.Errorf("a paper opened a lesson: %q became %q", lessonBefore.Title, afterFail.State)
	}
	if code := c.do(http.MethodPost, "/api/v1/lessons/"+lockedLesson.String()+"/attempts", map[string]any{
		"attempts": []map[string]any{{
			"exercise_id": lessonExercise,
			"response":    map[string]any{"option_id": "a"},
		}},
	}, nil); code != http.StatusForbidden {
		t.Errorf("a locked lesson accepted a submission after a failed paper: status %d, want 403", code)
	}
	var progressRows, xpRows, dailyRows, unlockRows, paperRows int
	// The dashboard's counters are derived from attempts too, so they are part of "nothing
	// moved": a paper's answers must not read as path answers.
	type overviewCounts struct {
		TotalAttempts int `json:"total_attempts"`
		TotalXP       int `json:"total_xp"`
	}
	var countsBefore overviewCounts
	if code := c.do(http.MethodGet, "/api/v1/progress/overview", nil, &countsBefore); code != http.StatusOK {
		t.Fatalf("reading the overview before the paper: status %d", code)
	}

	checks := []struct {
		name string
		sql  string
		out  *int
	}{
		{"lesson_progress", `SELECT count(*)::int FROM lesson_progress WHERE user_id = $1`, &progressRows},
		{"xp_events", `SELECT count(*)::int FROM xp_events WHERE user_id = $1`, &xpRows},
		{"daily_activity", `SELECT count(*)::int FROM daily_activity WHERE user_id = $1`, &dailyRows},
		{"level_unlocks", `SELECT count(*)::int FROM level_unlocks WHERE user_id = $1`, &unlockRows},
		{"proficiency_papers (sat)", `SELECT count(*)::int FROM proficiency_papers WHERE user_id = $1 AND submitted_at IS NOT NULL`, &paperRows},
	}
	for _, check := range checks {
		if err := st.Pool.QueryRow(ctx, check.sql, userID).Scan(check.out); err != nil {
			t.Fatalf("reading %s: %v", check.name, err)
		}
	}
	if progressRows != 0 || xpRows != 0 || dailyRows != 0 || unlockRows != 0 {
		t.Errorf("a failed paper moved derived state: lesson_progress=%d xp=%d daily=%d unlocks=%d",
			progressRows, xpRows, dailyRows, unlockRows)
	}
	if paperRows != 1 {
		t.Errorf("recorded %d sat papers, want 1 — a failed attempt is still a record", paperRows)
	}
	var countsAfter overviewCounts
	if code := c.do(http.MethodGet, "/api/v1/progress/overview", nil, &countsAfter); code != http.StatusOK {
		t.Fatalf("reading the overview after the paper: status %d", code)
	}
	if countsAfter.TotalAttempts != countsBefore.TotalAttempts || countsAfter.TotalXP != countsBefore.TotalXP {
		t.Errorf("a paper showed up as path progress: attempts %d -> %d, xp %d -> %d",
			countsBefore.TotalAttempts, countsAfter.TotalAttempts, countsBefore.TotalXP, countsAfter.TotalXP)
	}

	// --- an incomplete or foreign submission is refused ----------------------
	second := drawPaper()
	partial := answer(second, true)
	if code := c.do(http.MethodPost, "/api/v1/proficiency/1/submit", map[string]any{
		"paper_id": second.PaperID,
		"answers":  partial[:len(partial)-1],
	}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("an incomplete paper: status %d, want 422", code)
	}
	// A question from the same level that is NOT in this paper cannot be swapped in.
	var foreign uuid.UUID
	if err := st.Pool.QueryRow(ctx, `
		SELECT e.id FROM exercises e
		JOIN lessons ls ON ls.id = e.lesson_id
		JOIN units u ON u.id = ls.unit_id
		JOIN levels l ON l.id = u.level_id
		WHERE l.level_number = 1 AND NOT (e.id = ANY($1))
		LIMIT 1`, idsOf(second.Questions)).Scan(&foreign); err != nil {
		t.Fatalf("finding a question outside the paper: %v", err)
	}
	swapped := append(partial[:len(partial)-1], map[string]any{
		"exercise_id": foreign, "client_ref": "swapped", "response": map[string]any{"option_id": "a"},
	})
	if code := c.do(http.MethodPost, "/api/v1/proficiency/1/submit", map[string]any{
		"paper_id": second.PaperID,
		"answers":  swapped,
	}, nil); code != http.StatusUnprocessableEntity {
		t.Errorf("a question outside the paper: status %d, want 422", code)
	}

	// --- a passing paper opens the level ------------------------------------
	var passed submitResponse
	if code := c.do(http.MethodPost, "/api/v1/proficiency/1/submit", map[string]any{
		"paper_id": second.PaperID,
		"answers":  answer(second, true),
	}, &passed); code != http.StatusOK {
		t.Fatalf("submitting a passing paper: status %d", code)
	}
	if !passed.Paper.Passed {
		t.Fatalf("a paper with every answer correct did not pass: %+v", passed.Paper)
	}
	if !passed.UnlockedNow {
		t.Error("the passing paper did not report opening the level")
	}

	var afterPass store.LessonDetail
	if code := c.do(http.MethodGet, "/api/v1/lessons/"+lockedLesson.String(), nil, &afterPass); code != http.StatusOK {
		t.Fatalf("reading the lesson after a pass: status %d", code)
	}
	if afterPass.State == "locked" {
		t.Errorf("the lesson is still locked after a pass: %q", afterPass.State)
	}
	// ...and it now accepts a submission, which it refused a few lines ago.
	if code := c.do(http.MethodPost, "/api/v1/lessons/"+lockedLesson.String()+"/attempts", map[string]any{
		"attempts": []map[string]any{{
			"exercise_id": lessonExercise,
			"response":    map[string]any{"option_id": "a"},
		}},
	}, nil); code != http.StatusOK {
		t.Errorf("the opened lesson refused a submission: status %d, want 200", code)
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*)::int FROM level_unlocks WHERE user_id = $1`, userID).Scan(&unlockRows); err != nil {
		t.Fatalf("reading unlocks: %v", err)
	}
	if unlockRows != 1 {
		t.Errorf("recorded %d unlocks, want 1", unlockRows)
	}

	// --- a retake does not double-unlock -------------------------------------
	third := drawPaper()
	var again submitResponse
	if code := c.do(http.MethodPost, "/api/v1/proficiency/1/submit", map[string]any{
		"paper_id": third.PaperID,
		"answers":  answer(third, true),
	}, &again); code != http.StatusOK {
		t.Fatalf("retaking a passed level: status %d", code)
	}
	if !again.Paper.Passed {
		t.Error("a second passing paper was not recorded as passed")
	}
	if again.UnlockedNow {
		t.Error("a retake reported opening a level that was already open")
	}
	if err := st.Pool.QueryRow(ctx, `SELECT count(*)::int FROM level_unlocks WHERE user_id = $1`, userID).Scan(&unlockRows); err != nil {
		t.Fatalf("reading unlocks after a retake: %v", err)
	}
	if unlockRows != 1 {
		t.Errorf("a retake produced %d unlocks, want 1", unlockRows)
	}

	t.Logf("proficiency verified: %d-question papers, failed paper moved nothing and kept the lesson locked, "+
		"passing paper made %q %s and accepted a submission, retake did not unlock twice",
		len(first.Questions), lessonBefore.Title, afterPass.State)
}

func idsOf(questions []struct {
	ID     uuid.UUID      `json:"id"`
	Type   string         `json:"type"`
	Prompt map[string]any `json:"prompt"`
}) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(questions))
	for _, q := range questions {
		ids = append(ids, q.ID)
	}
	return ids
}
