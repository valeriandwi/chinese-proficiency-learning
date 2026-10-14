// Command contentctl validates and imports authored curriculum content.
//
// Usage:
//
//	contentctl validate --file content/hsk2.0/level-1.yaml
//	contentctl import   --file content/hsk2.0/level-1.yaml
//	contentctl migrate
//	contentctl list
//
// validate never touches a database, so content can be checked in CI without a
// Postgres instance.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"

	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/curriculum"
	"github.com/valeriandwipurnomo/hanpath/internal/media"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 {
		return fmt.Errorf("usage: contentctl <validate|import|migrate|list> [--file path]...")
	}
	action := os.Args[1]

	fs := flag.NewFlagSet(action, flag.ExitOnError)
	var files stringSlice
	fs.Var(&files, "file", "path to a curriculum YAML document (repeatable)")
	_ = fs.Parse(os.Args[2:])

	switch action {
	case "validate":
		if len(files) == 0 {
			return fmt.Errorf("validate: at least one --file is required")
		}
		ok := true
		for _, path := range files {
			doc, err := load(path)
			if err != nil {
				return err
			}
			problems := doc.Validate()
			if len(problems) == 0 {
				fmt.Printf("✓ %s — %s (%d levels, %d lexemes)\n", path, doc.Version, len(doc.Levels), len(doc.Vocabulary))
				continue
			}
			ok = false
			fmt.Printf("✗ %s — %d problem(s):\n", path, len(problems))
			for _, p := range problems {
				fmt.Printf("    - %s\n", p)
			}
		}
		if !ok {
			return fmt.Errorf("validation failed")
		}
		return nil

	case "import":
		if len(files) == 0 {
			return fmt.Errorf("import: at least one --file is required")
		}
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
		ctx := context.Background()

		if err := store.Migrate(ctx, cfg.DatabaseURL, log); err != nil {
			return err
		}
		st, err := store.New(ctx, cfg.DatabaseURL, log)
		if err != nil {
			return err
		}
		defer st.Close()

		mediaStore, err := media.NewLocalStore(cfg.MediaDir, cfg.MediaURLPrefix)
		if err != nil {
			return err
		}
		tts, err := media.NewTTS(cfg.TTSProvider, cfg.TTSVoice, cfg.FFmpegPath)
		if err != nil {
			// Not fatal here: a deployment that only serves existing content
			// needs no synthesiser. It becomes fatal the moment content with
			// audio_text is imported, where media.EnsureAudio refuses.
			log.Warn("no text-to-speech provider; importing content with audio will fail", "error", err)
			tts = nil
		} else {
			log.Info("audio synthesis ready", "provider", tts.Name(), "voice", tts.Voice(), "format", tts.Format())
		}
		provisioner := curriculum.AudioProvisioner{Store: mediaStore, TTS: tts}

		for _, path := range files {
			doc, err := load(path)
			if err != nil {
				return err
			}
			report, err := curriculum.Import(ctx, st, doc, provisioner)
			if err != nil {
				return err
			}
			fmt.Printf("imported %s: %d levels, %d units, %d lessons, %d lexemes, %d exercises (%d listening, %d speaking), %d passages\n",
				report.Curriculum, report.Levels, report.Units, report.Lessons, report.Lexemes,
				report.Exercises, report.Listening, report.Speaking, report.Passages)
			if report.AudioGenerated > 0 {
				fmt.Printf("  generated %d audio asset(s) into %s\n", report.AudioGenerated, cfg.MediaDir)
			}
			for _, w := range report.Warnings {
				fmt.Printf("  warning: %s\n", w)
			}
		}
		return nil

	case "migrate":
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
		if err := store.Migrate(context.Background(), cfg.DatabaseURL, log); err != nil {
			return err
		}
		applied, pending, err := store.MigrationStatus(context.Background(), cfg.DatabaseURL)
		if err != nil {
			return err
		}
		fmt.Printf("migrations applied=%d pending=%d\n", applied, pending)
		return nil

	case "list":
		cfg, err := config.Load()
		if err != nil {
			return err
		}
		log := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
		st, err := store.New(context.Background(), cfg.DatabaseURL, log)
		if err != nil {
			return err
		}
		defer st.Close()
		list, err := st.Curricula(context.Background())
		if err != nil {
			return err
		}
		for _, c := range list {
			fmt.Printf("%-8s %-28s %-9s levels=%d units=%d lessons=%d exercises=%d\n",
				c.Version, c.Name, c.Status, c.LevelCount, c.UnitCount, c.LessonCount, c.ExerciseCount)
		}
		return nil

	default:
		return fmt.Errorf("unknown action %q: expected validate, import, migrate or list", action)
	}
}

func load(path string) (*curriculum.File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", path, err)
	}
	defer f.Close()
	return curriculum.Parse(f)
}

type stringSlice []string

func (s *stringSlice) String() string { return fmt.Sprint(*s) }
func (s *stringSlice) Set(v string) error {
	*s = append(*s, v)
	return nil
}
