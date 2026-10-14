package media

import (
	"context"
	"errors"
	"os"
	"runtime"
	"testing"
)

// These tests exercise the real synthesiser. They are the only way to know the
// audio pipeline works — a mocked provider would prove nothing about whether a
// learner hears Chinese. They skip when `say`/ffmpeg are absent (Linux CI) and the
// import path fails loudly in that case rather than shipping silent lessons.

func sayProvider(t *testing.T) *SayProvider {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("macOS `say` is not available on this platform")
	}
	p, err := NewSayProvider("Tingting", "")
	if err != nil {
		if errors.Is(err, ErrTTSUnavailable) {
			t.Skipf("no local TTS provider: %v", err)
		}
		t.Fatalf("NewSayProvider: %v", err)
	}
	return p
}

func TestSayProviderSynthesizesChinese(t *testing.T) {
	p := sayProvider(t)
	if p.Format() != "mp3" {
		t.Errorf("format = %q, want mp3", p.Format())
	}
	data, format, err := p.Synthesize(context.Background(), "你好，我叫小明。")
	if err != nil {
		t.Fatalf("Synthesize: %v", err)
	}
	if format != "mp3" {
		t.Errorf("format = %q, want mp3", format)
	}
	// A real clip of a spoken sentence is tens of kilobytes. A few hundred bytes
	// means the synthesiser produced silence or an error page.
	if len(data) < 2000 {
		t.Fatalf("synthesised audio is only %d bytes; that is not a spoken sentence", len(data))
	}
	// The output must be a real container, not an error string written into the
	// file by a tool that failed quietly. ffmpeg's MP3 muxer writes an ID3v2 tag
	// before the first frame, so accept either that or a raw frame sync.
	looksLikeMP3 := (len(data) > 3 && string(data[:3]) == "ID3") ||
		(data[0] == 0xFF && data[1]&0xE0 == 0xE0)
	if !looksLikeMP3 {
		t.Errorf("output does not look like an MP3 (first bytes: % x)", data[:4])
	}
}

func TestSayProviderRejectsAnUnknownVoice(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS `say` is not available on this platform")
	}
	// An unknown voice name makes `say` fall back to a default voice, which would
	// silently produce English audio for Chinese content. It must fail instead.
	_, err := NewSayProvider("DefinitelyNotAVoice", "")
	if err == nil {
		t.Fatal("an unknown voice name must be rejected at construction")
	}
	if errors.Is(err, ErrTTSUnavailable) {
		t.Fatalf("this should be a voice error, not an environment error: %v", err)
	}
}

func TestEnsureAudioIsIdempotent(t *testing.T) {
	p := sayProvider(t)
	store, err := NewLocalStore(t.TempDir(), "/media")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	ctx := context.Background()

	key, generated, err := EnsureAudio(ctx, store, p, "你好")
	if err != nil {
		t.Fatalf("first EnsureAudio: %v", err)
	}
	if !generated {
		t.Error("first call should have generated the asset")
	}
	if !store.Exists(key) {
		t.Fatal("the asset should exist after generation")
	}

	// Re-importing unchanged content must be free: same key, nothing generated.
	again, regenerated, err := EnsureAudio(ctx, store, p, "你好")
	if err != nil {
		t.Fatalf("second EnsureAudio: %v", err)
	}
	if again != key {
		t.Errorf("key changed between imports: %q then %q", key, again)
	}
	if regenerated {
		t.Error("second call regenerated an identical asset; import would not be idempotent")
	}

	// Edited text is a different asset — the learner must not keep hearing the old
	// pronunciation after a correction.
	edited, generatedEdited, err := EnsureAudio(ctx, store, p, "你好吗")
	if err != nil {
		t.Fatalf("EnsureAudio for edited text: %v", err)
	}
	if edited == key || !generatedEdited {
		t.Errorf("edited text should produce a new asset (got key %q, generated=%v)", edited, generatedEdited)
	}
}

func TestEnsureAudioWithoutAProviderFailsLoudly(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), "/media")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	// This is the state a Linux build without a TTS provider is in: importing a
	// listening exercise must fail rather than create an item with no audio.
	if _, _, err := EnsureAudio(context.Background(), store, nil, "你好"); !errors.Is(err, ErrTTSUnavailable) {
		t.Fatalf("expected ErrTTSUnavailable, got %v", err)
	}
	if _, _, err := EnsureAudio(context.Background(), store, nil, ""); err == nil {
		t.Fatal("empty text must be refused")
	}
}

func TestNewTTSSelection(t *testing.T) {
	if runtime.GOOS == "darwin" {
		p, err := NewTTS("", "Tingting", "")
		if err != nil {
			t.Fatalf("the default provider should be the local one: %v", err)
		}
		if p.Name() != "say" {
			t.Errorf("default provider = %q, want say", p.Name())
		}
	}
	// A named provider that does not exist must fail with its name rather than
	// quietly substituting a different voice.
	if _, err := NewTTS("some-cloud-vendor", "voice", ""); err == nil {
		t.Error("an unregistered provider name must be rejected")
	}
	if _, err := NewTTS("none", "", ""); !errors.Is(err, ErrTTSUnavailable) {
		t.Errorf("TTS_PROVIDER=none should report unavailability, got %v", err)
	}
	// ffmpeg is required for transcoding; a bogus path must be reported as such.
	if runtime.GOOS == "darwin" {
		if _, err := NewSayProvider("Tingting", "/nonexistent/ffmpeg"); !errors.Is(err, ErrTTSUnavailable) {
			t.Errorf("a missing ffmpeg should report unavailability, got %v", err)
		}
	}
}

func TestAudioDirectoryIsConfigurable(t *testing.T) {
	// The store must not silently write into the repository root.
	dir := t.TempDir()
	store, err := NewLocalStore(dir, "/assets/audio")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	if store.Dir != dir {
		t.Errorf("store dir = %q, want %q", store.Dir, dir)
	}
	if url := store.URL("abc.mp3"); url != "/assets/audio/abc.mp3" {
		t.Errorf("URL = %q, want /assets/audio/abc.mp3", url)
	}
	if _, err := NewLocalStore("", "/media"); err == nil {
		t.Error("an empty directory must be refused")
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the configured directory should exist: %v", err)
	}
}
