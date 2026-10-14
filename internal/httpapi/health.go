package httpapi

import (
	"fmt"
	"net/http"
	"time"
)

// handleHealth is a liveness probe: it answers as long as the process is running.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":         "ok",
		"version":        Version,
		"uptime_seconds": int(time.Since(s.started).Seconds()),
	})
}

// handleReady is a readiness probe: it fails when the database is unreachable or,
// more subtly, when the database schema is behind this binary. That second check
// is what turns "the endpoint suddenly 500s" into "you forgot to migrate".
func (s *Server) handleReady(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := contextWithTimeout(r, 5*time.Second)
	defer cancel()

	if err := s.store.Ping(ctx); err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable", "database": "unreachable", "error": err.Error(),
		})
		return
	}
	applied, pending, err := storeMigrationStatus(ctx, s.cfg.DatabaseURL)
	if err != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status": "unavailable", "database": "migration status unknown", "error": err.Error(),
		})
		return
	}
	if pending > 0 {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"status":             "unavailable",
			"migrations_applied": applied,
			"migrations_pending": pending,
			"error":              fmt.Sprintf("%d migration(s) not applied: run the migrate target", pending),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":             "ready",
		"version":            Version,
		"migrations_applied": applied,
	})
}
