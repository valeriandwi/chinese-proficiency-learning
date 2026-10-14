package exercises

import (
	"encoding/json"
	"fmt"
	"strings"
)

// --- multiple choice -------------------------------------------------------

type mcqOption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
	// AudioKey lets a listening MCQ carry per-option audio.
	AudioKey string `json:"audio_key,omitempty"`
}

type mcqPrompt struct {
	Question string      `json:"question"`
	Options  []mcqOption `json:"options"`
	AudioKey string      `json:"audio_key,omitempty"`
	// Pinyin and Hanzi render hints; empty means "use the learner's preference".
	ShowHanzi bool `json:"show_hanzi,omitempty"`
}

type mcqAnswerKey struct {
	CorrectID string `json:"correct_id"`
	Why       string `json:"why,omitempty"`
}

type mcqResponse struct {
	OptionID string `json:"option_id"`
}

type mcqGrader struct{}

func (mcqGrader) Type() string { return "mcq" }

func (mcqGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p mcqPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	var k mcqAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.question", p.Question); err != nil {
		return err
	}
	if len(p.Options) < 2 {
		return fmt.Errorf("exercises: mcq requires at least 2 options, got %d", len(p.Options))
	}
	seen := map[string]bool{}
	for i, o := range p.Options {
		if err := requireNonEmptyArg(fmt.Sprintf("prompt.options[%d].id", i), o.ID); err != nil {
			return err
		}
		if err := requireNonEmptyArg(fmt.Sprintf("prompt.options[%d].text", i), o.Text); err != nil {
			return err
		}
		if seen[o.ID] {
			return fmt.Errorf("exercises: mcq duplicate option id %q", o.ID)
		}
		seen[o.ID] = true
	}
	if !seen[k.CorrectID] {
		return fmt.Errorf("exercises: mcq correct_id %q is not one of the options", k.CorrectID)
	}
	return nil
}

func (g mcqGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p mcqPrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var k mcqAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r mcqResponse
	if err := decodeResponse(response, &r); err != nil {
		var empty struct{}
		if err2 := decode(response, &empty); err2 == nil {
			return Result{IsCorrect: boolPtr(false), Score: 0, Grader: g.Type(),
				Feedback: map[string]any{"message": "no answer submitted", "correct_id": k.CorrectID}}, nil
		}
		return Result{}, err
	}
	var chosen *mcqOption
	for i := range p.Options {
		if p.Options[i].ID == r.OptionID {
			chosen = &p.Options[i]
		}
	}
	if chosen == nil {
		// An option id that is not one of the options is a wrong answer, not a
		// server error: it happens whenever a client holds a stale copy of the
		// lesson, or a learner crafts the request. Returning 500 here would turn a
		// learner's mistake into an outage.
		return Result{IsCorrect: boolPtr(false), Score: 0, Grader: g.Type(),
			Feedback: map[string]any{
				"correct_id":     k.CorrectID,
				"why":            k.Why,
				"unknown_option": r.OptionID,
				"message":        "that choice is not one of this question's options",
			}}, nil
	}
	correct := r.OptionID == k.CorrectID
	fb := map[string]any{"correct_id": k.CorrectID}
	if k.Why != "" {
		fb["why"] = k.Why
	}
	score := 0.0
	if correct {
		score = 1
	}
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: g.Type(), Feedback: fb}, nil
}

// --- multiple choice, multiple answers -------------------------------------

type mcqMultiAnswerKey struct {
	CorrectIDs []string `json:"correct_ids"`
	Why        string   `json:"why,omitempty"`
}

type mcqMultiResponse struct {
	OptionIDs []string `json:"option_ids"`
}

type mcqMultiGrader struct{}

func (mcqMultiGrader) Type() string { return "mcq_multi" }

func (mcqMultiGrader) Validate(prompt, answerKey json.RawMessage) error {
	if err := (mcqGrader{}).Validate(prompt, json.RawMessage(`{"correct_id":`+mustJSONString(firstOptionID(prompt))+`}`)); err != nil {
		return err
	}
	var k mcqMultiAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if len(k.CorrectIDs) < 2 {
		return fmt.Errorf("exercises: mcq_multi requires at least 2 correct_ids")
	}
	var p mcqPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	known := map[string]bool{}
	for _, o := range p.Options {
		known[o.ID] = true
	}
	for _, id := range k.CorrectIDs {
		if !known[id] {
			return fmt.Errorf("exercises: mcq_multi correct_ids includes %q which is not an option", id)
		}
	}
	return nil
}

func (g mcqMultiGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var k mcqMultiAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r mcqMultiResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	want := map[string]bool{}
	for _, id := range k.CorrectIDs {
		want[id] = true
	}
	got := map[string]bool{}
	for _, id := range r.OptionIDs {
		got[id] = true
	}
	hits, falsePositives := 0, 0
	for id := range got {
		if want[id] {
			hits++
		} else {
			falsePositives++
		}
	}
	// Partial credit with a penalty for selecting wrong options: selecting
	// everything must not be a winning strategy.
	score := float64(hits-falsePositives) / float64(len(want))
	score = clamp01(score)
	correct := hits == len(want) && falsePositives == 0
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: g.Type(),
		Feedback: map[string]any{"correct_ids": k.CorrectIDs, "why": k.Why}}, nil
}

// --- true / false ----------------------------------------------------------

type trueFalseAnswerKey struct {
	Correct bool   `json:"correct"`
	Why     string `json:"why,omitempty"`
}

type trueFalseResponse struct {
	Value bool `json:"value"`
}

type trueFalseGrader struct{}

func (trueFalseGrader) Type() string { return "true_false" }

func (trueFalseGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p struct {
		Statement string `json:"statement"`
		AudioKey  string `json:"audio_key,omitempty"`
	}
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.statement", p.Statement); err != nil {
		return err
	}
	var k trueFalseAnswerKey
	return decode(answerKey, &k)
}

func (g trueFalseGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var k trueFalseAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r trueFalseResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	correct := r.Value == k.Correct
	score := 0.0
	if correct {
		score = 1
	}
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: g.Type(),
		Feedback: map[string]any{"correct": k.Correct, "why": k.Why}}, nil
}

// --- fill in the blank -----------------------------------------------------

type fillBlankPrompt struct {
	Sentence string `json:"sentence"` // contains ___ as the blank marker
	Hint     string `json:"hint,omitempty"`
	Pinyin   string `json:"pinyin,omitempty"`
	// WordBank, when present, is the set of draggable choices.
	WordBank []string `json:"word_bank,omitempty"`
}

type fillBlankAnswerKey struct {
	Accepted []string `json:"accepted"`
}

type fillBlankResponse struct {
	Text string `json:"text"`
}

type fillBlankGrader struct{}

func (fillBlankGrader) Type() string { return "fill_blank" }

func (fillBlankGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p fillBlankPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.sentence", p.Sentence); err != nil {
		return err
	}
	if !strings.Contains(p.Sentence, "___") {
		return fmt.Errorf("exercises: fill_blank sentence must contain the ___ blank marker")
	}
	var k fillBlankAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if len(k.Accepted) == 0 {
		return fmt.Errorf("exercises: fill_blank requires at least one accepted answer")
	}
	for i, a := range k.Accepted {
		if err := requireNonEmptyArg(fmt.Sprintf("answer_key.accepted[%d]", i), a); err != nil {
			return err
		}
	}
	return nil
}

func (g fillBlankGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p fillBlankPrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var k fillBlankAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r fillBlankResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	correct := r.Text != "" && matchesAny(r.Text, k.Accepted)
	score := 0.0
	if correct {
		score = 1
	}
	fb := map[string]any{"accepted": k.Accepted, "assembled": strings.ReplaceAll(p.Sentence, "___", strings.TrimSpace(r.Text))}
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: g.Type(), Feedback: fb}, nil
}

// --- sentence ordering -----------------------------------------------------

type sentenceOrderPrompt struct {
	Instruction string   `json:"instruction"`
	Tokens      []string `json:"tokens"` // presented to the learner in this order
	Pinyin      string   `json:"pinyin,omitempty"`
}

type sentenceOrderAnswerKey struct {
	// Order is the sequence of 0-based indices into prompt.tokens that forms the
	// correct sentence. Indices (not strings) so duplicate tokens stay unambiguous.
	Order      []int  `json:"order"`
	FullPinyin string `json:"full_pinyin,omitempty"`
}

type sentenceOrderResponse struct {
	Order []int `json:"order"`
}

type sentenceOrderGrader struct{}

func (sentenceOrderGrader) Type() string { return "sentence_order" }

func (sentenceOrderGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p sentenceOrderPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if len(p.Tokens) < 2 {
		return fmt.Errorf("exercises: sentence_order requires at least 2 tokens")
	}
	var k sentenceOrderAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if len(k.Order) != len(p.Tokens) {
		return fmt.Errorf("exercises: sentence_order order has %d entries but there are %d tokens", len(k.Order), len(p.Tokens))
	}
	seen := map[int]bool{}
	for _, idx := range k.Order {
		if idx < 0 || idx >= len(p.Tokens) {
			return fmt.Errorf("exercises: sentence_order index %d out of range", idx)
		}
		if seen[idx] {
			return fmt.Errorf("exercises: sentence_order index %d repeated", idx)
		}
		seen[idx] = true
	}
	return nil
}

func (g sentenceOrderGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p sentenceOrderPrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var k sentenceOrderAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r sentenceOrderResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	// Score by longest correct prefix relative to the full length: reordering is
	// incremental, so a learner who got the first three right deserves credit.
	prefix := 0
	for prefix < len(r.Order) && prefix < len(k.Order) && r.Order[prefix] == k.Order[prefix] {
		prefix++
	}
	score := float64(prefix) / float64(len(k.Order))
	correct := len(r.Order) == len(k.Order) && prefix == len(k.Order)
	if !correct {
		score = clamp01(score)
	}
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: g.Type(),
		Feedback: map[string]any{"correct_order": k.Order, "correct_prefix": prefix, "total": len(k.Order)}}, nil
}

// --- translation / dictation ----------------------------------------------

type translationPrompt struct {
	Instruction string `json:"instruction"`
	Source      string `json:"source"`
	Direction   string `json:"direction"` // "en->zh" | "zh->en"
	AudioKey    string `json:"audio_key,omitempty"`
	Hint        string `json:"hint,omitempty"`
}

type translationAnswerKey struct {
	Accepted  []string `json:"accepted"`
	Reference string   `json:"reference,omitempty"`
}

type translationResponse struct {
	Text string `json:"text"`
}

type translationGrader struct{}

func (translationGrader) Type() string { return "translation" }

func (translationGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p translationPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.source", p.Source); err != nil {
		return err
	}
	switch p.Direction {
	case "en->zh", "zh->en":
	default:
		return fmt.Errorf("exercises: translation direction must be en->zh or zh->en, got %q", p.Direction)
	}
	var k translationAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if len(k.Accepted) == 0 {
		return fmt.Errorf("exercises: translation requires at least one accepted answer")
	}
	return nil
}

func (g translationGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p translationPrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var k translationAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r translationResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	exact := r.Text != "" && matchesAny(r.Text, k.Accepted)
	// Near-match is a distinct, honestly-reported outcome: a translation that
	// differs only by punctuation or spacing is accepted; anything else is
	// surfaced as "compare with the reference" rather than silently marked wrong.
	score := 0.0
	if exact {
		score = 1
	}
	fb := map[string]any{"accepted": k.Accepted, "reference": k.Reference}
	if !exact && r.Text != "" {
		fb["message"] = "not an exact match for an accepted answer; compare with the reference translation"
		fb["needs_review"] = true
	}
	return Result{IsCorrect: boolPtr(exact), Score: score, Grader: g.Type(), Feedback: fb}, nil
}

// --- hanzi writing ---------------------------------------------------------

// hanziTracePrompt drives the client-side canvas. Stroke geometry itself lives in
// the hanzi-writer dataset (Make Me a Hanzi, Arphic Public License); the server
// stores only the outcome of each stroke, which the client reports and the
// server re-interprets under strict rules below.
type hanziTracePrompt struct {
	Character   string `json:"character"`
	Instruction string `json:"instruction,omitempty"`
	Mode        string `json:"mode"` // "guided" | "recall" | "sentence"
	StrokeCount int    `json:"stroke_count"`
}

type hanziStrokeReport struct {
	Index         int  `json:"index"`
	Mistaken      bool `json:"mistaken"`
	OrderMismatch bool `json:"order_mismatch,omitempty"`
	Direction     bool `json:"direction_error,omitempty"`
	OutOfBounds   bool `json:"out_of_bounds,omitempty"`
}

type hanziAnswerKey struct {
	Character   string `json:"character"`
	StrokeCount int    `json:"stroke_count"`
}

type hanziResponse struct {
	// Character and StrokeCount are the client's own claim about what it drew. They
	// are NOT used for grading — the answer key decides what is correct — but they
	// are part of the payload the SPA sends, and rejecting them made every writing
	// submission fail with a 500 ("unknown field"), which no test caught because
	// none of them ever submitted a stroke report.
	Character   string              `json:"character"`
	StrokeCount int                 `json:"stroke_count"`
	Strokes     []hanziStrokeReport `json:"strokes"`
	Completed   bool                `json:"completed"`
	HintsUsed   int                 `json:"hints_used"`
	DurationMs  int                 `json:"duration_ms,omitempty"`
}

type hanziTraceGrader struct{}

func (hanziTraceGrader) Type() string { return "hanzi_trace" }

func (hanziTraceGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p hanziTracePrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if _, err := singleHanzi(p.Character); err != nil {
		return fmt.Errorf("exercises: hanzi_trace prompt.character: %w", err)
	}
	if p.StrokeCount <= 0 {
		return fmt.Errorf("exercises: hanzi_trace prompt.stroke_count must be positive")
	}
	if p.Mode == "" {
		p.Mode = "guided"
	}
	switch p.Mode {
	case "guided", "recall", "sentence":
	default:
		return fmt.Errorf("exercises: hanzi_trace mode must be guided, recall or sentence, got %q", p.Mode)
	}
	var k hanziAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if k.Character != p.Character {
		return fmt.Errorf("exercises: hanzi_trace answer_key.character %q does not match prompt.character %q", k.Character, p.Character)
	}
	if k.StrokeCount != p.StrokeCount {
		return fmt.Errorf("exercises: hanzi_trace stroke_count mismatch between prompt (%d) and answer_key (%d)", p.StrokeCount, k.StrokeCount)
	}
	return nil
}

func (g hanziTraceGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var k hanziAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r hanziResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	return gradeHanzi(g.Type(), k, r, false)
}

type hanziRecallGrader struct{ hanziTraceGrader }

func (hanziRecallGrader) Type() string { return "hanzi_recall" }

func (g hanziRecallGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var k hanziAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r hanziResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	// Recall is stricter than tracing: writing from memory with a hint shown
	// halves the credit, because the point of the drill is unaided production.
	return gradeHanzi(g.Type(), k, r, true)
}

func gradeHanzi(grader string, k hanziAnswerKey, r hanziResponse, strict bool) (Result, error) {
	// If the client names a character, it must be the character this exercise asks
	// for. Grading strokes against a different character than the learner saw would
	// be meaningless, and a mismatch means a client bug or a tampered payload.
	if r.Character != "" && r.Character != k.Character {
		return Result{}, fmt.Errorf("%w: %s response.character %q does not match the exercise character %q",
			ErrBadResponse, grader, r.Character, k.Character)
	}
	if r.StrokeCount != 0 && r.StrokeCount != k.StrokeCount {
		return Result{}, fmt.Errorf("%w: %s response.stroke_count %d does not match the exercise stroke count %d",
			ErrBadResponse, grader, r.StrokeCount, k.StrokeCount)
	}
	if len(r.Strokes) == 0 {
		return Result{IsCorrect: boolPtr(false), Score: 0, Grader: grader,
			Feedback: map[string]any{"message": "no strokes recorded"}}, nil
	}
	clean, orderErrors, directionErrors, outOfBounds := 0, 0, 0, 0
	for _, s := range r.Strokes {
		if s.Index < 0 || s.Index >= k.StrokeCount {
			return Result{}, fmt.Errorf("exercises: hanzi stroke index %d out of range for a %d-stroke character", s.Index, k.StrokeCount)
		}
		if s.OrderMismatch {
			orderErrors++
		}
		if s.Direction {
			directionErrors++
		}
		if s.OutOfBounds {
			outOfBounds++
		}
		if !s.Mistaken && !s.OrderMismatch && !s.Direction && !s.OutOfBounds {
			clean++
		}
	}
	score := float64(clean) / float64(k.StrokeCount)
	if strict && r.HintsUsed > 0 {
		score *= 0.5
	}
	if !r.Completed {
		score = 0
	}
	score = clamp01(score)
	threshold := 0.9
	correct := r.Completed && score >= threshold
	fb := map[string]any{
		"character":        k.Character,
		"stroke_count":     k.StrokeCount,
		"strokes_reported": len(r.Strokes),
		"order_errors":     orderErrors,
		"direction_errors": directionErrors,
		"out_of_bounds":    outOfBounds,
		"hints_used":       r.HintsUsed,
		"threshold":        threshold,
	}
	if !correct {
		fb["needs_revision"] = true
	}
	if strict {
		fb["strict"] = true
	}
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: grader, Feedback: fb}, nil
}

// --- short answer (rubric) -------------------------------------------------

type shortAnswerPrompt struct {
	Question     string `json:"question"`
	PassageID    string `json:"passage_id,omitempty"`
	MinSentences int    `json:"min_sentences,omitempty"`
	MaxChars     int    `json:"max_chars,omitempty"`
}

type shortAnswerAnswerKey struct {
	MustInclude    []string `json:"must_include"`
	MustNotInclude []string `json:"must_not_include,omitempty"`
	MinChars       int      `json:"min_chars"`
	Reference      string   `json:"reference,omitempty"`
}

type shortAnswerResponse struct {
	Text string `json:"text"`
}

type shortAnswerGrader struct{}

func (shortAnswerGrader) Type() string { return "short_answer" }

func (shortAnswerGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p shortAnswerPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.question", p.Question); err != nil {
		return err
	}
	var k shortAnswerAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if len(k.MustInclude) == 0 && k.MinChars == 0 {
		return fmt.Errorf("exercises: short_answer needs at least one must_include token or a min_chars rubric")
	}
	if k.MinChars == 0 {
		k.MinChars = 5
	}
	if len([]rune(k.Reference)) > 0 && k.MinChars > len([]rune(k.Reference)) {
		return fmt.Errorf("exercises: short_answer min_chars (%d) exceeds the reference answer length (%d)", k.MinChars, len([]rune(k.Reference)))
	}
	return nil
}

func (g shortAnswerGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var k shortAnswerAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r shortAnswerResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	text := strings.TrimSpace(r.Text)
	chars := len([]rune(text))

	// A rubric check is a coverage check, not comprehension grading. The result
	// records needs_review unconditionally so no caller can present this as a
	// reliable grade of a free-form Chinese answer.
	covered, missing := 0, []string{}
	for _, tok := range k.MustInclude {
		if strings.Contains(text, tok) {
			covered++
		} else {
			missing = append(missing, tok)
		}
	}
	lengthOK := chars >= k.MinChars
	violations := []string{}
	for _, tok := range k.MustNotInclude {
		if strings.Contains(text, tok) {
			violations = append(violations, tok)
		}
	}

	score := 0.0
	if len(k.MustInclude) > 0 {
		score += 0.6 * float64(covered) / float64(len(k.MustInclude))
	} else if lengthOK {
		score += 0.6
	}
	if lengthOK {
		score += 0.4
	}
	if len(violations) > 0 {
		score *= 0.5
	}
	score = clamp01(score)

	// Never assert correctness mechanically for free-form writing.
	fb := map[string]any{
		"grader":       "rubric",
		"covered":      covered,
		"missing":      missing,
		"chars":        chars,
		"min_chars":    k.MinChars,
		"violations":   violations,
		"reference":    k.Reference,
		"needs_review": true,
		"limitation":   "keyword/coverage rubric only: this check cannot judge grammar, tone or argument quality",
	}
	return Result{IsCorrect: nil, Score: score, Grader: "rubric", Feedback: fb}, nil
}

// --- shared helpers --------------------------------------------------------

func clamp01(f float64) float64 {
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// singleHanzi asserts the value is exactly one non-space Han character.
func singleHanzi(s string) (rune, error) {
	runes := []rune(strings.TrimSpace(s))
	if len(runes) != 1 {
		return 0, fmt.Errorf("expected exactly one character, got %d", len(runes))
	}
	r := runes[0]
	if r < 0x3400 || (r > 0x9FFF && r < 0xF900) || r > 0xFAFF {
		return 0, fmt.Errorf("character %q is outside the CJK ideograph ranges", r)
	}
	return r, nil
}

func mustJSONString(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return `""`
	}
	return string(b)
}

func firstOptionID(prompt json.RawMessage) string {
	var p mcqPrompt
	if err := decode(prompt, &p); err != nil || len(p.Options) == 0 {
		return ""
	}
	return p.Options[0].ID
}
