package exercises

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// The graders decide what counts as a correct Chinese answer, so these cases are
// where the product's honesty lives: partial credit must be earned, unknown input
// must be wrong rather than fatal, and free-form writing must never be asserted
// correct.

func TestGradersHappyAndFailurePaths(t *testing.T) {
	cases := []struct {
		name       string
		exerciseT  string
		prompt     string
		answerKey  string
		response   string
		wantScore  float64
		wantGraded bool // whether IsCorrect must be non-nil
		wantOK     bool
	}{
		{
			name:      "mcq correct",
			exerciseT: "mcq",
			prompt:    `{"question":"Which means you?","options":[{"id":"a","text":"你"},{"id":"b","text":"我"}]}`,
			answerKey: `{"correct_id":"a"}`,
			response:  `{"option_id":"a"}`,
			wantScore: 1, wantGraded: true, wantOK: true,
		},
		{
			name:      "mcq wrong",
			exerciseT: "mcq",
			prompt:    `{"question":"q","options":[{"id":"a","text":"你"},{"id":"b","text":"我"}]}`,
			answerKey: `{"correct_id":"a"}`,
			response:  `{"option_id":"b"}`,
			wantScore: 0, wantGraded: true, wantOK: false,
		},
		{
			// A stale client can send an option id that no longer exists. That is a
			// wrong answer, not a 500.
			name:      "mcq with an option that is not an option",
			exerciseT: "mcq",
			prompt:    `{"question":"q","options":[{"id":"a","text":"你"},{"id":"b","text":"我"}]}`,
			answerKey: `{"correct_id":"a"}`,
			response:  `{"option_id":"zzz"}`,
			wantScore: 0, wantGraded: true, wantOK: false,
		},
		{
			name:      "mcq with no answer",
			exerciseT: "mcq",
			prompt:    `{"question":"q","options":[{"id":"a","text":"你"},{"id":"b","text":"我"}]}`,
			answerKey: `{"correct_id":"a"}`,
			response:  `{}`,
			wantScore: 0, wantGraded: true, wantOK: false,
		},
		{
			name:      "mcq_multi all correct",
			exerciseT: "mcq_multi",
			prompt:    `{"question":"q","options":[{"id":"a","text":"老师"},{"id":"b","text":"学生"},{"id":"c","text":"学校"}]}`,
			answerKey: `{"correct_ids":["a","b"]}`,
			response:  `{"option_ids":["a","b"]}`,
			wantScore: 1, wantGraded: true, wantOK: true,
		},
		{
			// Selecting everything must not be a winning strategy.
			name:      "mcq_multi selecting everything is not correct",
			exerciseT: "mcq_multi",
			prompt:    `{"question":"q","options":[{"id":"a","text":"老师"},{"id":"b","text":"学生"},{"id":"c","text":"学校"}]}`,
			answerKey: `{"correct_ids":["a","b"]}`,
			response:  `{"option_ids":["a","b","c"]}`,
			wantScore: 0.5, wantGraded: true, wantOK: false,
		},
		{
			name:      "true_false correct",
			exerciseT: "true_false",
			prompt:    `{"statement":"你好 is a greeting"}`,
			answerKey: `{"correct":true}`,
			response:  `{"value":true}`,
			wantScore: 1, wantGraded: true, wantOK: true,
		},
		{
			name:      "fill_blank accepted alternative",
			exerciseT: "fill_blank",
			prompt:    `{"sentence":"你___！"}`,
			answerKey: `{"accepted":["好"]}`,
			response:  `{"text":"好"}`,
			wantScore: 1, wantGraded: true, wantOK: true,
		},
		{
			// Whitespace and full-width punctuation must not decide correctness.
			name:      "fill_blank normalises whitespace",
			exerciseT: "fill_blank",
			prompt:    `{"sentence":"你___！"}`,
			answerKey: `{"accepted":["好"]}`,
			response:  `{"text":" 好 "}`,
			wantScore: 1, wantGraded: true, wantOK: true,
		},
		{
			name:      "sentence_order correct",
			exerciseT: "sentence_order",
			prompt:    `{"instruction":"order","tokens":["叫","我","小明"]}`,
			answerKey: `{"order":[1,0,2]}`,
			response:  `{"order":[1,0,2]}`,
			wantScore: 1, wantGraded: true, wantOK: true,
		},
		{
			// Reordering is incremental: a correct prefix earns partial credit.
			name:      "sentence_order partial credit for a correct prefix",
			exerciseT: "sentence_order",
			prompt:    `{"instruction":"order","tokens":["叫","我","小明"]}`,
			answerKey: `{"order":[1,0,2]}`,
			response:  `{"order":[1,2,0]}`,
			wantScore: 1.0 / 3.0, wantGraded: true, wantOK: false,
		},
		{
			name:      "translation accepted with different punctuation",
			exerciseT: "translation",
			prompt:    `{"instruction":"translate","source":"Are you a student?","direction":"en->zh"}`,
			answerKey: `{"accepted":["你是学生吗","你是学生吗？"]}`,
			response:  `{"text":"你是学生吗。"}`,
			wantScore: 1, wantGraded: true, wantOK: true,
		},
		{
			name:      "translation near miss is wrong but flagged for review",
			exerciseT: "translation",
			prompt:    `{"instruction":"translate","source":"Tomorrow I go to school.","direction":"en->zh"}`,
			answerKey: `{"accepted":["明天我去学校"],"reference":"明天我去学校。"}`,
			response:  `{"text":"明天我学校去"}`,
			wantScore: 0, wantGraded: true, wantOK: false,
		},
		{
			name:      "hanzi_trace clean run",
			exerciseT: "hanzi_trace",
			prompt:    `{"character":"你","mode":"guided","stroke_count":7}`,
			answerKey: `{"character":"你","stroke_count":7}`,
			response:  `{"strokes":[{"index":0},{"index":1},{"index":2},{"index":3},{"index":4},{"index":5},{"index":6}],"completed":true,"hints_used":0}`,
			wantScore: 1, wantGraded: true, wantOK: true,
		},
		{
			name:      "hanzi_trace with one wrong stroke still passes",
			exerciseT: "hanzi_trace",
			prompt:    `{"character":"你","mode":"guided","stroke_count":7}`,
			answerKey: `{"character":"你","stroke_count":7}`,
			response:  `{"strokes":[{"index":0,"mistaken":true},{"index":1},{"index":2},{"index":3},{"index":4},{"index":5},{"index":6}],"completed":true,"hints_used":0}`,
			wantScore: 6.0 / 7.0, wantGraded: true, wantOK: false,
		},
		{
			name:      "hanzi_trace abandoned",
			exerciseT: "hanzi_trace",
			prompt:    `{"character":"你","mode":"guided","stroke_count":7}`,
			answerKey: `{"character":"你","stroke_count":7}`,
			response:  `{"strokes":[{"index":0}],"completed":false,"hints_used":0}`,
			wantScore: 0, wantGraded: true, wantOK: false,
		},
		{
			// Recall is stricter: the same strokes with a hint shown are halved.
			name:      "hanzi_recall halves the credit when a hint was used",
			exerciseT: "hanzi_recall",
			prompt:    `{"character":"人","mode":"recall","stroke_count":2}`,
			answerKey: `{"character":"人","stroke_count":2}`,
			response:  `{"strokes":[{"index":0},{"index":1}],"completed":true,"hints_used":1}`,
			wantScore: 0.5, wantGraded: true, wantOK: false,
		},
		{
			name:      "hanzi_trace with no strokes",
			exerciseT: "hanzi_trace",
			prompt:    `{"character":"人","mode":"guided","stroke_count":2}`,
			answerKey: `{"character":"人","stroke_count":2}`,
			response:  `{"strokes":[],"completed":false,"hints_used":0}`,
			wantScore: 0, wantGraded: true, wantOK: false,
		},
		{
			name:      "short_answer full rubric coverage",
			exerciseT: "short_answer",
			prompt:    `{"question":"Write about your day"}`,
			answerKey: `{"must_include":["今天"],"min_chars":12,"reference":"今天我早上八点起床。"}`,
			response:  `{"text":"今天我早上八点起床。我中午吃饭。"}`,
			wantScore: 1, wantGraded: false, // never asserted correct mechanically
		},
		{
			name:      "short_answer missing the required word",
			exerciseT: "short_answer",
			prompt:    `{"question":"Write about your day"}`,
			answerKey: `{"must_include":["今天"],"min_chars":12,"reference":"今天我早上八点起床。"}`,
			response:  `{"text":"我早上八点起床，然后去学校学习汉语。"}`,
			wantScore: 0.4, wantGraded: false,
		},
		{
			name:      "short_answer too short",
			exerciseT: "short_answer",
			prompt:    `{"question":"Write about your day"}`,
			answerKey: `{"must_include":["今天"],"min_chars":12,"reference":"今天我早上八点起床。"}`,
			response:  `{"text":"今天"}`,
			wantScore: 0.6, wantGraded: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			res, err := Grade(tc.exerciseT, json.RawMessage(tc.prompt), json.RawMessage(tc.answerKey), json.RawMessage(tc.response))
			if err != nil {
				t.Fatalf("Grade returned an error for a well-formed response: %v", err)
			}
			if !aboutEqual(res.Score, tc.wantScore) {
				t.Errorf("score = %.4f, want %.4f", res.Score, tc.wantScore)
			}
			if tc.wantGraded {
				if res.IsCorrect == nil {
					t.Fatalf("IsCorrect is nil for a mechanically gradable type")
				}
				if *res.IsCorrect != tc.wantOK {
					t.Errorf("IsCorrect = %v, want %v", *res.IsCorrect, tc.wantOK)
				}
			} else if res.IsCorrect != nil {
				t.Errorf("IsCorrect = %v for a free-form answer; it must stay ungraded", *res.IsCorrect)
			}
			if res.Grader == "" {
				t.Error("grader name is empty")
			}
		})
	}
}

// Authored content is validated at import time. These are the authoring mistakes
// that would otherwise reach a learner as an ungradeable exercise.
func TestValidateRejectsBadContent(t *testing.T) {
	cases := []struct {
		name      string
		exerciseT string
		prompt    string
		answerKey string
	}{
		{"unknown type", "nope", `{}`, `{}`},
		{"mcq with one option", "mcq", `{"question":"q","options":[{"id":"a","text":"你"}]}`, `{"correct_id":"a"}`},
		{"mcq whose key is not an option", "mcq", `{"question":"q","options":[{"id":"a","text":"你"},{"id":"b","text":"我"}]}`, `{"correct_id":"c"}`},
		{"mcq with duplicate option ids", "mcq", `{"question":"q","options":[{"id":"a","text":"你"},{"id":"a","text":"我"}]}`, `{"correct_id":"a"}`},
		{"mcq unknown prompt field", "mcq", `{"question":"q","optionz":[]}`, `{"correct_id":"a"}`},
		{"fill_blank without a blank", "fill_blank", `{"sentence":"你好"}`, `{"accepted":["好"]}`},
		{"fill_blank with no accepted answer", "fill_blank", `{"sentence":"你___"}`, `{"accepted":[]}`},
		{"sentence_order with a short order", "sentence_order", `{"tokens":["a","b","c"]}`, `{"order":[0,1]}`},
		{"sentence_order with a repeated index", "sentence_order", `{"tokens":["a","b"]}`, `{"order":[0,0]}`},
		{"translation with an unknown direction", "translation", `{"source":"x","direction":"zh->fr"}`, `{"accepted":["x"]}`},
		{"hanzi_trace with a non-hanzi", "hanzi_trace", `{"character":"ab","mode":"guided","stroke_count":2}`, `{"character":"ab","stroke_count":2}`},
		{"hanzi_trace whose stroke counts disagree", "hanzi_trace", `{"character":"人","mode":"guided","stroke_count":2}`, `{"character":"人","stroke_count":3}`},
		{"short_answer with no rubric at all", "short_answer", `{"question":"q"}`, `{"must_include":[],"min_chars":0}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := Validate(tc.exerciseT, json.RawMessage(tc.prompt), json.RawMessage(tc.answerKey)); err == nil {
				t.Fatalf("Validate accepted invalid content: prompt=%s key=%s", tc.prompt, tc.answerKey)
			}
		})
	}
}

func TestValidateAcceptsEveryRegisteredType(t *testing.T) {
	// Every registered type must have at least one authored example, otherwise the
	// registry and the content drift apart silently.
	if got := len(Types()); got == 0 {
		t.Fatal("no exercise types are registered")
	}
	for _, typ := range Types() {
		if _, err := Lookup(typ); err != nil {
			t.Errorf("type %q is listed but cannot be looked up: %v", typ, err)
		}
	}
}

func TestNormalizeText(t *testing.T) {
	// Whitespace is deliberately insignificant: "ni hao" and "nihao" are the same
	// answer to a learner, and Chinese answers are typed with stray spaces often
	// enough that treating them as wrong would be hostile.
	cases := map[string]string{
		"  你好  ":  "你好",
		"你是学生吗？":  "你是学生吗",
		"Ni Hao":  "nihao",
		"Nǐ hǎo！": "nǐhǎo",
		"你好。":     "你好",
	}
	for in, want := range cases {
		if got := normalizeText(in); got != want {
			t.Errorf("normalizeText(%q) = %q, want %q", in, got, want)
		}
	}
}

func aboutEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 0.0001
}

func TestHanziAcceptsThePayloadTheClientActuallySends(t *testing.T) {
	// Regression. web/src/lib/answers.ts `hanziPayload` sends character and
	// stroke_count with every stroke report; the grader's response struct did not
	// accept them, so EVERY writing submission from the real client failed with
	// `unknown field "character"`. Because a grading error was mapped to 500 it
	// presented as a server outage on POST /lessons/{id}/attempts, and the cases
	// above never caught it because every one of them omits those two fields.
	prompt := json.RawMessage(`{"character":"你","mode":"guided","stroke_count":7}`)
	key := json.RawMessage(`{"character":"你","stroke_count":7}`)
	clientPayload := `{"character":"你","stroke_count":7,` +
		`"strokes":[{"index":0},{"index":1},{"index":2},{"index":3},{"index":4},{"index":5},{"index":6}],` +
		`"completed":true,"hints_used":0}`

	res, err := Grade("hanzi_trace", prompt, key, json.RawMessage(clientPayload))
	if err != nil {
		t.Fatalf("the payload the SPA sends must be accepted: %v", err)
	}
	if res.IsCorrect == nil || !*res.IsCorrect || res.Score != 1 {
		t.Fatalf("a clean trace should be graded correct, got %+v", res)
	}

	t.Run("a character other than the exercise character is refused, not graded", func(t *testing.T) {
		bad := strings.Replace(clientPayload, `"character":"你"`, `"character":"我"`, 1)
		_, err := Grade("hanzi_trace", prompt, key, json.RawMessage(bad))
		if err == nil {
			t.Fatal("grading a different character than the exercise asks for must be refused")
		}
		if !errors.Is(err, ErrBadResponse) {
			t.Errorf("this is a client error, so it must be tagged ErrBadResponse (API answers 422), got %v", err)
		}
		if !strings.Contains(err.Error(), "does not match") {
			t.Errorf("the error should say what did not match, got %v", err)
		}
	})

	t.Run("a stroke count that contradicts the exercise is refused", func(t *testing.T) {
		bad := strings.Replace(clientPayload, `"stroke_count":7`, `"stroke_count":9`, 1)
		if _, err := Grade("hanzi_trace", prompt, key, json.RawMessage(bad)); !errors.Is(err, ErrBadResponse) {
			t.Errorf("expected ErrBadResponse, got %v", err)
		}
	})

	t.Run("an unknown field is a client error, never a server fault", func(t *testing.T) {
		// This is the shape of the bug that broke writing: a payload the grader does
		// not understand must be answerable as 422, not as an outage.
		_, err := Grade("hanzi_trace", prompt, key,
			json.RawMessage(`{"character":"你","stroke_count":7,"strokes":[],"completed":true,"hints_used":0,"grade_me":true}`))
		if !errors.Is(err, ErrBadResponse) {
			t.Errorf("expected ErrBadResponse, got %v", err)
		}
	})
}

func TestEveryGradingTypeTagsABadPayloadAsAClientError(t *testing.T) {
	// Any type can receive a payload it cannot decode. None of them may report that
	// as a server fault, because the API turns ErrBadResponse into 422 and everything
	// else into 500.
	cases := []struct {
		exerciseType string
		prompt       string
		answerKey    string
	}{
		{"mcq", `{"question":"q","options":[{"id":"a","text":"x"},{"id":"b","text":"y"}]}`, `{"correct_id":"a"}`},
		{"true_false", `{"statement":"s","correct":true}`, `{"correct":true}`},
		{"fill_blank", `{"sentence":"我___学生"}`, `{"accepted":["是"]}`},
		{"sentence_order", `{"tokens":["我","是"]}`, `{"order":[0,1]}`},
		{"translation", `{"source":"hello","direction":"en->zh"}`, `{"accepted":["你好"]}`},
		{"hanzi_trace", `{"character":"你","mode":"guided","stroke_count":7}`, `{"character":"你","stroke_count":7}`},
		{"short_answer", `{"question":"q"}`, `{"must_include":["今天"],"min_chars":4,"reference":"今天我起床。"}`},
		{"listening_mcq", `{"instruction":"listen","audio_key":"a.mp3","options":[{"id":"a","text":"x"},{"id":"b","text":"y"}]}`, `{"correct_id":"a","transcript":"x"}`},
		{"listening_dictation", `{"instruction":"listen","audio_key":"a.mp3"}`, `{"accepted":["你好"],"transcript":"你好"}`},
		{"speak_repeat", `{"instruction":"repeat","audio_key":"a.mp3","text":"你好"}`, `{"reference_text":"你好"}`},
		{"speak_answer", `{"instruction":"answer","question":"你叫什么名字？","key_points":["我叫…"]}`, `{"reference_text":"我叫小明。"}`},
	}
	for _, tc := range cases {
		t.Run(tc.exerciseType, func(t *testing.T) {
			_, err := Grade(tc.exerciseType,
				json.RawMessage(tc.prompt), json.RawMessage(tc.answerKey),
				json.RawMessage(`{"totally":"wrong shape"}`))
			if err == nil {
				// mcq treats an unparseable-but-empty payload as "no answer"; a payload
				// with the wrong shape must not be treated that way.
				t.Fatalf("%s accepted a payload with a field it does not know", tc.exerciseType)
			}
			if !errors.Is(err, ErrBadResponse) {
				t.Errorf("%s returned an untagged error, which the API reports as 500: %v", tc.exerciseType, err)
			}
		})
	}
}
