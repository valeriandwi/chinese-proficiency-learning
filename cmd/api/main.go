// Command api serves the hanpath HTTP API.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/valeriandwipurnomo/hanpath/internal/attempts"
	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/httpapi"
	"github.com/valeriandwipurnomo/hanpath/internal/mail"
	"github.com/valeriandwipurnomo/hanpath/internal/media"
	"github.com/valeriandwipurnomo/hanpath/internal/speech"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// version is stamped with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	migrateOnly := flag.Bool("migrate", false, "apply migrations and exit")
	flag.Parse()

	if err := run(*migrateOnly); err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
}

func run(migrateOnly bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := newLogger(cfg.LogLevel)
	httpapi.Version = version

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if migrateOnly {
		if err := store.Migrate(ctx, cfg.DatabaseURL, log); err != nil {
			return err
		}
		applied, pending, err := store.MigrationStatus(ctx, cfg.DatabaseURL)
		if err != nil {
			return err
		}
		log.Info("migrations applied", "applied", applied, "pending", pending)
		return nil
	}

	if os.Getenv("MIGRATE_ON_START") != "false" {
		if err := store.Migrate(ctx, cfg.DatabaseURL, log); err != nil {
			return err
		}
	}

	st, err := store.New(ctx, cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer st.Close()

	sender := mail.New(cfg.SMTPAddr, cfg.SMTPFrom, log)

	// Reference audio lives beside the database in development: a local store needs
	// no credentials, and swapping in S3 is a new media.Store implementation.
	mediaStore, err := media.NewLocalStore(cfg.MediaDir, cfg.MediaURLPrefix)
	if err != nil {
		return err
	}
	// Pronunciation scoring is optional. No provider configured is a supported
	// deployment: speaking drills stay self-assessed and the recordings endpoint says
	// so. A provider that IS configured but unusable is a startup failure, because a
	// silent downgrade would leave learners wondering why nothing is ever scored.
	scorer, err := speech.New(speech.Config{
		Provider:    cfg.SpeechProvider,
		AppEnv:      cfg.AppEnv,
		AzureKey:    cfg.SpeechAzureKey,
		AzureRegion: cfg.SpeechAzureRegion,
		FFmpegPath:  cfg.FFmpegPath,
	})
	if err != nil {
		if !errors.Is(err, speech.ErrNoProvider) {
			return err
		}
		log.Info("no pronunciation provider configured; speaking drills stay self-assessed")
		scorer = nil
	}

	srv := httpapi.New(httpapi.Deps{
		Config:   cfg,
		Log:      log,
		Store:    st,
		Attempts: attempts.New(st),
		Media:    mediaStore,
		Mail:     sender,
		Speech:   scorer,
	})

	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           srv.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       90 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info("api listening",
			"addr", cfg.HTTPAddr,
			"env", cfg.AppEnv,
			"public_base", cfg.PublicBase,
			"version", version,
			"mail_transport", mailTransport(cfg))
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("http server: %w", err)
	case <-ctx.Done():
		log.Info("shutdown signal received, draining")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("graceful shutdown: %w", err)
	}
	log.Info("api stopped")
	return nil
}

func mailTransport(cfg *config.Config) string {
	if cfg.SMTPAddr == "" {
		return "log"
	}
	return "smtp " + cfg.SMTPAddr
}

func newLogger(level string) *slog.Logger {
	var lv slog.Level
	switch level {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lv}))
}
