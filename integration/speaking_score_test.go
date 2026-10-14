package integration

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"testing"
	"time"

	"net/http/httptest"

	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// TestPronunciationScoringEndToEnd exercises the one path where a learner's voice
// crosses the network: upload -> provider -> stored score -> graded attempt.
//
// It runs against the fake provider, which is deterministic and needs no vendor
// account. That is deliberate: the fake makes the *pipeline* testable everywhere, and
// the Azure implementation is covered by its own contract test asserting the request
// it sends and the response it parses (internal/speech).
//
// The security property is tested here too. A client can name a recording it owns; it
// can never send a score. Without that, a hand-built payload could award itself the
// pass that completes the lesson and pays the XP.
func TestPronunciationScoringEndToEnd(t *testing.T) {
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
		AppEnv:                "test",
		LogLevel:              "error",
		PublicBase:            "http://127.0.0.1:5174",
		DatabaseURL:           dbURL,
		SessionCookieName:     "hanpath_speech_session",
		CSRFCookieName:        "hanpath_speech_csrf",
		CSRFHeaderName:        "X-CSRF-Token",
		SessionTTL:            time.Hour,
		MaxAttemptsPerRequest: 100,
		// Scoring costs money per call in production, so the caps are part of the
		// contract this test checks.
		SpeechProvider:       "fake",
		SpeechMaxUploadBytes: 256 << 10,
		SpeechDailyCap:       3,
	}
	srv := httptest.NewServer(serverFor(t, cfg, log, st).Routes())
	defer srv.Close()
	c := newClient(t, srv.URL)

	email := fmt.Sprintf("speech-%d@example.com", time.Now().UnixNano())
	// The password avoids common words on purpose: the server refuses one containing
	// "password", which is how this test first failed with a 422.
	if code := c.do("POST", "/api/v1/auth/register", map[string]any{
		"email": email, "display_name": "Integration Speaker",
		"password": "a-long-enough-passphrase", "timezone": "Asia/Jakarta",
	}, nil); code != 201 {
		t.Fatalf("registering: got %d", code)
	}

	// --- find a speaking drill and a non-speaking one ----------------------
	var path struct {
		Levels []struct {
			Units []struct {
				Lessons []struct {
					ID    string `json:"id"`
					State string `json:"state"`
				} `json:"lessons"`
			} `json:"units"`
		} `json:"levels"`
	}
	if code := c.do("GET", "/api/v1/curriculum/hsk2.0/path", nil, &path); code != 200 {
		t.Fatalf("reading the path: got %d", code)
	}
	if len(path.Levels) == 0 || len(path.Levels[0].Units) == 0 || len(path.Levels[0].Units[0].Lessons) == 0 {
		t.Fatal("the path has no first lesson; the content import did not produce a usable curriculum")
	}
	lesson := path.Levels[0].Units[0].Lessons[0]
	if lesson.State != "available" {
		t.Fatalf("the first lesson is %q, so a submission would be refused", lesson.State)
	}

	var detail struct {
		Exercises []struct {
			ID   string `json:"id"`
			Type string `json:"type"`
		} `json:"exercises"`
	}
	if code := c.do("GET", "/api/v1/lessons/"+lesson.ID, nil, &detail); code != 200 {
		t.Fatalf("reading the lesson: got %d", code)
	}
	speakingID, otherID := "", ""
	for _, ex := range detail.Exercises {
		if ex.Type == "speak_repeat" && speakingID == "" {
			speakingID = ex.ID
		}
		if ex.Type == "mcq" && otherID == "" {
			otherID = ex.ID
		}
	}
	if speakingID == "" || otherID == "" {
		t.Fatalf("the first lesson needs a speak_repeat and an mcq drill for this test (got %d exercises)", len(detail.Exercises))
	}

	// --- score a recording --------------------------------------------------
	type scoreResponse struct {
		ID       string  `json:"id"`
		Provider string  `json:"provider"`
		Overall  float64 `json:"overall"`
		Band     float64 `json:"passing_band"`
		Words    []struct {
			Word      string  `json:"word"`
			Accuracy  float64 `json:"accuracy"`
			ErrorType string  `json:"error_type"`
		} `json:"words"`
		Reference string `json:"reference"`
	}
	var scored scoreResponse
	code := c.doMultipart("/api/v1/recordings", map[string]string{"exercise_id": speakingID},
		"audio", "take.wav", testSpeechAudio(4000), &scored)
	if code != 200 {
		t.Fatalf("scoring a recording: got %d", code)
	}
	if scored.ID == "" || scored.Provider != "fake" {
		t.Fatalf("scoring returned %+v", scored)
	}
	if scored.Overall <= 0 || scored.Overall > 1 || scored.Band != 0.7 {
		t.Fatalf("score %v with band %v is out of contract", scored.Overall, scored.Band)
	}
	if len(scored.Words) == 0 || scored.Reference == "" {
		t.Fatalf("a score must carry per-word detail and the reference it judged against: %+v", scored)
	}
	t.Logf("scored %v via %s, %d words, band %v", scored.Overall, scored.Provider, len(scored.Words), scored.Band)

	// --- the attempt uses the stored score ---------------------------------
	type attemptSummary struct {
		Results []struct {
			IsCorrect   *bool          `json:"is_correct"`
			Score       float64        `json:"score"`
			NeedsReview bool           `json:"needs_review"`
			Grader      string         `json:"grader"`
			Feedback    map[string]any `json:"feedback"`
		} `json:"results"`
	}
	submit := func(ref string, response map[string]any) attemptSummary {
		t.Helper()
		var summary attemptSummary
		body := map[string]any{"mode": "practice", "attempts": []any{map[string]any{
			"exercise_id": speakingID, "client_ref": ref, "response": response, "duration_ms": 2000,
		}}}
		if code := c.do("POST", "/api/v1/lessons/"+lesson.ID+"/attempts", body, &summary); code != 200 {
			t.Fatalf("submitting %s: got %d", ref, code)
		}
		if len(summary.Results) != 1 {
			t.Fatalf("expected one graded result, got %d", len(summary.Results))
		}
		return summary
	}

	graded := submit("speech-scored-1", map[string]any{
		"duration_ms": 2000, "recording_id": scored.ID, "self_rating": 4,
	})
	result := graded.Results[0]
	if result.IsCorrect != nil {
		t.Errorf("is_correct is %v; a pronunciation score must never be reported as right or wrong", *result.IsCorrect)
	}
	// Compared with a tolerance, not for equality: the score is stored as numeric(5,4),
	// so the value grading reads back is the provider's number rounded to four places.
	// The rounding is deliberate — a stored score is a record, not a float — and a
	// learner should never see the two disagree.
	if diff := result.Score - scored.Overall; diff > 1e-4 || diff < -1e-4 {
		t.Errorf("the graded score is %v but the provider measured %v — grading must use the stored score", result.Score, scored.Overall)
	}
	if result.NeedsReview {
		t.Error("a scored drill must stop asking for human review; that is the point of scoring it")
	}
	if result.Feedback["provider"] != "fake" || result.Feedback["has_speech_recognition"] != true {
		t.Errorf("feedback does not report the measurement: %#v", result.Feedback)
	}
	if _, ok := result.Feedback["words"]; !ok {
		t.Error("the per-word assessment should reach the learner")
	}

	// --- a client cannot send its own score --------------------------------
	forged := submit("speech-forged-1", map[string]any{
		"duration_ms": 2000,
		"score":       1.0,
		"provider":    "attacker",
		"accuracy":    1.0,
		"words":       []any{map[string]any{"word": "你", "accuracy": 1.0, "error_type": "None"}},
	})
	forgedResult := forged.Results[0]
	if forgedResult.Feedback["has_speech_recognition"] == true || forgedResult.Feedback["provider"] == "attacker" {
		t.Fatalf("a client-supplied score was accepted: %#v", forgedResult.Feedback)
	}
	if !forgedResult.NeedsReview {
		t.Error("an unscored attempt must still be reported as needing review")
	}
	if _, ok := forgedResult.Feedback["words"]; ok {
		t.Error("forged per-word detail reached the feedback")
	}

	// --- the endpoint's own limits -----------------------------------------
	if code := c.doMultipart("/api/v1/recordings", map[string]string{"exercise_id": otherID},
		"audio", "take.wav", testSpeechAudio(1000), nil); code != 422 {
		t.Errorf("scoring a multiple-choice question: got %d, want 422", code)
	}
	if code := c.doMultipart("/api/v1/recordings", map[string]string{"exercise_id": speakingID},
		"not_audio", "take.wav", testSpeechAudio(1000), nil); code != 400 {
		t.Errorf("an upload with no audio part: got %d, want 400", code)
	}
	if code := c.doMultipart("/api/v1/recordings", map[string]string{"exercise_id": speakingID},
		"audio", "take.wav", testSpeechAudio(400<<10), nil); code != 413 {
		t.Errorf("an upload over the cap: got %d, want 413", code)
	}

	// The daily cap is a spending limit: with a cap of three, the third scoring is
	// allowed and the fourth is refused. A cap that triggers one call early would be a
	// bug a learner notices, so the boundary is asserted from both sides.
	for n := 2; n <= 3; n++ {
		if code := c.doMultipart("/api/v1/recordings", map[string]string{"exercise_id": speakingID},
			"audio", "take.wav", testSpeechAudio(4000), nil); code != 200 {
			t.Fatalf("scoring %d of the cap should be allowed: got %d", n, code)
		}
	}
	if code := c.doMultipart("/api/v1/recordings", map[string]string{"exercise_id": speakingID},
		"audio", "take.wav", testSpeechAudio(4000), nil); code != 429 {
		t.Errorf("scoring past the daily cap: got %d, want 429", code)
	}
}

// testSpeechAudio is a small but real WAV. The fake provider scores whatever it is
// given, so this only needs to be valid audio of a believable size — and generating it
// here keeps the test independent of ffmpeg, which the content import already needs.
func testSpeechAudio(samples int) []byte {
	const rate = 16000
	out := make([]byte, 0, 44+samples*2)
	u32 := func(v uint32) { out = append(out, byte(v), byte(v>>8), byte(v>>16), byte(v>>24)) }
	u16 := func(v uint16) { out = append(out, byte(v), byte(v>>8)) }
	out = append(out, "RIFF"...)
	u32(uint32(36 + samples*2))
	out = append(out, "WAVEfmt "...)
	u32(16)
	u16(1)
	u16(1)
	u32(rate)
	u32(rate * 2)
	u16(2)
	u16(16)
	out = append(out, "data"...)
	u32(uint32(samples * 2))
	for i := 0; i < samples; i++ {
		// A quiet sawtooth: non-silent, deterministic, and cheap.
		v := int16((i % 64) * 200)
		u16(uint16(v))
	}
	return out
}
