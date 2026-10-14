// Command worker runs asynchronous jobs: session pruning and progress
// recomputation. It is a separate process from the API on purpose — the API must
// stay responsive while a batch of recomputes runs — and it claims work from the
// jobs table with SELECT ... FOR UPDATE SKIP LOCKED, so several workers can run
// without a broker.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

var version = "dev"

func main() {
	once := flag.Bool("once", false, "drain the queue and exit (used by tests and cron)")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := store.New(ctx, cfg.DatabaseURL, log)
	if err != nil {
		fmt.Fprintf(os.Stderr, "fatal: %v\n", err)
		os.Exit(1)
	}
	defer st.Close()

	w := &worker{store: st, log: log, id: hostname() + "-" + uuid.NewString()[:8]}
	log.Info("worker started", "worker_id", w.id, "version", version, "once", *once)

	if err := w.enqueueRecurring(ctx); err != nil {
		log.Error("enqueueing recurring jobs", "error", err)
	}

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		if err := w.runOnce(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Error("job run failed", "error", err)
		}
		if *once {
			log.Info("queue drained, exiting")
			return
		}
		select {
		case <-ctx.Done():
			log.Info("worker stopping")
			return
		case <-ticker.C:
		}
	}
}

type worker struct {
	store *store.Store
	log   *slog.Logger
	id    string
}

// enqueueRecurring registers the periodic jobs if they are not already pending.
func (w *worker) enqueueRecurring(ctx context.Context) error {
	for _, kind := range []string{"prune_sessions"} {
		_, err := w.store.Pool.Exec(ctx, `
			INSERT INTO jobs (id, kind, payload, run_at)
			VALUES ($1, $2, '{}'::jsonb, now())
			ON CONFLICT (kind, (payload -> 'dedupe_key')) WHERE status = 'pending' DO NOTHING`,
			uuid.New(), kind)
		if err != nil {
			return err
		}
	}
	return nil
}

// runOnce claims and executes one job. Claiming and marking done happen in one
// transaction, so a crash mid-job leaves the row 'running' for the reaper rather
// than silently losing it.
func (w *worker) runOnce(ctx context.Context) error {
	return w.store.Tx(ctx, func(tx pgx.Tx) error {
		var id uuid.UUID
		var kind string
		var payload []byte
		err := tx.QueryRow(ctx, `
			SELECT id, kind, payload FROM jobs
			WHERE status = 'pending' AND run_at <= now()
			ORDER BY run_at
			FOR UPDATE SKIP LOCKED
			LIMIT 1`).Scan(&id, &kind, &payload)
		if err == pgx.ErrNoRows {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			UPDATE jobs SET status = 'running', locked_by = $2, locked_at = now(), attempts = attempts + 1
			WHERE id = $1`, id, w.id); err != nil {
			return err
		}

		var runErr error
		switch kind {
		case "prune_sessions":
			n, err := w.store.DeleteExpiredSessions(ctx)
			if err != nil {
				runErr = err
			} else {
				w.log.Info("pruned expired sessions", "deleted", n)
			}
		case "recompute_progress":
			var p struct {
				UserID string `json:"user_id"`
			}
			if err := json.Unmarshal(payload, &p); err != nil {
				runErr = fmt.Errorf("bad payload for %s: %w", kind, err)
				break
			}
			userID, err := uuid.Parse(p.UserID)
			if err != nil {
				runErr = fmt.Errorf("bad user_id in payload: %w", err)
				break
			}
			if err := store.RecomputeProgress(ctx, tx, userID); err != nil {
				runErr = err
			} else {
				w.log.Info("recomputed progress", "user_id", userID)
			}
		default:
			// An unknown job kind is a deployment mismatch, not a reason to spin:
			// fail it immediately with a clear message.
			runErr = fmt.Errorf("unknown job kind %q: this build has no handler for it", kind)
		}

		if runErr != nil {
			if _, err := tx.Exec(ctx, `
				UPDATE jobs SET status = CASE WHEN attempts >= max_attempts THEN 'failed' ELSE 'pending' END,
					last_error = $2, locked_by = NULL, locked_at = NULL,
					run_at = now() + (attempts * interval '30 seconds')
				WHERE id = $1`, id, runErr.Error()); err != nil {
				return err
			}
			return runErr
		}
		// Recurring jobs re-arm themselves.
		if kind == "prune_sessions" {
			if _, err := tx.Exec(ctx, `
				INSERT INTO jobs (id, kind, payload, run_at) VALUES ($1, $2, '{}'::jsonb, now() + interval '1 hour')
				ON CONFLICT (kind, (payload -> 'dedupe_key')) WHERE status = 'pending' DO NOTHING`,
				uuid.New(), kind); err != nil {
				return err
			}
		}
		_, err = tx.Exec(ctx, `UPDATE jobs SET status = 'done', finished_at = now() WHERE id = $1`, id)
		return err
	})
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "worker"
	}
	return h
}
