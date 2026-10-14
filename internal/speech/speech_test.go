package speech

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

// ffmpegPathOrFail finds ffmpeg or fails loudly. A skipped test here would report
// green while the conversion path — the thing that turns a browser recording into
// something the vendor accepts — was never exercised at all.
func ffmpegPathOrFail(t *testing.T) string {
	t.Helper()
	path, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Fatalf("ffmpeg is not on PATH, so the audio conversion cannot be tested: %v", err)
	}
	return path
}

// wavTone builds a minimal valid 16 kHz mono PCM WAV: a short sine, which is enough
// to prove decoding, resampling and piping work.
func wavTone(samples int) []byte {
	const rate = 8000
	var buf []byte
	push := func(v any) {
		if err := binary.Write(byteWriter{&buf}, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
	push([]byte("RIFF"))
	push(uint32(36 + samples*2))
	push([]byte("WAVEfmt "))
	push(uint32(16))
	push(uint16(1))        // PCM
	push(uint16(1))        // mono
	push(uint32(rate))     // sample rate
	push(uint32(rate * 2)) // byte rate
	push(uint16(2))        // block align
	push(uint16(16))       // bits
	push([]byte("data"))
	push(uint32(samples * 2))
	for i := 0; i < samples; i++ {
		push(int16(math.Sin(2*math.Pi*440*float64(i)/rate) * 8000))
	}
	return buf
}

type byteWriter struct{ b *[]byte }

func (w byteWriter) Write(p []byte) (int, error) {
	*w.b = append(*w.b, p...)
	return len(p), nil
}

func TestFakeScorerIsDeterministic(t *testing.T) {
	scorer := &FakeScorer{}
	req := Request{Reference: "你好吗", Audio: wavTone(800)}

	first, err := scorer.Score(context.Background(), req)
	if err != nil {
		t.Fatalf("scoring: %v", err)
	}
	second, err := scorer.Score(context.Background(), req)
	if err != nil {
		t.Fatalf("scoring again: %v", err)
	}
	if first.Overall != second.Overall || len(first.Words) != len(second.Words) {
		t.Fatalf("the fake scorer is not deterministic: %+v vs %+v", first, second)
	}

	// One unit per character: Chinese has no spaces, so an assessable unit is a
	// character and the count must match the reference.
	if len(first.Words) != 3 {
		t.Fatalf("got %d word scores for a 3-character reference: %+v", len(first.Words), first.Words)
	}
	for _, w := range first.Words {
		if w.Accuracy < 0 || w.Accuracy > 1 {
			t.Errorf("word accuracy out of range: %+v", w)
		}
		if w.ErrorType == "" {
			t.Errorf("word has no error type: %+v", w)
		}
	}
	for name, value := range map[string]float64{
		"overall": first.Overall, "accuracy": first.Accuracy, "fluency": first.Fluency,
		"completeness": first.Completeness, "prosody": first.Prosody,
	} {
		if value < 0 || value > 1 {
			t.Errorf("%s is %v, outside 0..1", name, value)
		}
	}

	// Different audio must produce a different number, or the fake is not scored
	// from the recording at all.
	other, err := scorer.Score(context.Background(), Request{Reference: "你好吗", Audio: wavTone(1600)})
	if err != nil {
		t.Fatalf("scoring other audio: %v", err)
	}
	if other.Overall == first.Overall {
		t.Fatalf("two different recordings scored identically (%v); the fake ignores the audio", other.Overall)
	}
}

func TestFakeScorerRejectsEmptyInput(t *testing.T) {
	if _, err := (&FakeScorer{}).Score(context.Background(), Request{Reference: "你好"}); !errors.Is(err, ErrEmptyAudio) {
		t.Fatalf("empty audio: got %v, want ErrEmptyAudio", err)
	}
	if _, err := (&FakeScorer{}).Score(context.Background(), Request{Audio: []byte{1, 2, 3}}); err == nil {
		t.Fatal("a missing reference text must be an error: there is nothing to score against")
	}
}

// TestAzureSentenceSendsWhatTheApiNeeds pins the request the vendor contract
// requires: key header, PCM WAV content type, and the base64 assessment config that
// decides granularity. None of this is checked by calling a real endpoint in tests.
func TestAzureSentenceSendsWhatTheApiNeeds(t *testing.T) {
	var got *http.Request
	var body []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		body = make([]byte, r.ContentLength)
		_, _ = r.Body.Read(body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"RecognitionStatus":"Success","NBest":[{"PronunciationAssessment":{"AccuracyScore":80,"FluencyScore":70,"CompletenessScore":90,"PronScore":78,"ProsodyScore":60},"Words":[{"Word":"你","PronunciationAssessment":{"AccuracyScore":90,"ErrorType":"None"}},{"Word":"好","PronunciationAssessment":{"AccuracyScore":40,"ErrorType":"Mispronunciation"}}]}]}`))
	}))
	defer server.Close()

	scorer := NewAzureScorer("test-key", "test-region", ffmpegPathOrFail(t))
	scorer.endpoint = server.URL

	score, err := scorer.Score(context.Background(), Request{Reference: "你好", Audio: wavTone(1600)})
	if err != nil {
		t.Fatalf("scoring: %v", err)
	}

	if got.Method != http.MethodPost {
		t.Errorf("method %s, want POST", got.Method)
	}
	for _, want := range []string{"language=zh-CN", "format=detailed"} {
		if !strings.Contains(got.URL.RawQuery, want) {
			t.Errorf("query %q does not contain %q", got.URL.RawQuery, want)
		}
	}
	if got.Header.Get("Ocp-Apim-Subscription-Key") != "test-key" {
		t.Errorf("subscription key header is %q", got.Header.Get("Ocp-Apim-Subscription-Key"))
	}
	if ct := got.Header.Get("Content-Type"); !strings.HasPrefix(ct, "audio/wav") {
		t.Errorf("content type %q, want audio/wav", ct)
	}
	rawConfig, err := base64.StdEncoding.DecodeString(got.Header.Get("Pronunciation-Assessment"))
	if err != nil {
		t.Fatalf("the assessment header is not base64: %v", err)
	}
	var parsed assessmentConfig
	if err := json.Unmarshal(rawConfig, &parsed); err != nil {
		t.Fatalf("the assessment header is not JSON: %v", err)
	}
	if parsed.ReferenceText != "你好" || parsed.Granularity != "Phoneme" || parsed.GradingSystem != "HundredMark" {
		t.Errorf("assessment config is wrong: %+v", parsed)
	}
	if len(body) < 44 || string(body[0:4]) != "RIFF" {
		t.Errorf("the request body is not a WAV file (first bytes %q)", string(body[:min(4, len(body))]))
	}

	// The response is 0..100 from the vendor; everything we store is 0..1.
	if score.Overall < 0.77 || score.Overall > 0.79 {
		t.Errorf("overall %v, want ~0.78", score.Overall)
	}
	if score.Provider != "azure" {
		t.Errorf("provider %q", score.Provider)
	}
	if len(score.Words) != 2 || score.Words[1].ErrorType != "Mispronunciation" {
		t.Errorf("per-word detail lost: %+v", score.Words)
	}
	if score.Words[1].Accuracy < 0.39 || score.Words[1].Accuracy > 0.41 {
		t.Errorf("word accuracy %v, want ~0.40", score.Words[1].Accuracy)
	}
}

func TestAzureMapsProviderFailures(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		body    string
		wantErr error
	}{
		{"bad key", http.StatusUnauthorized, `{}`, ErrProviderAuth},
		{"throttled", http.StatusTooManyRequests, `{}`, ErrProviderBusy},
		{"silence", http.StatusOK, `{"RecognitionStatus":"NoMatch","NBest":[]}`, ErrNoSpeech},
		{"no assessment", http.StatusOK, `{"RecognitionStatus":"Success","NBest":[]}`, ErrNoSpeech},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()

			scorer := NewAzureScorer("k", "r", ffmpegPathOrFail(t))
			scorer.endpoint = server.URL
			_, err := scorer.Score(context.Background(), Request{Reference: "你好", Audio: wavTone(800)})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestWAV16kMonoRejectsWhatItCannotDecode(t *testing.T) {
	ffmpeg := ffmpegPathOrFail(t)
	if _, err := WAV16kMono(context.Background(), ffmpeg, nil); !errors.Is(err, ErrEmptyAudio) {
		t.Fatalf("empty input: got %v", err)
	}
	if _, err := WAV16kMono(context.Background(), ffmpeg, []byte("this is not audio, it is a sentence")); !errors.Is(err, ErrUnsupportedAudio) {
		t.Fatalf("junk input: got %v, want ErrUnsupportedAudio", err)
	}
	out, err := WAV16kMono(context.Background(), ffmpeg, wavTone(800))
	if err != nil {
		t.Fatalf("converting a real tone: %v", err)
	}
	if len(out) <= 44 || string(out[0:4]) != "RIFF" {
		t.Fatalf("conversion did not produce a WAV (%d bytes)", len(out))
	}
}

func TestNewSelectsAProviderOrRefuses(t *testing.T) {
	if _, err := New(Config{}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("an empty provider must mean unavailable, got %v", err)
	}
	if _, err := New(Config{Provider: "none"}); !errors.Is(err, ErrNoProvider) {
		t.Fatalf("none: got %v", err)
	}
	if _, err := New(Config{Provider: "fake", AppEnv: "development"}); err != nil {
		t.Fatalf("fake in development: %v", err)
	}
	// A fake score shown to a real learner is a lie about their Chinese, so it is
	// refused where it would matter.
	if _, err := New(Config{Provider: "fake", AppEnv: "production"}); err == nil {
		t.Fatal("the fake provider must be refused in production")
	}
	if _, err := New(Config{Provider: "azure", AppEnv: "production"}); err == nil {
		t.Fatal("azure without credentials must be refused rather than silently disabled")
	}
	if _, err := New(Config{Provider: "azure", AppEnv: "production", AzureKey: "k", AzureRegion: "r"}); err == nil {
		t.Fatal("azure without an ffmpeg path must be refused: it cannot convert the upload")
	}
	if _, err := New(Config{Provider: "azure", AppEnv: "production", AzureKey: "k", AzureRegion: "r", FFmpegPath: "ffmpeg"}); err != nil {
		t.Fatalf("a fully configured azure provider must build: %v", err)
	}
	if _, err := New(Config{Provider: "whisper"}); err == nil {
		t.Fatal("an unknown provider must be refused, not silently treated as none")
	}
}

func TestSplitUnitsHandlesBothWritingSystems(t *testing.T) {
	if got := splitUnits("你好吗？"); len(got) != 3 {
		t.Errorf("Chinese reference split into %v", got)
	}
	if got := splitUnits("ni hao ma"); len(got) != 3 {
		t.Errorf("spaced reference split into %v", got)
	}
	// A reference containing spaces is whitespace-split, including any Chinese in
	// it: the units only feed the fake provider's word list, and the real provider
	// segments words itself.
	if got := splitUnits("你好 ma"); len(got) != 2 {
		t.Errorf("mixed reference split into %v", got)
	}
	if fmt.Sprint(splitUnits("")) != "[]" {
		t.Errorf("empty reference split into %v", splitUnits(""))
	}
}
