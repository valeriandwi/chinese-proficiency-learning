package speech

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// AzureScorer calls Microsoft's Pronunciation Assessment (the short-audio REST API).
//
// It is the provider that can judge Mandarin properly: the response carries a
// phoneme-level assessment, per-word accuracy with an error taxonomy, and scores for
// accuracy, fluency, completeness and prosody — including tone, which is what a
// learner of Chinese most needs told about.
//
// Cost and obligations, recorded once so nobody has to rediscover them: this sends
// the learner's recording and a text-to-speech subscription key to Microsoft, it is
// billed per hour of audio, and the audio is subject to Microsoft's terms rather than
// ours. It is therefore opt-in (`SPEECH_PROVIDER=azure`), off by default, and paired
// with a UI that tells the learner their voice is being sent before they record. See
// docs/sources.md §7.
type AzureScorer struct {
	key      string
	region   string
	ffmpeg   string
	client   *http.Client
	endpoint string // overridden in tests; derived from the region otherwise
	language string
}

// NewAzureScorer builds the provider. ffmpegPath is required because the API takes
// 16 kHz mono PCM WAV and browsers produce WebM/Opus; converting server-side means
// the client never has to care about the vendor's container preference.
func NewAzureScorer(key, region, ffmpegPath string) *AzureScorer {
	return &AzureScorer{
		key:    key,
		region: region,
		ffmpeg: ffmpegPath,
		client: &http.Client{Timeout: 20 * time.Second},
		endpoint: fmt.Sprintf("https://%s.stt.speech.microsoft.com/speech/recognition/conversation/cognitiveservices/v1",
			region),
		language: "zh-CN",
	}
}

func (a *AzureScorer) Name() string { return "azure" }

// assessmentConfig is the base64 header the API uses to describe what to assess.
// HundredMark keeps scores in the 0..100 range a human expects; the client-side
// conversion to 0..1 happens in parse.
type assessmentConfig struct {
	ReferenceText string `json:"ReferenceText"`
	GradingSystem string `json:"GradingSystem"`
	Granularity   string `json:"Granularity"`
	Dimension     string `json:"Dimension"`
}

type azureWord struct {
	Word                    string `json:"Word"`
	PronunciationAssessment struct {
		AccuracyScore float64 `json:"AccuracyScore"`
		ErrorType     string  `json:"ErrorType"`
	} `json:"PronunciationAssessment"`
}

type azureResponse struct {
	RecognitionStatus string `json:"RecognitionStatus"`
	DisplayText       string `json:"DisplayText"`
	NBest             []struct {
		PronunciationAssessment struct {
			AccuracyScore     float64 `json:"AccuracyScore"`
			FluencyScore      float64 `json:"FluencyScore"`
			CompletenessScore float64 `json:"CompletenessScore"`
			PronScore         float64 `json:"PronScore"`
			ProsodyScore      float64 `json:"ProsodyScore"`
		} `json:"PronunciationAssessment"`
		Words []azureWord `json:"Words"`
	} `json:"NBest"`
}

func (a *AzureScorer) Score(ctx context.Context, req Request) (Score, error) {
	if len(req.Audio) == 0 {
		return Score{}, ErrEmptyAudio
	}
	reference := strings.TrimSpace(req.Reference)
	if reference == "" {
		return Score{}, fmt.Errorf("speech: a reference text is required to score against")
	}

	wav, err := WAV16kMono(ctx, a.ffmpeg, req.Audio)
	if err != nil {
		return Score{}, err
	}

	config := assessmentConfig{
		ReferenceText: reference,
		GradingSystem: "HundredMark",
		Granularity:   "Phoneme",
		Dimension:     "Comprehensive",
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return Score{}, fmt.Errorf("speech: encoding the assessment config: %w", err)
	}

	language := a.language
	if req.Language != "" {
		language = req.Language
	}
	endpoint := fmt.Sprintf("%s?language=%s&format=detailed", a.endpoint, url.QueryEscape(language))

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(wav))
	if err != nil {
		return Score{}, fmt.Errorf("speech: building the request: %w", err)
	}
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", a.key)
	httpReq.Header.Set("Content-Type", "audio/wav; codecs=audio/pcm; samplerate=16000")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Pronunciation-Assessment", base64.StdEncoding.EncodeToString(configJSON))

	res, err := a.client.Do(httpReq)
	if err != nil {
		return Score{}, fmt.Errorf("speech: calling the pronunciation service: %w", err)
	}
	defer res.Body.Close()

	// The body is bounded: a vendor error page must not be able to fill memory, and
	// the response can never contain the learner's audio back at us.
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return Score{}, fmt.Errorf("speech: reading the response: %w", err)
	}
	if res.StatusCode == http.StatusUnauthorized || res.StatusCode == http.StatusForbidden {
		return Score{}, fmt.Errorf("%w: the service rejected the subscription key (HTTP %d)", ErrProviderAuth, res.StatusCode)
	}
	if res.StatusCode == http.StatusTooManyRequests {
		return Score{}, fmt.Errorf("%w: the service is rate limiting this key (HTTP 429)", ErrProviderBusy)
	}
	if res.StatusCode != http.StatusOK {
		// The body may echo the reference text, so only its length is logged, never
		// its content.
		return Score{}, fmt.Errorf("speech: the service returned HTTP %d (%d bytes)", res.StatusCode, len(body))
	}

	var parsed azureResponse
	if err := json.Unmarshal(body, &parsed); err != nil {
		return Score{}, fmt.Errorf("speech: parsing the response: %w", err)
	}
	if parsed.RecognitionStatus != "Success" {
		// "NoMatch" is the service saying it heard no speech. That is a learner
		// outcome, not a server fault, and it is reported as such.
		return Score{}, fmt.Errorf("%w: the service reported %q", ErrNoSpeech, parsed.RecognitionStatus)
	}
	if len(parsed.NBest) == 0 {
		return Score{}, fmt.Errorf("%w: the service returned no assessment", ErrNoSpeech)
	}

	best := parsed.NBest[0]
	words := make([]WordScore, 0, len(best.Words))
	for _, w := range best.Words {
		words = append(words, WordScore{
			Word:      w.Word,
			Accuracy:  clamp01(w.PronunciationAssessment.AccuracyScore / 100),
			ErrorType: w.PronunciationAssessment.ErrorType,
		})
	}

	return Score{
		Provider:     a.Name(),
		Model:        "azure-pronunciation-assessment",
		Overall:      clamp01(best.PronunciationAssessment.PronScore / 100),
		Accuracy:     clamp01(best.PronunciationAssessment.AccuracyScore / 100),
		Fluency:      clamp01(best.PronunciationAssessment.FluencyScore / 100),
		Completeness: clamp01(best.PronunciationAssessment.CompletenessScore / 100),
		Prosody:      clamp01(best.PronunciationAssessment.ProsodyScore / 100),
		Words:        words,
	}, nil
}
