// Package store owns all PostgreSQL access. Handlers never build SQL.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib" // database/sql driver, used by goose
	"github.com/pressly/goose/v3"

	"github.com/valeriandwipurnomo/hanpath/migrations"
)

// ErrNotFound is returned instead of pgx.ErrNoRows so callers do not import pgx.
var ErrNotFound = errors.New("store: not found")

// ErrConflict signals a uniqueness violation that the caller should surface as
// a 409 rather than a 500 (for example a duplicate email).
var ErrConflict = errors.New("store: conflict")

// migrationLockID serialises migrations across concurrently starting processes.
const migrationLockID int64 = 918273645

type Store struct {
	Pool *pgxpool.Pool
	log  *slog.Logger
}

// New opens a pool and verifies connectivity. A wrong DATABASE_URL fails here,
// at boot, rather than on the first request.
func New(ctx context.Context, dbURL string, log *slog.Logger) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		return nil, fmt.Errorf("store: parsing DATABASE_URL: %w", err)
	}
	cfg.MaxConns = 12
	cfg.MinConns = 2
	cfg.MaxConnLifetime = time.Hour
	cfg.HealthCheckPeriod = 30 * time.Second

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("store: creating pool: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("store: pinging database: %w", err)
	}
	return &Store{Pool: pool, log: log}, nil
}

func (s *Store) Close() { s.Pool.Close() }

// Ping reports database liveness for the readiness probe.
func (s *Store) Ping(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	return s.Pool.Ping(ctx)
}

// Migrate applies embedded migrations under a session-level advisory lock so two
// processes starting at once cannot race.
func Migrate(ctx context.Context, dbURL string, log *slog.Logger) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return fmt.Errorf("store: opening migration connection: %w", err)
	}
	defer db.Close()

	conn, err := db.Conn(ctx)
	if err != nil {
		return fmt.Errorf("store: acquiring migration connection: %w", err)
	}
	defer conn.Close()

	if _, err := conn.ExecContext(ctx, "SELECT pg_advisory_lock($1)", migrationLockID); err != nil {
		return fmt.Errorf("store: taking migration lock: %w", err)
	}
	defer func() {
		if _, err := conn.ExecContext(context.WithoutCancel(ctx), "SELECT pg_advisory_unlock($1)", migrationLockID); err != nil {
			log.Warn("releasing migration lock failed", "error", err)
		}
	}()

	goose.SetBaseFS(migrations.FS)
	goose.SetLogger(gooseLogger{log: log})
	if err := goose.SetDialect("postgres"); err != nil {
		return fmt.Errorf("store: setting goose dialect: %w", err)
	}
	if err := goose.UpContext(ctx, db, "."); err != nil {
		return fmt.Errorf("store: applying migrations: %w", err)
	}
	return nil
}

// MigrationStatus reports how many migrations have been applied (used by the
// health endpoint to prove the binary and the database agree).
func MigrationStatus(ctx context.Context, dbURL string) (applied, pending int, err error) {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return 0, 0, err
	}
	defer db.Close()
	goose.SetBaseFS(migrations.FS)
	if err := goose.SetDialect("postgres"); err != nil {
		return 0, 0, err
	}
	status, err := goose.GetDBVersionContext(ctx, db)
	if err != nil {
		return 0, 0, err
	}
	all, err := goose.CollectMigrations(".", 0, goose.MaxVersion)
	if err != nil {
		return 0, 0, err
	}
	for _, m := range all {
		if m.Version <= status {
			applied++
		} else {
			pending++
		}
	}
	return applied, pending, nil
}

// withTx runs fn in a transaction, rolling back on error or panic.
func (s *Store) withTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("store: beginning transaction: %w", err)
	}
	defer func() {
		if r := recover(); r != nil {
			_ = tx.Rollback(context.WithoutCancel(ctx))
			panic(r)
		}
	}()
	if err := fn(tx); err != nil {
		_ = tx.Rollback(context.WithoutCancel(ctx))
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("store: committing transaction: %w", err)
	}
	return nil
}

func mapErr(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, pgx.ErrNoRows):
		return ErrNotFound
	case isUniqueViolation(err):
		return ErrConflict
	default:
		return err
	}
}

func isUniqueViolation(err error) bool {
	var pgErr interface{ SQLState() string }
	if errors.As(err, &pgErr) {
		return pgErr.SQLState() == "23505"
	}
	return false
}

type gooseLogger struct{ log *slog.Logger }

// goose hands us a Printf-style format string. Passing those args straight to
// slog would print the literal "%d" and an "!BADKEY" attribute, so the format is
// applied here and the log line carries one message.
func (g gooseLogger) Printf(format string, v ...any) { g.log.Debug(fmt.Sprintf(format, v...)) }

func (g gooseLogger) Fatalf(format string, v ...any) { g.log.Error(fmt.Sprintf(format, v...)) }
