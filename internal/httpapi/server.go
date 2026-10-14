// Package httpapi is the HTTP layer: routing, middleware, request decoding,
// response envelopes and the error contract. It contains no business rules that
// are not about HTTP.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/attempts"
	"github.com/valeriandwipurnomo/hanpath/internal/auth"
	"github.com/valeriandwipurnomo/hanpath/internal/config"
	"github.com/valeriandwipurnomo/hanpath/internal/mail"
	"github.com/valeriandwipurnomo/hanpath/internal/media"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// Deps are the server's dependencies. A struct rather than positional arguments so
// adding one (a storage backend, a speech scorer) does not touch every call site.
type Deps struct {
	Config   *config.Config
	Log      *slog.Logger
	Store    *store.Store
	Attempts *attempts.Service
	Media    media.Store
	Mail     mail.Sender
}

// Server holds the dependencies for every handler.
type Server struct {
	cfg      *config.Config
	log      *slog.Logger
	store    *store.Store
	attempts *attempts.Service
	media    media.Store
	mail     mail.Sender

	authLimiter  *auth.Limiter
	writeLimiter *auth.Limiter
	started      time.Time
}

// New builds the API server.
func New(d Deps) *Server {
	return &Server{
		cfg:          d.Config,
		log:          d.Log,
		store:        d.Store,
		attempts:     d.Attempts,
		media:        d.Media,
		mail:         d.Mail,
		authLimiter:  auth.NewLimiter(20),  // auth endpoints: 20/min per IP
		writeLimiter: auth.NewLimiter(120), // learner writes: 120/min per session
		started:      time.Now(),
	}
}

// Routes builds the router.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.requestID)
	r.Use(s.recoverer)
	r.Use(s.logRequests)
	r.Use(s.cors)
	r.Use(s.loadSession)

	r.Get("/healthz", s.handleHealth)
	r.Get("/readyz", s.handleReady)

	// Reference audio is publishable content, like the curriculum catalogue, so it
	// is served without a session. It is proxied by the SPA's origin in production,
	// which is what lets an <audio> element play it without a cross-site request.
	r.Get("/media/{key}", s.handleMedia)

	r.Route("/api/v1", func(r chi.Router) {
		// Public: no session required.
		r.Group(func(r chi.Router) {
			r.With(s.rateLimit(s.authLimiter, "auth")).Post("/auth/register", s.handleRegister)
			r.With(s.rateLimit(s.authLimiter, "auth")).Post("/auth/login", s.handleLogin)
			r.Get("/auth/session", s.handleSessionInfo)
			r.With(s.rateLimit(s.authLimiter, "auth")).Post("/auth/verify-email", s.handleVerifyEmail)
			r.With(s.rateLimit(s.authLimiter, "auth")).Post("/auth/password-reset/request", s.handlePasswordResetRequest)
			r.With(s.rateLimit(s.authLimiter, "auth")).Post("/auth/password-reset/confirm", s.handlePasswordResetConfirm)
			r.Get("/curriculum", s.handleCurricula)
		})

		// Authenticated. Mutations additionally require the CSRF header.
		r.Group(func(r chi.Router) {
			r.Use(s.requireAuth)

			r.Post("/auth/logout", s.handleLogout)
			r.Get("/me", s.handleMe)
			r.Patch("/me", s.handleUpdateMe)

			r.Get("/placement/questions", s.handlePlacementQuestions)
			r.Post("/me/placement", s.handleSubmitPlacement)

			r.Get("/curriculum/{version}/path", s.handlePath)
			r.Get("/lessons/{id}", s.handleLesson)
			r.Post("/lessons/{id}/attempts", s.handleSubmitAttempts)

			r.Get("/progress/overview", s.handleProgressOverview)
			r.Post("/progress/refresh", s.handleProgressRefresh)

			r.Get("/reviews/queue", s.handleReviewQueue)
			r.Post("/reviews/answer", s.handleReviewAnswer)
		})
	})

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "no route matches this path", nil)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed for this path", nil)
	})
	return r
}

// --- response helpers ------------------------------------------------------

type fieldError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Code      string       `json:"code"`
	Message   string       `json:"message"`
	Details   []fieldError `json:"details,omitempty"`
	RequestID string       `json:"request_id,omitempty"`
}

// writeJSON writes a success response.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	if err := json.NewEncoder(w).Encode(v); err != nil {
		// The status line is already sent; nothing useful can be done here.
		_ = err
	}
}

// writeError writes the single error shape every endpoint uses.
func writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, details []fieldError) {
	env := errorEnvelope{Error: errorBody{Code: code, Message: message, Details: details}}
	if id := requestIDFrom(r.Context()); id != "" {
		env.Error.RequestID = id
	}
	writeJSON(w, status, env)
}

// decodeJSON strictly decodes a request body: unknown fields are rejected, the
// body is size-limited, and a trailing value is an error. A request that carries
// a field the server does not accept must fail loudly rather than be silently
// ignored.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		if i := strings.Index(ct, ";"); i > 0 {
			ct = ct[:i]
		}
		if strings.TrimSpace(ct) != "application/json" {
			writeError(w, r, http.StatusUnsupportedMediaType, "unsupported_media_type",
				fmt.Sprintf("Content-Type must be application/json, got %q", ct), nil)
			return false
		}
	}
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var typeErr *json.UnmarshalTypeError
		var syntaxErr *json.SyntaxError
		switch {
		case errors.As(err, &typeErr):
			writeError(w, r, http.StatusBadRequest, "invalid_json",
				fmt.Sprintf("field %q has the wrong type", typeErr.Field), nil)
		case errors.As(err, &syntaxErr):
			writeError(w, r, http.StatusBadRequest, "invalid_json", "request body is not valid JSON", nil)
		case strings.Contains(err.Error(), "unknown field"):
			writeError(w, r, http.StatusBadRequest, "unknown_field", err.Error(), nil)
		case strings.Contains(err.Error(), "http: request body too large"):
			writeError(w, r, http.StatusRequestEntityTooLarge, "body_too_large", "request body exceeds 1 MiB", nil)
		default:
			writeError(w, r, http.StatusBadRequest, "invalid_json", err.Error(), nil)
		}
		return false
	}
	if dec.More() {
		writeError(w, r, http.StatusBadRequest, "invalid_json", "request body must contain a single JSON object", nil)
		return false
	}
	return true
}

// respondStoreError maps store errors onto HTTP statuses in one place.
func (s *Server) respondStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not_found", "resource not found", nil)
	case errors.Is(err, store.ErrConflict):
		writeError(w, r, http.StatusConflict, "conflict", "resource already exists", nil)
	default:
		s.log.ErrorContext(r.Context(), "internal error", "error", err, "path", r.URL.Path)
		writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", nil)
	}
}

func parseLimit(r *http.Request, def, max int) int {
	raw := r.URL.Query().Get("limit")
	if raw == "" {
		return def
	}
	n, err := strconvAtoi(raw)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}

func strconvAtoi(s string) (int, error) {
	n := 0
	if s == "" {
		return 0, errors.New("empty")
	}
	for _, c := range s {
		if c < '0' || c > '9' {
			return 0, errors.New("not a number")
		}
		n = n*10 + int(c-'0')
		if n > 100000 {
			return 0, errors.New("too large")
		}
	}
	return n, nil
}

func parseUUIDParam(r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	return id, err == nil
}
