package httpapi

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/auth"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeySession
	ctxKeyUser
)

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

func requestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}

// sessionFrom returns the authenticated session, if any.
func sessionFrom(ctx context.Context) (*store.Session, bool) {
	v, ok := ctx.Value(ctxKeySession).(*store.Session)
	return v, ok
}

// userFrom returns the authenticated learner, if any.
func userFrom(ctx context.Context) (*store.User, bool) {
	v, ok := ctx.Value(ctxKeyUser).(*store.User)
	return v, ok
}

// requestID attaches an id to every request and echoes it to the client, so a
// user-reported error can be traced to one log line.
func (s *Server) requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			if u, err := uuid.NewV7(); err == nil {
				id = u.String()
			} else {
				id = uuid.NewString()
			}
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKeyRequestID, id)))
	})
}

// recoverer converts a panic into a 500 without killing the process, and logs the
// stack once.
func (s *Server) recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.log.ErrorContext(r.Context(), "panic recovered",
					"panic", fmt.Sprint(rec),
					"path", r.URL.Path,
					"request_id", requestIDFrom(r.Context()),
					"stack", string(debug.Stack()))
				writeError(w, r, http.StatusInternalServerError, "internal_error", "internal server error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// logRequests emits one structured line per request, with no sensitive fields.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &statusRecorder{ResponseWriter: w}
		start := time.Now()
		next.ServeHTTP(rec, r)

		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		}
		s.log.Log(r.Context(), level, "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestIDFrom(r.Context()))
	})
}

// cors allows the configured development origins. In production the SPA is served
// same-origin, so this list is normally empty.
func (s *Server) cors(next http.Handler) http.Handler {
	allowed := map[string]bool{}
	for _, o := range s.cfg.CORSAllowedOrigins {
		allowed[o] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && allowed[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, X-Request-Id")
			w.Header().Set("Access-Control-Max-Age", "600")
			w.Header().Add("Vary", "Origin")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loadSession resolves the session cookie into a session and learner. A missing or
// invalid cookie is not an error here: routes that need a learner enforce it with
// requireAuth.
func (s *Server) loadSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := r.Cookie(s.cfg.SessionCookieName)
		if err != nil || c.Value == "" {
			next.ServeHTTP(w, r)
			return
		}
		sess, user, err := s.store.SessionByTokenHash(r.Context(), auth.TokenHash(c.Value))
		if err != nil {
			// An unknown/expired cookie is cleared so the browser stops sending it.
			s.clearSessionCookies(w)
			next.ServeHTTP(w, r)
			return
		}
		_ = s.store.TouchSession(r.Context(), sess.ID)

		ctx := context.WithValue(r.Context(), ctxKeySession, sess)
		ctx = context.WithValue(ctx, ctxKeyUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// requireAuth rejects anonymous requests on protected routes.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := userFrom(r.Context()); !ok {
			writeError(w, r, http.StatusUnauthorized, "unauthorized", "authentication required", nil)
			return
		}
		if !isSafeMethod(r.Method) {
			if !s.csrfOK(r) {
				writeError(w, r, http.StatusForbidden, "csrf_failed",
					"missing or invalid CSRF token", nil)
				return
			}
		}
		// A session must also have been established on an allowed origin: a
		// cross-site request carrying a valid cookie is rejected outright.
		if origin := r.Header.Get("Origin"); origin != "" && !s.originAllowed(origin) {
			writeError(w, r, http.StatusForbidden, "origin_rejected", "origin is not allowed", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) csrfOK(r *http.Request) bool {
	sess, ok := sessionFrom(r.Context())
	if !ok {
		return false
	}
	// Header only, deliberately. Accepting the readable cookie as a fallback would
	// turn this into a plain cookie-value check, which a cross-site attacker who
	// can set a cookie on the domain (cookie tossing) would satisfy. The SPA reads
	// the cookie and sends it as a header, which is the double-submit contract.
	got := r.Header.Get(s.cfg.CSRFHeaderName)
	return auth.CSRFOk(sess.CSRFToken, got)
}

func (s *Server) originAllowed(origin string) bool {
	if len(s.cfg.CORSAllowedOrigins) == 0 {
		// Same-origin deployments only: accept the configured public base.
		return origin == s.cfg.PublicBase
	}
	for _, o := range s.cfg.CORSAllowedOrigins {
		if o == origin {
			return true
		}
	}
	return false
}

// rateLimit applies a token bucket keyed by client IP.
func (s *Server) rateLimit(l *auth.Limiter, scope string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := scope + ":" + clientIP(r)
			if ok, retry := l.Allow(key); !ok {
				secs := int(retry.Seconds()) + 1
				w.Header().Set("Retry-After", fmt.Sprint(secs))
				writeError(w, r, http.StatusTooManyRequests, "rate_limited",
					fmt.Sprintf("too many requests, retry in %ds", secs), nil)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if i := strings.Index(xff, ","); i > 0 {
			return strings.TrimSpace(xff[:i])
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func isSafeMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// --- cookie helpers --------------------------------------------------------

func (s *Server) setSessionCookies(w http.ResponseWriter, token, csrfToken string, maxAge time.Duration) {
	secure := s.cfg.SessionCookieSecure
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.SessionCookieName,
		Value:    token,
		Path:     "/",
		Domain:   s.cfg.SessionCookieDomain,
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	// The CSRF token is deliberately readable by the SPA so it can echo it back in
	// the X-CSRF-Token header (double-submit). It is worthless without the session.
	http.SetCookie(w, &http.Cookie{
		Name:     s.cfg.CSRFCookieName,
		Value:    csrfToken,
		Path:     "/",
		Domain:   s.cfg.SessionCookieDomain,
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) clearSessionCookies(w http.ResponseWriter) {
	for _, name := range []string{s.cfg.SessionCookieName, s.cfg.CSRFCookieName} {
		http.SetCookie(w, &http.Cookie{
			Name:     name,
			Value:    "",
			Path:     "/",
			Domain:   s.cfg.SessionCookieDomain,
			MaxAge:   -1,
			HttpOnly: name == s.cfg.SessionCookieName,
			Secure:   s.cfg.SessionCookieSecure,
			SameSite: http.SameSiteLaxMode,
		})
	}
}
