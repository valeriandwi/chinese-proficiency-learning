package exercises

// Listening and speaking exercises.
//
// These two families are defined by an honesty rule, not by a feature:
//
//   * Listening is mechanically gradable, because the learner produces a choice or
//     a transcription that can be compared to an answer key. Its transcript lives
//     in the ANSWER KEY, so it is structurally absent from the lesson payload and
//     can only be revealed after a submission — the same guarantee every other
//     type gives.
//   * Speaking has two modes, and which one applies is a deployment decision, not a
//     compile-time fact. With a pronunciation provider configured the recording is
//     scored (see internal/speech): a result reports what was measured, per word, and
//     stops asking for human review. With no provider — still the default — a speaking
//     result reports only what can be checked without one (a recording exists, it is
//     long enough to be an utterance, the
//     learner rated their own attempt) and returns IsCorrect: nil with
//     needs_review: true. It never claims the learner pronounced anything
//     correctly. A score here means "the drill was done", not "it was right".

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// --- shared shapes ----------------------------------------------------------

const (
	defaultMaxSeconds  = 20
	minMaxSeconds      = 3
	maxMaxSeconds      = 180
	defaultReplayLimit = 3
)

// speakingLimitation is shown to the learner verbatim. It is deliberately blunt:
// the failure mode to avoid is a learner believing an automated pass means their
// tones are correct.
const speakingLimitation = "recorded and self-reviewed only: this build has no speech recognition, so pronunciation, tones and fluency were NOT assessed. A score here means the drill was completed, not that your Chinese was correct."

// speakingPassingBand is the score at or above which a spoken drill counts as done.
// It is the same 0.7 the progress recomputation applies to every exercise, so a clear
// recording is what lets a lesson complete — speaking needs no rule of its own.
const speakingPassingBand = 0.7

// pronunciationWord is one assessed word, in the shape the scoring endpoint returns.
type pronunciationWord struct {
	Word      string  `json:"word"`
	Accuracy  float64 `json:"accuracy"`
	ErrorType string  `json:"error_type"`
}

type speakingResponse struct {
	DurationMs int `json:"duration_ms"`
	// SelfRating is the learner's own 1-5 judgement. It is stored as a signal for
	// them and their future revision, never folded into correctness.
	SelfRating int `json:"self_rating,omitempty"`
	// RecordingID names a recording this learner had scored. It is the ONLY scoring
	// input a client controls: the attempts service looks the score up in storage,
	// where it was written by the provider.
	RecordingID string `json:"recording_id,omitempty"`

	// The fields below are filled in by the server. They are declared here so the
	// graders can read them, and a client that sends them is not trusted — the
	// attempts service strips them from every payload before grading and restores them
	// only from a stored score (see attempts.sanitizeSpeakingResponse). Their absence
	// means simply "not scored".
	Score        *float64            `json:"score,omitempty"`
	Provider     string              `json:"provider,omitempty"`
	Accuracy     float64             `json:"accuracy,omitempty"`
	Fluency      float64             `json:"fluency,omitempty"`
	Completeness float64             `json:"completeness,omitempty"`
	Prosody      float64             `json:"prosody,omitempty"`
	Words        []pronunciationWord `json:"words,omitempty"`
}

type listeningMCQAnswerKey struct {
	CorrectID  string `json:"correct_id"`
	Transcript string `json:"transcript"`
	Why        string `json:"why,omitempty"`
}

type listeningMCQPrompt struct {
	Instruction string      `json:"instruction"`
	AudioKey    string      `json:"audio_key"`
	Options     []mcqOption `json:"options"`
	ReplayLimit int         `json:"replay_limit,omitempty"`
}

func requireAudioKey(prompt string) error {
	if strings.TrimSpace(prompt) == "" {
		return fmt.Errorf("exercises: prompt.audio_key is required: a listening item with no audio is not a listening item")
	}
	return nil
}

func clampReplayLimit(n int) int {
	if n <= 0 {
		return defaultReplayLimit
	}
	if n > 10 {
		return 10
	}
	return n
}

// --- listening: multiple choice --------------------------------------------

type listeningMCQGrader struct{}

func (listeningMCQGrader) Type() string { return "listening_mcq" }

func (listeningMCQGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p listeningMCQPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.instruction", p.Instruction); err != nil {
		return err
	}
	if err := requireAudioKey(p.AudioKey); err != nil {
		return err
	}
	if len(p.Options) < 2 {
		return fmt.Errorf("exercises: listening_mcq requires at least 2 options, got %d", len(p.Options))
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
			return fmt.Errorf("exercises: listening_mcq duplicate option id %q", o.ID)
		}
		seen[o.ID] = true
	}
	var k listeningMCQAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if !seen[k.CorrectID] {
		return fmt.Errorf("exercises: listening_mcq correct_id %q is not one of the options", k.CorrectID)
	}
	// The transcript is what the learner sees after answering. Without it the item
	// would give no feedback at all, since the audio is not re-readable as text.
	if err := requireNonEmptyArg("answer_key.transcript", k.Transcript); err != nil {
		return err
	}
	return nil
}

func (g listeningMCQGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p listeningMCQPrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var k listeningMCQAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r mcqResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	known := false
	for _, o := range p.Options {
		if o.ID == r.OptionID {
			known = true
		}
	}
	if !known {
		// An option id that is not an option is a wrong answer, not a server error
		// (the client may be holding a stale copy of the lesson).
		return Result{IsCorrect: boolPtr(false), Score: 0, Grader: g.Type(),
			Feedback: map[string]any{
				"correct_id": k.CorrectID,
				"transcript": k.Transcript,
				"why":        k.Why,
				"message":    "that choice is not one of this question's options",
			}}, nil
	}
	correct := r.OptionID == k.CorrectID
	score := 0.0
	if correct {
		score = 1
	}
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: g.Type(),
		Feedback: map[string]any{"correct_id": k.CorrectID, "transcript": k.Transcript, "why": k.Why}}, nil
}

// --- listening: dictation ---------------------------------------------------

type listeningDictationPrompt struct {
	Instruction string `json:"instruction"`
	AudioKey    string `json:"audio_key"`
	Hint        string `json:"hint,omitempty"`
	ReplayLimit int    `json:"replay_limit,omitempty"`
}

type listeningDictationAnswerKey struct {
	Accepted   []string `json:"accepted"`
	Transcript string   `json:"transcript"`
	Pinyin     string   `json:"pinyin,omitempty"`
}

type listeningDictationGrader struct{}

func (listeningDictationGrader) Type() string { return "listening_dictation" }

func (listeningDictationGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p listeningDictationPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.instruction", p.Instruction); err != nil {
		return err
	}
	if err := requireAudioKey(p.AudioKey); err != nil {
		return err
	}
	var k listeningDictationAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if len(k.Accepted) == 0 {
		return fmt.Errorf("exercises: listening_dictation requires at least one accepted answer")
	}
	if err := requireNonEmptyArg("answer_key.transcript", k.Transcript); err != nil {
		return err
	}
	return nil
}

func (g listeningDictationGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var k listeningDictationAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r translationResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	// Whitespace and punctuation are insignificant, exactly as in the writing
	// translation grader: a learner who heard the sentence and typed it with a
	// stray space has done the exercise.
	correct := r.Text != "" && matchesAny(r.Text, k.Accepted)
	score := 0.0
	if correct {
		score = 1
	}
	fb := map[string]any{"accepted": k.Accepted, "transcript": k.Transcript, "pinyin": k.Pinyin, "your_text": r.Text}
	if !correct && r.Text != "" {
		fb["message"] = "not quite what was said — compare your transcription with the transcript"
		fb["needs_review"] = true
	}
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: g.Type(), Feedback: fb}, nil
}

// --- speaking: repeat, read aloud, answer ----------------------------------

type speakRepeatPrompt struct {
	Instruction string `json:"instruction"`
	AudioKey    string `json:"audio_key"`
	Text        string `json:"text"`
	Pinyin      string `json:"pinyin,omitempty"`
	// ToneNote names the tones to aim for, e.g. "third tone — falling then rising",
	// and states the spoken form when it differs from the written one. A speaking drill
	// about a word IS a tone drill for that word: this is the part of it the learner can
	// act on before they record, and it reaches the feedback so they know what to fix.
	ToneNote   string `json:"tone_note,omitempty"`
	MinSeconds int    `json:"min_seconds,omitempty"`
	MaxSeconds int    `json:"max_seconds,omitempty"`
}

type readAloudPrompt struct {
	Instruction string `json:"instruction"`
	Text        string `json:"text"`
	Pinyin      string `json:"pinyin,omitempty"`
	MinSeconds  int    `json:"min_seconds,omitempty"`
	MaxSeconds  int    `json:"max_seconds,omitempty"`
}

type speakAnswerPrompt struct {
	Instruction string   `json:"instruction"`
	Question    string   `json:"question"`
	AudioKey    string   `json:"audio_key,omitempty"`
	ImageKey    string   `json:"image_key,omitempty"`
	KeyPoints   []string `json:"key_points,omitempty"`
	PrepSeconds int      `json:"prep_seconds,omitempty"`
	MaxSeconds  int      `json:"max_seconds,omitempty"`
}

type speakingAnswerKey struct {
	ReferenceText string `json:"reference_text,omitempty"`
	Reference     string `json:"reference,omitempty"`
}

// speakingWindow returns the effective recording bounds, applying the same
// defaults Validate checks against.
//
// Validate normalises a COPY of the prompt for checking, and the stored prompt keeps
// exactly what the author wrote. Grade must therefore apply the defaults too: an
// authored prompt with no min_seconds reads as 0, and 0 ms would then satisfy
// "duration >= min", awarding window credit for a recording that does not exist.
func speakingWindow(minSeconds, maxSeconds int) (int, int) {
	if minSeconds <= 0 {
		minSeconds = 1
	}
	if maxSeconds <= 0 || maxSeconds > maxMaxSeconds {
		maxSeconds = defaultMaxSeconds
	}
	if minSeconds > maxSeconds {
		minSeconds = maxSeconds
	}
	return minSeconds, maxSeconds
}

func validateSpeakingWindow(minSeconds, maxSeconds *int) error {
	if *maxSeconds == 0 {
		*maxSeconds = defaultMaxSeconds
	}
	if *maxSeconds < minMaxSeconds || *maxSeconds > maxMaxSeconds {
		return fmt.Errorf("exercises: max_seconds must be between %d and %d", minMaxSeconds, maxMaxSeconds)
	}
	if *minSeconds == 0 {
		*minSeconds = 1
	}
	if *minSeconds < 1 {
		return fmt.Errorf("exercises: min_seconds must be at least 1")
	}
	if *minSeconds > *maxSeconds {
		return fmt.Errorf("exercises: min_seconds (%d) must not exceed max_seconds (%d)", *minSeconds, *maxSeconds)
	}
	return nil
}

// gradeSpeaking turns a recording into a result using ONLY checkable properties.
//
// It is the same contract as the free-form writing rubric: nil correctness, a
// score built from what can be measured, needs_review, and the limitation text.
func gradeSpeaking(grader string, r speakingResponse, minSeconds, maxSeconds int, feedback map[string]any) (Result, error) {
	if feedback == nil {
		feedback = map[string]any{}
	}
	minSeconds, maxSeconds = speakingWindow(minSeconds, maxSeconds)
	recorded := r.DurationMs > 0
	inBand := r.DurationMs >= minSeconds*1000 && r.DurationMs <= maxSeconds*1000
	rated := r.SelfRating >= 1 && r.SelfRating <= 5

	score := 0.0
	if recorded {
		score += 0.5
	}
	if inBand {
		score += 0.3
	}
	if rated {
		score += 0.2
	}

	// A scored recording replaces the proxies entirely: the numbers come from a
	// pronunciation service that actually listened, so the window and self-rating
	// heuristic is no longer what decides anything.
	if r.Score != nil {
		return gradeScoredSpeaking(grader, r, feedback), nil
	}

	feedback["duration_ms"] = r.DurationMs
	feedback["expected_seconds"] = []int{minSeconds, maxSeconds}
	feedback["self_rating"] = r.SelfRating
	feedback["recorded"] = recorded
	feedback["has_speech_recognition"] = false
	feedback["needs_review"] = true
	feedback["limitation"] = speakingLimitation
	if !recorded {
		feedback["message"] = "no recording was submitted, so there was nothing to review"
	} else if !inBand {
		feedback["message"] = fmt.Sprintf("the recording is %.1fs, outside the %d-%ds window for this drill",
			float64(r.DurationMs)/1000, minSeconds, maxSeconds)
	}

	return Result{IsCorrect: nil, Score: clamp01(score), Grader: grader, Feedback: feedback}, nil
}

// --- speak_repeat -----------------------------------------------------------

type speakRepeatGrader struct{}

func (speakRepeatGrader) Type() string { return "speak_repeat" }

func (speakRepeatGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p speakRepeatPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.instruction", p.Instruction); err != nil {
		return err
	}
	if err := requireAudioKey(p.AudioKey); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.text", p.Text); err != nil {
		return err
	}
	if err := validateSpeakingWindow(&p.MinSeconds, &p.MaxSeconds); err != nil {
		return err
	}
	var k speakingAnswerKey
	return decode(answerKey, &k)
}

func (g speakRepeatGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p speakRepeatPrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var k speakingAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r speakingResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	return gradeSpeaking(g.Type(), r, p.MinSeconds, p.MaxSeconds, map[string]any{
		"reference_text": p.Text,
		"pinyin":         p.Pinyin,
		"expected":       fallbackText(k.ReferenceText, p.Text),
		"tone_note":      p.ToneNote,
	})
}

// --- read_aloud -------------------------------------------------------------

type readAloudGrader struct{}

func (readAloudGrader) Type() string { return "read_aloud" }

func (readAloudGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p readAloudPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.instruction", p.Instruction); err != nil {
		return err
	}
	// No audio_key: the learner reads the text in front of them, which is what
	// distinguishes this from speak_repeat.
	if err := requireNonEmptyArg("prompt.text", p.Text); err != nil {
		return err
	}
	if err := validateSpeakingWindow(&p.MinSeconds, &p.MaxSeconds); err != nil {
		return err
	}
	var k speakingAnswerKey
	return decode(answerKey, &k)
}

func (g readAloudGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p readAloudPrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var r speakingResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	return gradeSpeaking(g.Type(), r, p.MinSeconds, p.MaxSeconds, map[string]any{
		"reference_text": p.Text,
		"pinyin":         p.Pinyin,
	})
}

// --- speak_answer -----------------------------------------------------------

type speakAnswerGrader struct{}

func (speakAnswerGrader) Type() string { return "speak_answer" }

func (speakAnswerGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p speakAnswerPrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.instruction", p.Instruction); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.question", p.Question); err != nil {
		return err
	}
	if err := validateSpeakingWindow(new(int), &p.MaxSeconds); err != nil {
		return err
	}
	if p.PrepSeconds < 0 || p.PrepSeconds > 120 {
		return fmt.Errorf("exercises: prep_seconds must be between 0 and 120")
	}
	var k speakingAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if len(p.KeyPoints) == 0 && k.ReferenceText == "" && k.Reference == "" {
		return fmt.Errorf("exercises: speak_answer needs key_points or a reference answer, otherwise the learner has nothing to compare against")
	}
	return nil
}

func (g speakAnswerGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p speakAnswerPrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var k speakingAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r speakingResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}
	// speak_answer has no min_seconds: the learner thinks, then speaks, so only the
	// upper bound is authored and gradeSpeaking applies the default lower bound.
	return gradeSpeaking(g.Type(), r, 0, p.MaxSeconds, map[string]any{
		"question":       p.Question,
		"key_points":     p.KeyPoints,
		"reference_text": fallbackText(k.ReferenceText, k.Reference),
	})
}

func fallbackText(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// scoredSpeakingLimitation states what a pronunciation score is and is not. A learner
// who reads a number without this will believe it judged their Chinese.
const scoredSpeakingLimitation = "scored by the configured pronunciation service, which " +
	"heard the recording and compared it with the reference: accuracy of sounds and tones, " +
	"fluency, and how much of the reference was completed. It is not an examiner — it does " +
	"not judge whether your answer was appropriate, and a low score means \"hard to " +
	"recognise\", not \"wrong Chinese\"."

// gradeScoredSpeaking reports what the service measured.
//
// is_correct stays null even here. A pronunciation score measures how someone
// sounded; it is not a verdict on whether their Chinese was right, and a speaking
// drill that can be marked "wrong" is one learners stop attempting. What the score
// does decide is whether the drill counts as done — and therefore whether it still
// asks a human to review it, which is the thing tier 1 could never answer.
func gradeScoredSpeaking(grader string, r speakingResponse, feedback map[string]any) Result {
	overall := clamp01(*r.Score)
	passed := overall >= speakingPassingBand

	feedback["has_speech_recognition"] = true
	feedback["provider"] = r.Provider
	feedback["score"] = overall
	feedback["accuracy"] = r.Accuracy
	feedback["fluency"] = r.Fluency
	feedback["completeness"] = r.Completeness
	feedback["prosody"] = r.Prosody
	feedback["passing_band"] = speakingPassingBand
	feedback["passed"] = passed
	// Nothing is queued for a human: this drill has been judged, by a machine, and the
	// honest thing is to say so rather than leave it "pending review" forever.
	feedback["needs_review"] = false
	feedback["limitation"] = scoredSpeakingLimitation
	feedback["words"] = r.Words

	// The words worth practising again, weakest first — the part of the feedback a
	// learner can act on.
	weak := make([]map[string]any, 0)
	for _, w := range r.Words {
		if w.Accuracy < speakingPassingBand {
			weak = append(weak, map[string]any{"word": w.Word, "accuracy": w.Accuracy, "error_type": w.ErrorType})
		}
	}
	sort.SliceStable(weak, func(i, j int) bool { return weak[i]["accuracy"].(float64) < weak[j]["accuracy"].(float64) })
	feedback["weak_words"] = weak

	if !passed {
		feedback["message"] = fmt.Sprintf(
			"the pronunciation service scored this %.0f%%, below the %.0f%% that marks the drill done — listen to the reference again and retry",
			overall*100, speakingPassingBand*100)
	}
	return Result{IsCorrect: nil, Score: overall, Grader: grader, Feedback: feedback}
}
