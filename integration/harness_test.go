package integration

import (
	"log/slog"
	"os"
	"testing"

	"github.com/valeriandwipurnomo/hanpath/internal/attempts"
	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/httpapi"
	"github.com/valeriandwipurnomo/hanpath/internal/mail"
	"github.com/valeriandwipurnomo/hanpath/internal/media"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// testMediaStore keeps generated audio in the test's own temporary directory, so
// an integration run never writes into the repository's var/media.
func testMediaStore(t *testing.T) *media.LocalStore {
	t.Helper()
	store, err := media.NewLocalStore(t.TempDir(), "/media")
	if err != nil {
		t.Fatalf("creating a test media store: %v", err)
	}
	return store
}

// provisionerFor supplies the real synthesiser, so a listening exercise is imported
// exactly as it would be in production (audio generated, key resolved, asset
// stored).
//
// It FAILS rather than skips when no provider can run. The authored content now
// contains listening exercises, so a machine without a synthesiser cannot import
// the curriculum at all — skipping would report a green suite while testing
// nothing, which is the failure mode this test file exists to avoid.
func provisionerFor(t *testing.T) curriculum.AudioProvisioner {
	t.Helper()
	tts, err := media.NewTTS(os.Getenv("TTS_PROVIDER"), os.Getenv("TTS_VOICE"), os.Getenv("FFMPEG_PATH"))
	if err != nil {
		t.Fatalf("cannot import the authored content: %v\n"+
			"the curriculum contains listening exercises, so this suite needs a working "+
			"synthesiser. Install ffmpeg (or set FFMPEG_PATH to it) and make sure it is on "+
			"PATH for the test process; TTS_PROVIDER/TTS_VOICE select the provider and voice.",
			err)
	}
	return curriculum.AudioProvisioner{Store: testMediaStore(t), TTS: tts}
}

// serverFor builds the API with a temporary media store, mirroring how cmd/api
// wires it in production.
func serverFor(t *testing.T, cfg *config.Config, log *slog.Logger, st *store.Store) *httpapi.Server {
	t.Helper()
	return httpapi.New(httpapi.Deps{
		Config:   cfg,
		Log:      log,
		Store:    st,
		Attempts: attempts.New(st),
		Media:    testMediaStore(t),
		Mail:     mail.LogSender{Log: log},
	})
}
