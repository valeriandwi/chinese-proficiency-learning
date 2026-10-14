// Package media stores and serves learning assets (reference audio today,
// images and learner recordings later) and synthesises them at import time.
//
// Two rules this package exists to enforce:
//
//  1. Keys are content-derived, not invented. The key of an audio asset is a hash
//     of everything that determines its bytes — provider, voice, the exact text
//     and the container format — so an asset is immutable, cacheable forever, and
//     re-importing unchanged content generates nothing.
//  2. A listening exercise cannot exist without its audio. The importer asks this
//     package to ensure the asset, and refuses the import if it cannot.
package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Store persists assets and resolves the path a browser fetches them from.
type Store interface {
	Put(ctx context.Context, key string, data []byte, contentType string) error
	Get(ctx context.Context, key string) (data []byte, contentType string, err error)
	Exists(key string) bool
	// URL returns the same-origin path for a key. Assets are served by the API so
	// the browser never needs a cross-site request (and therefore never a
	// third-party cookie question) to play a listening item.
	URL(key string) string
}

// ErrNotFound is returned when an asset is missing.
var ErrNotFound = errors.New("media: asset not found")

// ErrBadKey is returned for a key that could escape the storage directory.
var ErrBadKey = errors.New("media: key must be a plain file name")

// LocalStore keeps assets in a directory. It exists so the audio pipeline works
// with no cloud credentials; swapping in S3 is a new Store implementation and a
// config change, and nothing above this file knows the difference.
type LocalStore struct {
	Dir    string
	Prefix string
}

// NewLocalStore creates the asset directory if needed.
func NewLocalStore(dir, prefix string) (*LocalStore, error) {
	if dir == "" {
		return nil, errors.New("media: MEDIA_DIR is empty")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("media: creating %s: %w", dir, err)
	}
	if prefix == "" {
		prefix = "/media"
	}
	return &LocalStore{Dir: dir, Prefix: strings.TrimSuffix(prefix, "/")}, nil
}

// validKey rejects anything that is not a single, safe path element. Keys are
// generated internally, so a rejection here means a bug or a hostile request.
func validKey(key string) error {
	if key == "" {
		return ErrBadKey
	}
	if key != path.Base(key) || strings.ContainsAny(key, `/\`) || strings.Contains(key, "..") {
		return fmt.Errorf("%w (got %q)", ErrBadKey, key)
	}
	return nil
}

func (s *LocalStore) Put(_ context.Context, key string, data []byte, _ string) error {
	if err := validKey(key); err != nil {
		return err
	}
	full := filepath.Join(s.Dir, key)
	// Write to a temporary file and rename, so a reader never sees a partial asset.
	tmp := full + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("media: writing %s: %w", key, err)
	}
	if err := os.Rename(tmp, full); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("media: publishing %s: %w", key, err)
	}
	return nil
}

func (s *LocalStore) Get(_ context.Context, key string) ([]byte, string, error) {
	if err := validKey(key); err != nil {
		return nil, "", err
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, key))
	if os.IsNotExist(err) {
		return nil, "", ErrNotFound
	}
	if err != nil {
		return nil, "", fmt.Errorf("media: reading %s: %w", key, err)
	}
	return data, ContentTypeFor(key), nil
}

func (s *LocalStore) Exists(key string) bool {
	if err := validKey(key); err != nil {
		return false
	}
	_, err := os.Stat(filepath.Join(s.Dir, key))
	return err == nil
}

func (s *LocalStore) URL(key string) string {
	if key == "" {
		return ""
	}
	return s.Prefix + "/" + key
}

// ContentTypeFor maps an asset extension to its MIME type.
func ContentTypeFor(key string) string {
	switch strings.ToLower(path.Ext(key)) {
	case ".mp3":
		return "audio/mpeg"
	case ".opus", ".ogg":
		return "audio/ogg"
	case ".m4a", ".aac":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".webp":
		return "image/webp"
	default:
		return "application/octet-stream"
	}
}

// AudioKey derives the immutable key for a synthesised clip: a hash over
// everything that decides its bytes. Two imports of the same sentence with the
// same voice therefore resolve to the same key and generate nothing new, while a
// voice change or an edit to the text produces a new asset instead of silently
// serving the old pronunciation.
func AudioKey(provider, voice, text, format string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{provider, voice, text, format}, "\x00")))
	return hex.EncodeToString(sum[:16]) + "." + strings.TrimPrefix(format, ".")
}

// TextForAudio is what actually gets spoken: the text with markup the synthesiser
// should not read out loud removed, and whitespace collapsed.
func TextForAudio(text string) string {
	replacer := strings.NewReplacer("___", "", "\u3000", " ")
	return strings.Join(strings.Fields(replacer.Replace(text)), " ")
}
