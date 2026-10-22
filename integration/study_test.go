package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// The study guide's vocabulary list, end to end.
//
// This exists because the endpoint shipped returning 500 to every request: its count query
// was handed the learner id as $1 while the SQL never mentioned $1, and Postgres refuses to
// infer a type for a parameter that appears in no expression — "could not determine data
// type of parameter $1 (SQLSTATE 42P18)". The page that calls it had just been routed, and
// nothing in the suite ever made the request. A routed endpoint needs at least one request
// in the suite; this is that request, for every shape of the filter.
//
// Gated on HANPATH_TEST_DATABASE_URL like the rest of this package; run
// `make test-integration`.
func TestStudyGuideVocabularyEndToEnd(t *testing.T) {
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

	if _, err := curriculum.Import(ctx, st, loadContent(t), provisionerFor(t)); err != nil {
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
		"email":        fmt.Sprintf("integration-guide-%d@example.com", time.Now().UnixNano()),
		"display_name": "Guide Reader",
		"password":     "a-long-enough-passphrase",
		"timezone":     "Asia/Jakarta",
	}, &reg); code != http.StatusCreated {
		t.Fatalf("register: status %d", code)
	}
	t.Cleanup(func() {
		if _, err := st.Pool.Exec(context.Background(), `DELETE FROM users WHERE id = $1`, reg.User.ID); err != nil {
			t.Logf("cleanup: %v", err)
		}
	})

	type entry struct {
		Simplified string `json:"simplified"`
		Pinyin     string `json:"pinyin"`
		Level      int    `json:"level"`
		Bookmarked bool   `json:"bookmarked"`
	}
	type page struct {
		Items  []entry `json:"items"`
		Total  int     `json:"total"`
		Level  int     `json:"level"`
		Limit  int     `json:"limit"`
		Offset int     `json:"offset"`
	}

	// Every level: the unfiltered read a learner lands on.
	var every page
	if code := c.do(http.MethodGet, "/api/v1/study/vocabulary?level=0&limit=5", nil, &every); code != http.StatusOK {
		t.Fatalf("vocabulary for every level: status %d, want 200 — this is the shape that used to be a 500", code)
	}
	if every.Total == 0 || len(every.Items) == 0 {
		t.Fatalf("the guide returned nothing: total %d, items %d", every.Total, len(every.Items))
	}
	if len(every.Items) > 5 {
		t.Errorf("limit=5 returned %d items", len(every.Items))
	}
	for _, item := range every.Items {
		if item.Level < 1 {
			t.Errorf("%q came back with level %d", item.Simplified, item.Level)
		}
	}

	// One level: the filter the guide's pills send.
	var levelOne page
	if code := c.do(http.MethodGet, "/api/v1/study/vocabulary?level=1&limit=5", nil, &levelOne); code != http.StatusOK {
		t.Fatalf("vocabulary for level 1: status %d", code)
	}
	if levelOne.Total > every.Total {
		t.Errorf("level 1 holds %d words, more than every level's %d", levelOne.Total, every.Total)
	}
	for _, item := range levelOne.Items {
		if item.Level != 1 {
			t.Errorf("level=1 returned %q at level %d", item.Simplified, item.Level)
		}
	}

	// A search: the same query with the filter that has its own placeholders.
	var searched page
	if code := c.do(http.MethodGet, "/api/v1/study/vocabulary?level=1&search=%E4%BD%A0&limit=5", nil, &searched); code != http.StatusOK {
		t.Fatalf("vocabulary search: status %d", code)
	}
	if searched.Total == 0 {
		t.Fatal("searching 你 at level 1 found nothing: the search filter is not being applied")
	}
	if searched.Total > levelOne.Total {
		t.Errorf("searching narrowed to %d from %d? it returned more", searched.Total, levelOne.Total)
	}

	// Paging must not change the total, and offset must move the window: compare two pages
	// of the SAME filter, never two different ones.
	var pageOne, pageTwo page
	if code := c.do(http.MethodGet, "/api/v1/study/vocabulary?level=1&limit=2&offset=0", nil, &pageOne); code != http.StatusOK {
		t.Fatalf("vocabulary page 1: status %d", code)
	}
	if code := c.do(http.MethodGet, "/api/v1/study/vocabulary?level=1&limit=2&offset=2", nil, &pageTwo); code != http.StatusOK {
		t.Fatalf("vocabulary page 2: status %d", code)
	}
	if pageOne.Total != pageTwo.Total {
		t.Errorf("the total changed between pages: %d then %d", pageOne.Total, pageTwo.Total)
	}
	if len(pageOne.Items) > 0 && len(pageTwo.Items) > 0 && pageOne.Items[0].Simplified == pageTwo.Items[0].Simplified {
		t.Errorf("offset=2 returned the first row again: %q", pageOne.Items[0].Simplified)
	}

	// The grammar tab shares the same handler shapes.
	var grammar struct {
		Items []json.RawMessage `json:"items"`
		Level int               `json:"level"`
	}
	if code := c.do(http.MethodGet, "/api/v1/study/grammar?level=1", nil, &grammar); code != http.StatusOK {
		t.Fatalf("grammar notes: status %d", code)
	}
	if len(grammar.Items) == 0 {
		t.Error("the guide's grammar tab is empty for level 1")
	}
}
