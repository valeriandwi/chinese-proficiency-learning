package auth

import (
	"crypto/sha256"
	"crypto/subtle"
	"errors"
	"fmt"
	"sync"
	"time"
)

// TokenHash is the value stored for a bearer credential. Plaintext tokens are
// never persisted, so a database leak does not hand over live sessions.
func TokenHash(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// CSRFOk compares a submitted CSRF token with the session's token in constant time.
func CSRFOk(expected, got string) bool {
	if expected == "" || got == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(expected), []byte(got)) == 1
}

// --- rate limiting ---------------------------------------------------------

// Limiter is an in-process token bucket, one bucket per key.
//
// Deliberate MVP trade-off: it is per-process, so two API instances would each
// allow the configured rate. Redis-backed limiting (already provisioned in
// compose) replaces this without changing call sites once the API is scaled out.
type Limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	rate    float64 // tokens per second
	burst   float64
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter builds a limiter allowing perMinute requests per minute with a burst
// of the same size.
func NewLimiter(perMinute int) *Limiter {
	if perMinute <= 0 {
		perMinute = 30
	}
	return &Limiter{
		buckets: map[string]*bucket{},
		rate:    float64(perMinute) / 60.0,
		burst:   float64(perMinute),
		now:     time.Now,
	}
}

// Allow consumes one token for key. When it returns false, retryAfter is how long
// the caller should wait before the next attempt will succeed.
func (l *Limiter) Allow(key string) (allowed bool, retryAfter time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	b, ok := l.buckets[key]
	if !ok {
		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}
	// Refill proportionally to elapsed time.
	elapsed := now.Sub(b.last).Seconds()
	b.tokens = min(l.burst, b.tokens+elapsed*l.rate)
	b.last = now

	if b.tokens < 1 {
		need := (1 - b.tokens) / l.rate
		return false, time.Duration(need * float64(time.Second))
	}
	b.tokens--
	// Opportunistic pruning keeps the map bounded without a background goroutine.
	if len(l.buckets) > 10000 {
		for k, v := range l.buckets {
			if now.Sub(v.last) > 10*time.Minute && v.tokens >= l.burst {
				delete(l.buckets, k)
			}
		}
	}
	return true, 0
}

// --- password reset / verification tokens ---------------------------------

// ErrTokenInvalid is returned for an unknown, expired or already-used token.
var ErrTokenInvalid = errors.New("auth: token is invalid, expired or already used")

// EmailVerificationTTL and PasswordResetTTL bound how long an emailed link works.
const (
	EmailVerificationTTL = 48 * time.Hour
	PasswordResetTTL     = 2 * time.Hour
)

// SessionTTLDefault is used when configuration does not specify one.
const SessionTTLDefault = 30 * 24 * time.Hour

// NewSessionToken returns a fresh opaque session token and its stored hash.
func NewSessionToken() (token string, hash []byte, err error) {
	token, err = RandomToken(32)
	if err != nil {
		return "", nil, fmt.Errorf("auth: generating session token: %w", err)
	}
	return token, TokenHash(token), nil
}

// NewCSRFToken returns a fresh double-submit CSRF token.
func NewCSRFToken() (string, error) { return RandomToken(32) }
