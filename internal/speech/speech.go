// Package speech scores a learner's spoken attempt against a reference text.
//
// It exists because a speaking drill with no assessment is a drill with no feedback:
// the tier-1 version of this app could only say "a recording arrived", which is why
// every speaking attempt was flagged for human review. A Scorer answers the question
// the learner actually has — how close was that? — and the honest answer depends on
// who is answering:
//
//   - azure: Microsoft's Pronunciation Assessment. Real phoneme and tone scoring for
//     Mandarin, per-word detail, and the audio leaves the machine. This is the only
//     configured provider that can judge tones, so it is the one a deployment aimed
//     at learners should use.
//   - fake: deterministic pseudo-scores derived from a hash of the audio. It exists
//     so the entire upload -> score -> grade -> display path is exercisable offline
//     and in tests, and it is refused outside development.
//   - none: the feature is off. The endpoint says so (503) and the client falls back
//     to tier 1 rather than pretending it scored anything.
//
// Nothing here stores audio. A Scorer receives bytes and returns numbers; the caller
// drops the bytes. See docs/architecture.md, "Pronunciation is scored, never stored".
package speech

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// PassingBand is the score at or above which a speaking drill counts as done. It is
// the same 0.7 the progress recomputation uses for every exercise, so a clear
// recording is what lets a lesson complete — no separate XP rule for speaking.
const PassingBand = 0.7

// ErrorType values reported per word, following the provider's taxonomy. "None"
// means the word was recognised cleanly.
const (
	ErrorNone       = "None"
	ErrorMispronun  = "Mispronunciation"
	ErrorOmission   = "Omission"
	ErrorInsertion  = "Insertion"
	ErrorUnexpected = "UnexpectedBreak"
	ErrorMissing    = "MissingBreak"
	ErrorMonotone   = "Monotone"
)

// WordScore is one word of the reference the learner was assessed on. Accuracy is
// the provider's judgement of that word alone (0..1).
type WordScore struct {
	Word      string  `json:"word"`
	Accuracy  float64 `json:"accuracy"`
	ErrorType string  `json:"error_type"`
}

// Score is the provider's verdict. Every field is 0..1.
type Score struct {
	Provider     string      `json:"provider"`
	Model        string      `json:"model"`
	Overall      float64     `json:"overall"`
	Accuracy     float64     `json:"accuracy"`
	Fluency      float64     `json:"fluency"`
	Completeness float64     `json:"completeness"`
	Prosody      float64     `json:"prosody"`
	Words        []WordScore `json:"words"`
}

// Request is everything a scorer may look at. Audio is the raw upload, in whatever
// container the browser produced; a provider that needs a specific format converts
// it (see WAV16kMono) rather than demanding it from the client.
type Request struct {
	Reference   string
	Audio       []byte
	ContentType string
	Language    string
}

// Scorer is the seam. Adding a provider means implementing this and extending New;
// no caller changes.
type Scorer interface {
	// Name identifies the provider in stored scores and in the UI.
	Name() string
	// Score assesses a recording against the reference text.
	Score(ctx context.Context, req Request) (Score, error)
}

var (
	// ErrNoProvider means the deployment has no scorer configured. Callers surface
	// this as "not available" rather than as a failure of the learner's recording.
	ErrNoProvider = errors.New("speech: no pronunciation provider is configured")
	// ErrUnsupportedAudio means the upload is not audio we can convert.
	ErrUnsupportedAudio = errors.New("speech: the uploaded file is not supported audio")
	// ErrEmptyAudio is a zero-length or silent upload.
	ErrEmptyAudio = errors.New("speech: the recording is empty")
	// ErrNoSpeech means the provider heard no speech at all. That is an outcome to
	// report to the learner ("we could not hear anything"), not a server fault.
	ErrNoSpeech = errors.New("speech: no speech was recognised in the recording")
	// ErrProviderAuth means the provider rejected our credentials — an operator
	// problem, and never the learner's fault.
	ErrProviderAuth = errors.New("speech: the provider rejected the configured credentials")
	// ErrProviderBusy means the provider is throttling us and the learner should be
	// invited to try again rather than told their answer failed.
	ErrProviderBusy = errors.New("speech: the provider is busy")
	// ErrTooManyRecordings means this learner has hit the daily cap on scored
	// recordings, which exists because every scoring call costs money.
	ErrTooManyRecordings = errors.New("speech: the daily limit for scored recordings has been reached")
)

// Config selects and configures a provider.
type Config struct {
	// Provider is "none", "fake" or "azure" (case-insensitive). Empty behaves as
	// "none": a missing setting must never silently start sending learner audio to a
	// third party.
	Provider string
	// AppEnv gates the fake provider, which must not be reachable in production.
	AppEnv string
	// AzureKey and AzureRegion configure the Azure provider.
	AzureKey    string
	AzureRegion string
	// FFmpegPath converts uploads to the 16 kHz mono WAV the Azure API expects.
	FFmpegPath string
}

// New builds the configured scorer.
func New(cfg Config) (Scorer, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.Provider)) {
	case "", "none":
		return nil, ErrNoProvider
	case "fake":
		if cfg.AppEnv == "production" {
			return nil, fmt.Errorf("speech: the fake scorer is a development provider and must not run in production")
		}
		return &FakeScorer{}, nil
	case "azure":
		if cfg.AzureKey == "" || cfg.AzureRegion == "" {
			return nil, fmt.Errorf("speech: provider azure needs AZURE_SPEECH_KEY and AZURE_SPEECH_REGION")
		}
		if strings.TrimSpace(cfg.FFmpegPath) == "" {
			return nil, fmt.Errorf("speech: provider azure needs FFMPEG_PATH to convert the upload")
		}
		return NewAzureScorer(cfg.AzureKey, cfg.AzureRegion, cfg.FFmpegPath), nil
	default:
		return nil, fmt.Errorf("speech: unknown provider %q (want none, fake or azure)", cfg.Provider)
	}
}

// clamp01 keeps a provider's arithmetic from leaking out of range into a stored
// score, where a CHECK constraint would reject the whole attempt.
func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}
