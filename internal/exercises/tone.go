package exercises

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Tone drills: the pronunciation question a learner can answer without a
// pronunciation provider.
//
// Why a separate type rather than another MCQ: tone is the thing that decides whether
// Mandarin is understood, and a drill that asks about it deserves a name of its own —
// in the type list, in the UI label, and in what the validator enforces. `tone_mcq`
// refuses to import content that does not state the tones it is asking about, so a
// tone drill with no answer cannot reach a learner.
//
// Two shapes, one type, because they ask the same question:
//
//   - a single-syllable word is a LISTENING drill. The audio is the only source, and
//     the answer is the tone actually spoken. Single syllables cannot undergo tone
//     sandhi, so the audio and the answer can never disagree.
//   - a multi-syllable word asks about the tones WRITTEN for it: 你好 is written 3-3
//     and spoken 2-3, so a listening question would mark a learner wrong for hearing
//     correctly. The explanation states both, which is teaching rather than a trick.
//
// Neither shape needs speech recognition: the learner picks a tone, the server
// compares it with the answer key.

// toneNames maps a tone number to how it is presented as an option.
var toneNames = map[string]string{
	"1": "1st tone — high level",
	"2": "2nd tone — rising",
	"3": "3rd tone — falling then rising",
	"4": "4th tone — falling",
	"5": "neutral — light and short",
}

// ToneName returns the human name of a tone digit, or the digit itself when it is
// something we do not recognise (a mistake in content should be visible, not silently
// relabelled).
func ToneName(tone string) string {
	if name, ok := toneNames[strings.TrimSpace(tone)]; ok {
		return name
	}
	return strings.TrimSpace(tone)
}

type tonePrompt struct {
	Instruction string `json:"instruction"`
	// Text is the word under test. It is shown: the drill asks about this word's tones,
	// and the learner is not being tested on reading a character they have just met.
	Text string `json:"text"`
	// AudioKey is present for the single-syllable shape, filled in by the importer from
	// audio_text. Its presence is what makes this a listening question.
	AudioKey    string      `json:"audio_key,omitempty"`
	AudioText   string      `json:"audio_text,omitempty"`
	Options     []mcqOption `json:"options"`
	ReplayLimit int         `json:"replay_limit,omitempty"`
}

type toneAnswerKey struct {
	CorrectID string `json:"correct_id"`
	// Tones is the answer in the same shape as the options, e.g. "4" or "4-3". It is
	// required: it is what the drill is actually asking, and what the UI groups mastery
	// by, and a tone drill whose author forgot the tones is an unanswerable exercise.
	Tones string `json:"tones"`
	// Why explains the answer, and states the spoken form when it differs from the
	// written one.
	Why string `json:"why,omitempty"`
}

type toneMCQGrader struct{}

func (toneMCQGrader) Type() string { return "tone_mcq" }

func (toneMCQGrader) Validate(prompt, answerKey json.RawMessage) error {
	var p tonePrompt
	if err := decode(prompt, &p); err != nil {
		return err
	}
	if err := requireNonEmptyArg("prompt.text", p.Text); err != nil {
		return err
	}
	if len(p.Options) < 2 {
		return fmt.Errorf("exercises: tone_mcq needs at least two tone options")
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
			return fmt.Errorf("exercises: duplicate option id %q in tone_mcq", o.ID)
		}
		seen[o.ID] = true
	}
	// A listening tone question must have audio; without it the learner has nothing to
	// answer from, since the pinyin is deliberately not shown.
	if strings.TrimSpace(p.AudioText) != "" {
		if err := requireAudioKey(p.AudioKey); err != nil {
			return err
		}
	}

	var k toneAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return err
	}
	if err := requireNonEmptyArg("answer_key.tones", k.Tones); err != nil {
		return err
	}
	// The answer must be one of the choices, or the exercise cannot be answered at all.
	if !seen[k.CorrectID] {
		return fmt.Errorf("exercises: answer_key.correct_id %q is not one of the options", k.CorrectID)
	}
	return nil
}

func (toneMCQGrader) Grade(prompt, answerKey, response json.RawMessage) (Result, error) {
	var p tonePrompt
	if err := decode(prompt, &p); err != nil {
		return Result{}, err
	}
	var k toneAnswerKey
	if err := decode(answerKey, &k); err != nil {
		return Result{}, err
	}
	var r mcqResponse
	if err := decodeResponse(response, &r); err != nil {
		return Result{}, err
	}

	// A choice that is not among the options is a graded failure with an explanation,
	// matching every other choice drill — never a silent zero.
	known := false
	for _, o := range p.Options {
		if o.ID == r.OptionID {
			known = true
			break
		}
	}
	if r.OptionID == "" || !known {
		return Result{IsCorrect: boolPtr(false), Score: 0, Grader: "tone_mcq", Feedback: map[string]any{
			"correct_id":     k.CorrectID,
			"tones":          k.Tones,
			"your_choice":    r.OptionID,
			"unknown_option": r.OptionID,
			"message":        "that choice is not one of this question's tone options",
		}}, nil
	}

	correct := r.OptionID == k.CorrectID
	feedback := map[string]any{
		"correct_id":  k.CorrectID,
		"tones":       k.Tones,
		"your_choice": r.OptionID,
	}
	if k.Why != "" {
		feedback["why"] = k.Why
	}
	score := 0.0
	if correct {
		score = 1
	}
	return Result{IsCorrect: boolPtr(correct), Score: score, Grader: "tone_mcq", Feedback: feedback}, nil
}
