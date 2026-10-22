package media

import (
	"context"
	"errors"
	"os"
	"testing"
)

// The resolve-only provider exists so a Linux server can import the curriculum from the
// audio files that ship in the repository. Two properties make that work, and both are
// worth a test rather than a comment:
//
//  1. it reports the SAME identity as `say`, because AudioKey hashes provider+voice+text+
//     format — a different name would compute different keys and find nothing;
//  2. it never synthesises: a genuinely missing asset must fail with the exercise's name
//     rather than importing silence.

func TestExistingAudioProviderUsesTheSayIdentity(t *testing.T) {
	say := &SayProvider{VoiceName: "Tingting", AudioFormat: "mp3"}
	existing := NewExistingAudioProvider("Tingting")

	if existing.Name() != say.Name() {
		t.Errorf("Name() = %q, want %q: the key would not match the committed audio", existing.Name(), say.Name())
	}
	if existing.Voice() != say.Voice() {
		t.Errorf("Voice() = %q, want %q", existing.Voice(), say.Voice())
	}
	if existing.Format() != say.Format() {
		t.Errorf("Format() = %q, want %q", existing.Format(), say.Format())
	}

	text := TextForAudio("你好吗？")
	want := AudioKey(say.Name(), say.Voice(), text, say.Format())
	got := AudioKey(existing.Name(), existing.Voice(), text, existing.Format())
	if got != want {
		t.Fatalf("key mismatch: existing %q vs say %q — the committed audio would never be found", got, want)
	}
}

func TestExistingAudioProviderDefaultsTheVoice(t *testing.T) {
	if got := NewExistingAudioProvider("").Voice(); got != "Tingting" {
		t.Errorf("empty voice = %q, want the say default Tingting", got)
	}
}

func TestEnsureAudioResolvesAStoredAssetWithoutSynthesising(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir, "/media")
	if err != nil {
		t.Fatalf("local store: %v", err)
	}
	provider := NewExistingAudioProvider("Tingting")
	text := "你好吗？"

	// Place the asset under exactly the key the provider would compute.
	key := AudioKey(provider.Name(), provider.Voice(), TextForAudio(text), provider.Format())
	if err := os.WriteFile(dir+"/"+key, []byte("audio bytes"), 0o644); err != nil {
		t.Fatalf("seeding the asset: %v", err)
	}

	got, generated, err := EnsureAudio(context.Background(), store, provider, text)
	if err != nil {
		t.Fatalf("EnsureAudio refused an asset that exists: %v", err)
	}
	if got != key {
		t.Errorf("key = %q, want %q", got, key)
	}
	if generated {
		t.Error("generated = true: the resolve-only provider must never synthesise")
	}
}

func TestEnsureAudioFailsForAMissingAsset(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), "/media")
	if err != nil {
		t.Fatalf("local store: %v", err)
	}

	_, _, err = EnsureAudio(context.Background(), store, NewExistingAudioProvider("Tingting"), "没有这个")
	if err == nil {
		t.Fatal("EnsureAudio resolved an asset that does not exist: silence would be imported")
	}
	if !errors.Is(err, ErrTTSUnavailable) {
		t.Errorf("error = %v, want ErrTTSUnavailable so the caller can explain why", err)
	}
}

func TestNewTTSSelectsTheResolveOnlyProvider(t *testing.T) {
	provider, err := NewTTS("existing", "", "")
	if err != nil {
		t.Fatalf("NewTTS(existing): %v", err)
	}
	if provider.Name() != SayName {
		t.Errorf("provider name = %q, want %q", provider.Name(), SayName)
	}

	// "none" still means "no audio at all", and is still an error rather than a silent
	// resolve-only fallback.
	if _, err := NewTTS("none", "", ""); !errors.Is(err, ErrTTSUnavailable) {
		t.Errorf("NewTTS(none) error = %v, want ErrTTSUnavailable", err)
	}
}
