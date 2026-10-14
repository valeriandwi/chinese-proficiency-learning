package media

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAudioKeyIsStableAndContentDerived(t *testing.T) {
	base := AudioKey("say", "Tingting", "你好", "mp3")

	// Stability is what makes re-importing free: the same content must resolve to
	// the same asset, or every import would mint new files.
	if again := AudioKey("say", "Tingting", "你好", "mp3"); again != base {
		t.Fatalf("the same inputs produced different keys: %q vs %q", base, again)
	}
	if !strings.HasSuffix(base, ".mp3") {
		t.Errorf("key %q should carry the container extension", base)
	}
	if len(strings.TrimSuffix(base, ".mp3")) != 32 {
		t.Errorf("key %q should carry a 16-byte hash prefix", base)
	}

	// Changing anything that changes the audio must change the key, or a fixed
	// pronunciation would keep being served after the text is corrected.
	variants := map[string]string{
		"different text":     AudioKey("say", "Tingting", "你好吗", "mp3"),
		"different voice":    AudioKey("say", "Meijia", "你好", "mp3"),
		"different provider": AudioKey("azure", "Tingting", "你好", "mp3"),
		"different format":   AudioKey("say", "Tingting", "你好", "opus"),
	}
	for name, got := range variants {
		if got == base {
			t.Errorf("%s produced the same key as the original: %q", name, got)
		}
	}
}

func TestTextForAudio(t *testing.T) {
	cases := map[string]string{
		"  你好  ":      "你好",
		"你___！":       "你！",
		"我\u3000爱 中国": "我 爱 中国",
		"明天我去学校。":     "明天我去学校。",
	}
	for in, want := range cases {
		if got := TextForAudio(in); got != want {
			t.Errorf("TextForAudio(%q) = %q, want %q", in, got, want)
		}
	}
	// A blank marker must never be read aloud as a literal underscore sequence.
	if got := TextForAudio("你___好"); strings.Contains(got, "_") {
		t.Errorf("blank markers must be removed before synthesis, got %q", got)
	}
}

func TestLocalStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	store, err := NewLocalStore(dir, "/media")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	ctx := context.Background()
	key := AudioKey("say", "Tingting", "你好", "mp3")
	payload := []byte("not really mp3 bytes")

	if store.Exists(key) {
		t.Fatal("a fresh store should not report the asset as present")
	}
	if err := store.Put(ctx, key, payload, "audio/mpeg"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if !store.Exists(key) {
		t.Fatal("Exists should be true after Put")
	}
	data, contentType, err := store.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if string(data) != string(payload) {
		t.Errorf("Get returned %q, want %q", data, payload)
	}
	if contentType != "audio/mpeg" {
		t.Errorf("content type = %q, want audio/mpeg", contentType)
	}
	if url := store.URL(key); url != "/media/"+key {
		t.Errorf("URL = %q, want /media/%s (same-origin, so the browser needs no cross-site request)", url, key)
	}
	if _, _, err := store.Get(ctx, "missing.mp3"); err != ErrNotFound {
		t.Errorf("Get on a missing key returned %v, want ErrNotFound", err)
	}
}

func TestLocalStoreRejectsUnsafeKeys(t *testing.T) {
	store, err := NewLocalStore(t.TempDir(), "/media")
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	ctx := context.Background()
	// Keys are generated internally, so a rejection means a bug or a hostile
	// request reaching the handler.
	for _, key := range []string{"", "../secret.mp3", "sub/dir.mp3", `..\win.mp3`, ".."} {
		if err := store.Put(ctx, key, []byte("x"), "audio/mpeg"); err == nil {
			t.Errorf("Put accepted the unsafe key %q", key)
		}
		if _, _, err := store.Get(ctx, key); err == nil {
			t.Errorf("Get accepted the unsafe key %q", key)
		}
		if store.Exists(key) {
			t.Errorf("Exists reported true for the unsafe key %q", key)
		}
	}
}

func TestLocalStoreCreatesItsDirectory(t *testing.T) {
	nested := filepath.Join(t.TempDir(), "var", "media")
	if _, err := os.Stat(nested); !os.IsNotExist(err) {
		t.Fatalf("precondition failed: %s already exists", nested)
	}
	if _, err := NewLocalStore(nested, "/media"); err != nil {
		t.Fatalf("NewLocalStore should create its directory: %v", err)
	}
	if info, err := os.Stat(nested); err != nil || !info.IsDir() {
		t.Fatalf("directory was not created: %v", err)
	}
}

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"a.mp3":  "audio/mpeg",
		"a.opus": "audio/ogg",
		"a.ogg":  "audio/ogg",
		"a.png":  "image/png",
		"a.webp": "image/webp",
		"a.bin":  "application/octet-stream",
		"a":      "application/octet-stream",
	}
	for key, want := range cases {
		if got := ContentTypeFor(key); got != want {
			t.Errorf("ContentTypeFor(%q) = %q, want %q", key, got, want)
		}
	}
}
