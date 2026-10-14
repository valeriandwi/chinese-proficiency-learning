package speech

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"unicode/utf8"
)

// FakeScorer produces deterministic pseudo-scores from the audio and the reference.
//
// Why it exists: the whole path — upload caps, storage, grading integration, the UI
// that shows a weak word — needs to run offline, in tests, and in a dev environment
// with no vendor account. It is not a pronunciation model and never pretends to be
// one: the same bytes always produce the same numbers, which is what makes the rest
// of the system testable without a key.
//
// It refuses to run in production (see New), because a fake score shown to a real
// learner is a lie about their Chinese.
type FakeScorer struct{}

func (f *FakeScorer) Name() string { return "fake" }

func (f *FakeScorer) Score(_ context.Context, req Request) (Score, error) {
	if len(req.Audio) == 0 {
		return Score{}, ErrEmptyAudio
	}
	reference := strings.TrimSpace(req.Reference)
	if reference == "" {
		return Score{}, fmt.Errorf("speech: a reference text is required to score against")
	}

	// One hash drives every number, so a test can pin an exact expectation and the
	// same recording always scores the same.
	sum := sha256.Sum256(append(append([]byte{}, req.Audio...), []byte(reference)...))

	// Spread deliberately below and above the passing band: a fake that always
	// passes would hide every below-band code path from the test suite.
	overall := 0.35 + float64(sum[0])/255*0.6
	wordAccuracy := func(i int) float64 {
		return clamp01(overall - 0.1 + float64(sum[(i+1)%len(sum)])/255*0.2)
	}

	units := splitUnits(reference)
	words := make([]WordScore, 0, len(units))
	for i, unit := range units {
		accuracy := wordAccuracy(i)
		words = append(words, WordScore{Word: unit, Accuracy: accuracy, ErrorType: errorTypeFor(accuracy)})
	}

	return Score{
		Provider:     f.Name(),
		Model:        "deterministic-fake",
		Overall:      clamp01(overall),
		Accuracy:     clamp01(overall + 0.02),
		Fluency:      clamp01(overall - 0.03),
		Completeness: clamp01(overall + 0.05),
		Prosody:      clamp01(overall - 0.06),
		Words:        words,
	}, nil
}

// splitUnits divides a reference into the units a learner is assessed on. Chinese is
// written without spaces, so a bare character sequence is split per character; a
// spaced reference (pinyin, or a sentence with punctuation and words) splits on
// whitespace.
func splitUnits(reference string) []string {
	if strings.ContainsAny(reference, " \t") {
		return strings.Fields(reference)
	}
	units := make([]string, 0, utf8.RuneCountInString(reference))
	for _, r := range reference {
		if strings.ContainsRune("，。！？、；：（）「」！?", r) {
			continue
		}
		units = append(units, string(r))
	}
	return units
}

func errorTypeFor(accuracy float64) string {
	switch {
	case accuracy >= 0.8:
		return ErrorNone
	case accuracy >= 0.65:
		return ErrorMispronun
	default:
		return ErrorMispronun
	}
}
