// Package exercises implements the exercise registry: per-type validation of
// authored content and per-type grading of learner responses.
//
// This registry is the single extension seam of the product. Adding an exercise
// type means adding one file here (Validate + Grade) and one React renderer;
// no schema migration and no change to the attempt pipeline.
//
// Grading contract:
//   - Graders never trust the client. A response is decoded strictly
//     (unknown fields are an error) and every claim is re-derived server-side.
//   - IsCorrect may be nil, meaning "not mechanically gradable". That is a
//     first-class outcome, not a failure: the attempt is still recorded with a
//     score and needs_review feedback rather than being scored as wrong.
package exercises

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Skill values mirror the exercises.skill CHECK constraint.
type Skill string

const (
	SkillVocabulary Skill = "vocabulary"
	SkillGrammar    Skill = "grammar"
	SkillReading    Skill = "reading"
	SkillListening  Skill = "listening"
	SkillWriting    Skill = "writing"
	SkillSpeaking   Skill = "speaking"
)

// Result is the outcome of grading one response.
type Result struct {
	IsCorrect *bool          `json:"is_correct"`
	Score     float64        `json:"score"`
	Grader    string         `json:"grader"`
	Feedback  map[string]any `json:"feedback"`
}

// Grader validates authored content and grades one response.
type Grader interface {
	Type() string
	// Validate is called at content-import time; a returned error rejects the
	// import. It must catch authoring mistakes that would otherwise surface as
	// an ungradeable exercise in front of a learner.
	Validate(prompt, answerKey json.RawMessage) error
	Grade(prompt, answerKey, response json.RawMessage) (Result, error)
}

var registry = map[string]Grader{}

func register(g Grader) { registry[g.Type()] = g }

func init() {
	register(mcqGrader{})
	register(mcqMultiGrader{})
	register(trueFalseGrader{})
	register(fillBlankGrader{})
	register(sentenceOrderGrader{})
	register(translationGrader{})
	register(hanziTraceGrader{})
	register(hanziRecallGrader{})
	register(shortAnswerGrader{})
	// Tone: the pronunciation question that needs no provider (see tone.go).
	register(toneMCQGrader{})
	// Listening: audio in, mechanically gradable.
	register(listeningMCQGrader{})
	register(listeningDictationGrader{})
	// Speaking: audio out, NOT mechanically gradable (see speaking.go).
	register(speakRepeatGrader{})
	register(readAloudGrader{})
	register(speakAnswerGrader{})
}

// ListeningTypes and SpeakingTypes are the type families, kept here so callers
// (the importer's audio step, the UI) do not hardcode a list.
func ListeningTypes() []string { return []string{"listening_mcq", "listening_dictation"} }
func SpeakingTypes() []string {
	return []string{"speak_repeat", "read_aloud", "speak_answer"}
}

// IsMechanicallyGradable reports whether a type's result can assert correctness.
// Speaking types never can in this build, and that fact is asserted by tests.
func IsMechanicallyGradable(exerciseType string) bool {
	for _, t := range SpeakingTypes() {
		if t == exerciseType {
			return false
		}
	}
	return true
}

// ErrBadResponse marks a payload that does not match the exercise type it was sent
// for — an unknown field, a wrong shape, a claim that contradicts the exercise. It
// is the caller's fault, so it surfaces as 422 rather than a 500: a client bug must
// not be reported as a server fault, which is what hid the writing-drill breakage.
var ErrBadResponse = errors.New("exercises: response does not match the exercise type")

// decodeResponse decodes a learner's payload, tagging any failure as ErrBadResponse.
func decodeResponse(payload json.RawMessage, v any) error {
	if err := decode(payload, v); err != nil {
		return fmt.Errorf("%w: %v", ErrBadResponse, err)
	}
	return nil
}

// Types lists every registered exercise type, sorted.
func Types() []string {
	out := make([]string, 0, len(registry))
	for t := range registry {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// Lookup returns the grader for an exercise type.
func Lookup(exerciseType string) (Grader, error) {
	g, ok := registry[exerciseType]
	if !ok {
		return nil, fmt.Errorf("exercises: unknown exercise type %q (known: %s)", exerciseType, strings.Join(Types(), ", "))
	}
	return g, nil
}

// Validate checks authored content for the given type.
func Validate(exerciseType string, prompt, answerKey json.RawMessage) error {
	g, err := Lookup(exerciseType)
	if err != nil {
		return err
	}
	return g.Validate(prompt, answerKey)
}

// Grade grades a response, returning an error only for malformed input or an
// unknown type. A wrong answer is a successful grade with Score 0.
func Grade(exerciseType string, prompt, answerKey, response json.RawMessage) (Result, error) {
	g, err := Lookup(exerciseType)
	if err != nil {
		return Result{}, err
	}
	if len(bytes.TrimSpace(response)) == 0 {
		response = json.RawMessage(`{}`)
	}
	return g.Grade(prompt, answerKey, response)
}

// --- helpers ---------------------------------------------------------------

// decode strictly decodes JSON, rejecting unknown fields. Authored content and
// learner responses are both decoded this way: a typo in a YAML prompt should
// fail the import, not silently produce an exercise with missing options.
func decode(raw json.RawMessage, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("exercises: decoding payload: %w", err)
	}
	return nil
}

func boolPtr(b bool) *bool { return &b }

// normalizeText canonicalises a free-text answer for comparison: it trims
// surrounding whitespace, drops all whitespace (Chinese and pinyin answers are
// typed with stray spaces often enough that they must not decide correctness),
// unifies full-width punctuation, removes terminal punctuation, and lowercases
// ASCII — Chinese has no meaningful casing, but pinyin answers do (Ni Hao vs
// ni hao).
func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Map(func(r rune) rune {
		switch r {
		case '\u3000', ' ', '\t', '\n', '\r':
			return -1
		case '，':
			return ','
		case '。':
			return '.'
		case '？':
			return '?'
		case '！':
			return '!'
		case '：':
			return ':'
		case '；':
			return ';'
		case '、':
			return ','
		case '（':
			return '('
		case '）':
			return ')'
		}
		return r
	}, s)
	s = strings.TrimRight(s, ".,!?;:'\"、 ")
	return strings.ToLower(s)
}

// matchesAny reports whether the normalised response equals any accepted answer.
func matchesAny(response string, accepted []string) bool {
	got := normalizeText(response)
	for _, a := range accepted {
		if normalizeText(a) == got {
			return true
		}
	}
	return false
}

func requireNonEmptyArg(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("exercises: %s must not be empty", name)
	}
	return nil
}
