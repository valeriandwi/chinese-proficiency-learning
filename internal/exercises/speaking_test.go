package exercises

import (
	"encoding/json"
	"strings"
	"testing"
)

// Listening and speaking are the two families where it is easy to ship something
// that looks like a feature and is not one: an item with no audio, or an
// automated pass that means nothing. These tests pin both.

func TestListeningMCQ(t *testing.T) {
	prompt := `{"instruction":"What time is it?","audio_key":"abc123.mp3","options":[{"id":"a","text":"3:10"},{"id":"b","text":"10:03"},{"id":"c","text":"3:30"}]}`
	key := `{"correct_id":"a","transcript":"三点十分","why":"The hour comes first with 点."}`

	t.Run("a correct choice is graded correct and reveals the transcript", func(t *testing.T) {
		res, err := Grade("listening_mcq", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(`{"option_id":"a"}`))
		if err != nil {
			t.Fatalf("Grade: %v", err)
		}
		if res.IsCorrect == nil || !*res.IsCorrect || res.Score != 1 {
			t.Fatalf("expected a correct result, got %+v", res)
		}
		if res.Feedback["transcript"] != "三点十分" {
			t.Errorf("the transcript must be revealed after answering, got %v", res.Feedback["transcript"])
		}
	})

	t.Run("a wrong choice is graded wrong and still reveals the transcript", func(t *testing.T) {
		res, err := Grade("listening_mcq", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(`{"option_id":"c"}`))
		if err != nil {
			t.Fatalf("Grade: %v", err)
		}
		if res.IsCorrect == nil || *res.IsCorrect {
			t.Fatalf("expected an incorrect result, got %+v", res)
		}
		if res.Feedback["transcript"] != "三点十分" {
			t.Errorf("the transcript is what makes a listening item reviewable; it must be present")
		}
	})

	t.Run("an option that is not an option is wrong, not a server error", func(t *testing.T) {
		res, err := Grade("listening_mcq", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(`{"option_id":"zzz"}`))
		if err != nil {
			t.Fatalf("Grade returned an error for a stale client payload: %v", err)
		}
		if res.IsCorrect == nil || *res.IsCorrect {
			t.Fatalf("expected an incorrect result, got %+v", res)
		}
	})
}

func TestValidateRejectsBrokenListeningContent(t *testing.T) {
	cases := []struct {
		name      string
		exerciseT string
		prompt    string
		answerKey string
		wantErr   string
	}{
		{
			name: "listening_mcq with no audio",
			// This is the invariant the whole audio pipeline exists for: an item
			// that asks the learner to listen must have something to listen to.
			exerciseT: "listening_mcq",
			prompt:    `{"instruction":"Pick","options":[{"id":"a","text":"x"},{"id":"b","text":"y"}]}`,
			answerKey: `{"correct_id":"a","transcript":"x"}`,
			wantErr:   "audio_key",
		},
		{
			name:      "listening_mcq with a transcript in the prompt",
			exerciseT: "listening_mcq",
			prompt:    `{"instruction":"Pick","audio_key":"a.mp3","transcript":"三点十分","options":[{"id":"a","text":"x"},{"id":"b","text":"y"}]}`,
			answerKey: `{"correct_id":"a","transcript":"x"}`,
			wantErr:   "unknown field",
		},
		{
			name:      "listening_mcq with no transcript in the answer key",
			exerciseT: "listening_mcq",
			prompt:    `{"instruction":"Pick","audio_key":"a.mp3","options":[{"id":"a","text":"x"},{"id":"b","text":"y"}]}`,
			answerKey: `{"correct_id":"a"}`,
			wantErr:   "transcript",
		},
		{
			name:      "listening_dictation with no audio",
			exerciseT: "listening_dictation",
			prompt:    `{"instruction":"Type what you hear"}`,
			answerKey: `{"accepted":["你好"],"transcript":"你好"}`,
			wantErr:   "audio_key",
		},
		{
			name:      "listening_dictation with no accepted answers",
			exerciseT: "listening_dictation",
			prompt:    `{"instruction":"Type what you hear","audio_key":"a.mp3"}`,
			answerKey: `{"accepted":[],"transcript":"你好"}`,
			wantErr:   "accepted",
		},
		{
			name:      "speak_repeat with no reference audio",
			exerciseT: "speak_repeat",
			prompt:    `{"instruction":"Repeat","text":"你好"}`,
			answerKey: `{"reference_text":"你好"}`,
			wantErr:   "audio_key",
		},
		{
			name:      "speak_repeat with no text",
			exerciseT: "speak_repeat",
			prompt:    `{"instruction":"Repeat","audio_key":"a.mp3"}`,
			answerKey: `{"reference_text":"你好"}`,
			wantErr:   "text",
		},
		{
			name:      "speak_repeat with an impossible time window",
			exerciseT: "speak_repeat",
			prompt:    `{"instruction":"Repeat","audio_key":"a.mp3","text":"你好","min_seconds":30,"max_seconds":10}`,
			answerKey: `{"reference_text":"你好"}`,
			wantErr:   "max_seconds",
		},
		{
			name:      "read_aloud with no text",
			exerciseT: "read_aloud",
			prompt:    `{"instruction":"Read this aloud"}`,
			answerKey: `{"reference_text":"你好"}`,
			wantErr:   "text",
		},
		{
			name:      "speak_answer with nothing to compare against",
			exerciseT: "speak_answer",
			prompt:    `{"instruction":"Answer","question":"What is your name?"}`,
			answerKey: `{}`,
			wantErr:   "key_points",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.exerciseT, json.RawMessage(tc.prompt), json.RawMessage(tc.answerKey))
			if err == nil {
				t.Fatalf("Validate accepted broken content: %s", tc.prompt)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q should mention %q", err, tc.wantErr)
			}
		})
	}
}

func TestListeningDictationGrading(t *testing.T) {
	prompt := `{"instruction":"Type what you hear","audio_key":"a.mp3"}`
	key := `{"accepted":["我叫小明","我叫小明。"],"transcript":"我叫小明。","pinyin":"wǒ jiào Xiǎomíng"}`

	cases := []struct {
		name      string
		response  string
		correct   bool
		wantScore float64
	}{
		{"exact", `{"text":"我叫小明。"}`, true, 1},
		{"without the full stop", `{"text":"我叫小明"}`, true, 1},
		{"with a stray space", `{"text":" 我叫小明 "}`, true, 1},
		{"wrong sentence", `{"text":"你叫什么名字"}`, false, 0},
		{"empty", `{}`, false, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Grade("listening_dictation", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(tc.response))
			if err != nil {
				t.Fatalf("Grade: %v", err)
			}
			if res.IsCorrect == nil || *res.IsCorrect != tc.correct {
				t.Errorf("is_correct = %v, want %v", res.IsCorrect, tc.correct)
			}
			if res.Score != tc.wantScore {
				t.Errorf("score = %v, want %v", res.Score, tc.wantScore)
			}
			// The transcript is the feedback for a dictation item.
			if res.Feedback["transcript"] != "我叫小明。" {
				t.Errorf("the transcript must be returned, got %v", res.Feedback["transcript"])
			}
		})
	}
}

func TestSpeakingNeverAssertsCorrectness(t *testing.T) {
	// The honesty invariant. Every speaking type, given a textbook-perfect
	// submission, must still refuse to claim the learner said anything correctly:
	// there is no speech recognition in this build.
	good := `{"duration_ms":2600,"self_rating":4}`

	prompts := map[string]string{
		"speak_repeat": `{"instruction":"Repeat","audio_key":"a.mp3","text":"我叫小明","max_seconds":10}`,
		"read_aloud":   `{"instruction":"Read aloud","text":"我叫小明","max_seconds":10}`,
		"speak_answer": `{"instruction":"Answer","question":"What is your name?","key_points":["我叫…"],"max_seconds":15}`,
	}
	keys := map[string]string{
		"speak_repeat": `{"reference_text":"我叫小明"}`,
		"read_aloud":   `{"reference_text":"我叫小明"}`,
		"speak_answer": `{"reference_text":"我叫小明"}`,
	}

	for _, typ := range SpeakingTypes() {
		t.Run(typ, func(t *testing.T) {
			res, err := Grade(typ, json.RawMessage(prompts[typ]), json.RawMessage(keys[typ]), json.RawMessage(good))
			if err != nil {
				t.Fatalf("Grade: %v", err)
			}
			if res.IsCorrect != nil {
				t.Fatalf("%s returned is_correct=%v; a speaking result must never assert correctness", typ, *res.IsCorrect)
			}
			if res.Feedback["needs_review"] != true {
				t.Errorf("%s must flag the result for review", typ)
			}
			if res.Feedback["limitation"] == "" || res.Feedback["limitation"] == nil {
				t.Errorf("%s must state what it could not assess", typ)
			}
			if res.Feedback["has_speech_recognition"] != false {
				t.Errorf("%s must report that no speech recognition was used", typ)
			}
			if res.Score <= 0 {
				t.Errorf("%s scored a complete, in-window recording as %v; the drill should at least register", typ, res.Score)
			}
		})
	}

	// And the registry agrees with the graders, so no caller can treat a speaking
	// score as a correctness signal.
	for _, typ := range SpeakingTypes() {
		if IsMechanicallyGradable(typ) {
			t.Errorf("%s must not be reported as mechanically gradable", typ)
		}
	}
	if !IsMechanicallyGradable("mcq") || !IsMechanicallyGradable("listening_dictation") {
		t.Error("mcq and listening_dictation are mechanically gradable")
	}
}

func TestSpeakingScoresOnlyWhatItCanCheck(t *testing.T) {
	prompt := `{"instruction":"Repeat","audio_key":"a.mp3","text":"我叫小明","min_seconds":1,"max_seconds":10}`
	key := `{"reference_text":"我叫小明"}`

	t.Run("no recording", func(t *testing.T) {
		res, err := Grade("speak_repeat", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(`{"duration_ms":0,"self_rating":5}`))
		if err != nil {
			t.Fatalf("Grade: %v", err)
		}
		if res.Score != 0.2 {
			t.Errorf("score = %v; only the self-rating was present, so only its weight should count", res.Score)
		}
		if res.IsCorrect != nil {
			t.Error("a missing recording must not be reported as an incorrect answer")
		}
		if res.Feedback["message"] == nil {
			t.Error("the learner should be told there was nothing to review")
		}
	})

	t.Run("recording outside the window", func(t *testing.T) {
		res, err := Grade("speak_repeat", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(`{"duration_ms":45000,"self_rating":3}`))
		if err != nil {
			t.Fatalf("Grade: %v", err)
		}
		// Recorded (0.5) plus rating (0.2), but outside the 1-10s window (no 0.3).
		if res.Score != 0.7 {
			t.Errorf("score = %v, want 0.7 (recorded + rating, no window credit)", res.Score)
		}
	})

	t.Run("a complete, in-window take", func(t *testing.T) {
		res, err := Grade("speak_repeat", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(`{"duration_ms":2600,"self_rating":5}`))
		if err != nil {
			t.Fatalf("Grade: %v", err)
		}
		if res.Score != 1 {
			t.Errorf("score = %v, want 1", res.Score)
		}
		if res.Grader != "speak_repeat" {
			t.Errorf("grader = %q", res.Grader)
		}
	})

	t.Run("the reference text comes back for self-comparison", func(t *testing.T) {
		res, err := Grade("speak_repeat", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(`{"duration_ms":2600,"self_rating":3}`))
		if err != nil {
			t.Fatalf("Grade: %v", err)
		}
		if res.Feedback["reference_text"] != "我叫小明" {
			t.Errorf("the learner needs the reference text to compare against, got %v", res.Feedback["reference_text"])
		}
	})
}

func TestSpeakingDefaultWindowDoesNotCreditAnEmptyRecording(t *testing.T) {
	// Regression: the authored prompt carries max_seconds but no min_seconds, so the
	// stored prompt has min_seconds = 0 and "duration >= min*1000" accepted a 0 ms
	// submission as inside the window. The live API showed the symptom — a speaking
	// attempt with no recording scored 0.5 (window + rating) instead of 0.2 — while
	// the unit test missed it because it passed min_seconds explicitly.
	prompt := `{"instruction":"Repeat","audio_key":"a.mp3","text":"你好","max_seconds":15}`
	key := `{"reference_text":"你好"}`

	res, err := Grade("speak_repeat", json.RawMessage(prompt), json.RawMessage(key),
		json.RawMessage(`{"duration_ms":0,"self_rating":5}`))
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if res.Score != 0.2 {
		t.Errorf("a 0 ms submission scored %v with the default window; only the self-rating (0.2) may count", res.Score)
	}
	if res.Feedback["recorded"] != false {
		t.Error("a 0 ms submission must not be reported as recorded")
	}
	window, ok := res.Feedback["expected_seconds"].([]int)
	if !ok || len(window) != 2 || window[0] != 1 || window[1] != 15 {
		t.Errorf("the effective window reported to the learner is %v, want [1 15]", res.Feedback["expected_seconds"])
	}

	// A real take of the same prompt must still earn the full credit, so the fix
	// cannot pass by making everything score less.
	real, err := Grade("speak_repeat", json.RawMessage(prompt), json.RawMessage(key),
		json.RawMessage(`{"duration_ms":2600,"self_rating":5}`))
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if real.Score != 1 {
		t.Errorf("a 2.6 s take in a 1-15 s window scored %v, want 1", real.Score)
	}

	// And an over-long recording loses the window credit but is still recorded.
	long, err := Grade("speak_repeat", json.RawMessage(prompt), json.RawMessage(key),
		json.RawMessage(`{"duration_ms":45000,"self_rating":3}`))
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	if long.Score != 0.7 {
		t.Errorf("a 45 s take in a 1-15 s window scored %v, want 0.7 (recorded + rating)", long.Score)
	}
}

func TestSpeakAnswerReturnsKeyPoints(t *testing.T) {
	prompt := `{"instruction":"Answer in two sentences","question":"What did you do today?","key_points":["今天","早上","晚上"],"max_seconds":30}`
	key := `{"reference_text":"今天我早上八点起床。"}`
	res, err := Grade("speak_answer", json.RawMessage(prompt), json.RawMessage(key), json.RawMessage(`{"duration_ms":8000,"self_rating":2}`))
	if err != nil {
		t.Fatalf("Grade: %v", err)
	}
	points, ok := res.Feedback["key_points"].([]string)
	if !ok || len(points) != 3 {
		t.Fatalf("expected 3 key points for self-checking, got %v", res.Feedback["key_points"])
	}
	if res.IsCorrect != nil {
		t.Fatal("speak_answer must not assert correctness")
	}
}

// --- pronunciation scoring ----------------------------------------------------------
//
// These pin the contract of a *scored* speaking drill. The numbers themselves come
// from a provider (internal/speech); what matters here is what the grader does with
// them, and what it refuses to claim.

const scoredPrompt = `{"instruction":"Say it","audio_key":"k","text":"你好","max_seconds":15}`
const scoredAnswerKey = `{"reference_text":"你好"}`

func gradeScored(t *testing.T, response string) Result {
	t.Helper()
	res, err := Grade("speak_repeat", json.RawMessage(scoredPrompt), json.RawMessage(scoredAnswerKey), json.RawMessage(response))
	if err != nil {
		t.Fatalf("grading: %v", err)
	}
	return res
}

func TestScoredSpeakingReportsWhatWasMeasured(t *testing.T) {
	res := gradeScored(t, `{"duration_ms":2000,"self_rating":4,"recording_id":"11111111-1111-1111-1111-111111111111",
		"score":0.82,"provider":"test","accuracy":0.84,"fluency":0.79,"completeness":0.9,"prosody":0.7,
		"words":[{"word":"你","accuracy":0.91,"error_type":"None"},{"word":"好","accuracy":0.41,"error_type":"Mispronunciation"}]}`)

	if res.Score != 0.82 {
		t.Errorf("score %v, want the measured 0.82", res.Score)
	}
	if res.IsCorrect != nil {
		t.Errorf("is_correct must stay null: a pronunciation score is not a verdict on correctness")
	}
	if got := res.Feedback["needs_review"]; got != false {
		t.Errorf("needs_review is %v; a machine-judged drill must stop asking for a human", got)
	}
	if got := res.Feedback["has_speech_recognition"]; got != true {
		t.Errorf("has_speech_recognition is %v, want true once a service actually listened", got)
	}
	if got := res.Feedback["passed"]; got != true {
		t.Errorf("passed is %v for a score above the band", got)
	}
	if got := res.Feedback["provider"]; got != "test" {
		t.Errorf("provider is %v", got)
	}
	weak, ok := res.Feedback["weak_words"].([]map[string]any)
	if !ok || len(weak) != 1 || weak[0]["word"] != "好" {
		t.Fatalf("weak_words should name exactly the word below the band, got %#v", res.Feedback["weak_words"])
	}
	if limitation, _ := res.Feedback["limitation"].(string); !strings.Contains(limitation, "not an examiner") {
		t.Errorf("the limitation must state what the score is not: %q", limitation)
	}
	if _, ok := res.Feedback["expected_seconds"]; ok {
		t.Error("a scored result should not carry the duration-window heuristic as if it were the scoring")
	}
}

func TestScoredSpeakingBelowTheBandDoesNotCount(t *testing.T) {
	res := gradeScored(t, `{"duration_ms":2000,"score":0.4,"provider":"test","words":[{"word":"你","accuracy":0.4,"error_type":"Mispronunciation"}]}`)
	if got := res.Feedback["passed"]; got != false {
		t.Errorf("passed is %v for a score below the band", got)
	}
	if res.Score != 0.4 {
		t.Errorf("score %v, want the measured 0.4 — the number is reported honestly", res.Score)
	}
	// Below the band is a measurement too, so it is still not queued for a human.
	if got := res.Feedback["needs_review"]; got != false {
		t.Errorf("needs_review is %v; a low score is not an open question", got)
	}
	if msg, _ := res.Feedback["message"].(string); !strings.Contains(msg, "below the") {
		t.Errorf("a below-band message should say so, got %q", msg)
	}
}

func TestUnscoredSpeakingStillAsksForReview(t *testing.T) {
	// The tier-1 path must survive: no provider, no score, and the honest "a human
	// should look at this" the app has always given.
	res := gradeScored(t, `{"duration_ms":3000,"self_rating":4}`)
	if got := res.Feedback["needs_review"]; got != true {
		t.Errorf("needs_review is %v for an unscored attempt, want true", got)
	}
	if got := res.Feedback["has_speech_recognition"]; got != false {
		t.Errorf("has_speech_recognition is %v with no score", got)
	}
	if res.IsCorrect != nil {
		t.Errorf("is_correct must stay null")
	}
}

func TestSpeakingScoreIsClampedIntoRange(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want float64
	}{
		{`{"duration_ms":1000,"score":1.4}`, 1},
		{`{"duration_ms":1000,"score":-0.5}`, 0},
		{`{"duration_ms":1000,"score":0.65}`, 0.65},
	} {
		res := gradeScored(t, tc.in)
		if res.Score != tc.want {
			t.Errorf("score %s graded %v, want %v", tc.in, res.Score, tc.want)
		}
	}
}
