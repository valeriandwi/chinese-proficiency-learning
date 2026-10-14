// Package config loads and validates environment configuration.
//
// Fail-fast contract: Load returns an error rather than substituting a default
// for anything security-relevant (database URL, session cookie name, S3 keys).
package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv     string
	LogLevel   string
	HTTPAddr   string
	PublicBase string

	CORSAllowedOrigins []string

	DatabaseURL string
	RedisAddr   string

	SessionCookieName   string
	SessionCookieDomain string
	SessionCookieSecure bool
	SessionTTL          time.Duration

	CSRFCookieName string
	CSRFHeaderName string

	S3Endpoint     string
	S3Region       string
	S3AccessKey    string
	S3SecretKey    string
	S3Bucket       string
	S3UsePathStyle bool

	// Media assets (generated reference audio today). A local directory is the
	// default so the pipeline works with no cloud credentials; an S3-compatible
	// store is a new media.Store implementation, not a change to this struct's
	// consumers.
	MediaDir       string
	MediaURLPrefix string
	// TTSProvider selects the speech synthesiser ("say" is the macOS default and
	// needs no credentials; "none" disables audio generation, which makes any
	// listening import fail loudly rather than ship a silent lesson).
	TTSProvider string
	TTSVoice    string
	FFmpegPath  string

	SMTPAddr string
	SMTPFrom string

	// RateLimitBurstsPerMinute guards auth endpoints.
	RateLimitPerMinute int
	// MaxAttemptsPerRequest bounds a single batch attempt submission.
	MaxAttemptsPerRequest int
}

// Load reads .env when present (development convenience) and then the process
// environment, which always wins.
func Load() (*Config, error) {
	_ = godotenv.Load() // absent in production; never fatal

	c := &Config{
		AppEnv:                env("APP_ENV", "development"),
		LogLevel:              env("LOG_LEVEL", "info"),
		HTTPAddr:              env("HTTP_ADDR", "127.0.0.1:8081"),
		PublicBase:            env("PUBLIC_BASE_URL", "http://localhost:5174"),
		DatabaseURL:           os.Getenv("DATABASE_URL"),
		RedisAddr:             os.Getenv("REDIS_ADDR"),
		SessionCookieName:     env("SESSION_COOKIE_NAME", "hanpath_session"),
		SessionCookieDomain:   os.Getenv("SESSION_COOKIE_DOMAIN"),
		SessionCookieSecure:   envBool("SESSION_COOKIE_SECURE", false),
		SessionTTL:            time.Duration(envInt("SESSION_TTL_HOURS", 720)) * time.Hour,
		CSRFCookieName:        env("CSRF_COOKIE_NAME", "hanpath_csrf"),
		CSRFHeaderName:        env("CSRF_HEADER_NAME", "X-CSRF-Token"),
		S3Endpoint:            os.Getenv("S3_ENDPOINT"),
		S3Region:              env("S3_REGION", "us-east-1"),
		S3AccessKey:           os.Getenv("S3_ACCESS_KEY"),
		S3SecretKey:           os.Getenv("S3_SECRET_KEY"),
		S3Bucket:              os.Getenv("S3_BUCKET"),
		S3UsePathStyle:        envBool("S3_USE_PATH_STYLE", true),
		MediaDir:              env("MEDIA_DIR", "./var/media"),
		MediaURLPrefix:        env("MEDIA_URL_PREFIX", "/media"),
		TTSProvider:           os.Getenv("TTS_PROVIDER"),
		TTSVoice:              env("TTS_VOICE", "Tingting"),
		FFmpegPath:            env("FFMPEG_PATH", "ffmpeg"),
		SMTPAddr:              os.Getenv("SMTP_ADDR"),
		SMTPFrom:              env("SMTP_FROM", "hanpath <no-reply@hanpath.local>"),
		RateLimitPerMinute:    envInt("RATE_LIMIT_PER_MINUTE", 30),
		MaxAttemptsPerRequest: envInt("MAX_ATTEMPTS_PER_REQUEST", 100),
	}

	for _, o := range strings.Split(env("CORS_ALLOWED_ORIGINS", ""), ",") {
		if o = strings.TrimSpace(o); o != "" {
			c.CORSAllowedOrigins = append(c.CORSAllowedOrigins, o)
		}
	}

	if err := c.validate(); err != nil {
		return nil, err
	}
	return c, nil
}

func (c *Config) validate() error {
	var missing []string
	if c.DatabaseURL == "" {
		missing = append(missing, "DATABASE_URL")
	}
	if c.SessionCookieName == "" {
		missing = append(missing, "SESSION_COOKIE_NAME")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: missing required environment variables: %s", strings.Join(missing, ", "))
	}

	u, err := url.Parse(c.DatabaseURL)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("config: DATABASE_URL is not a valid URL")
	}
	if _, err := url.Parse(c.PublicBase); err != nil {
		return fmt.Errorf("config: PUBLIC_BASE_URL is not a valid URL")
	}
	// A Secure cookie over plain http can never be set by the browser, which then
	// presents as "login silently does nothing".
	if c.SessionCookieSecure && strings.HasPrefix(c.PublicBase, "http://") {
		return fmt.Errorf("config: SESSION_COOKIE_SECURE=true requires an https PUBLIC_BASE_URL (got %s)", c.PublicBase)
	}
	if c.AppEnv == "production" && !c.SessionCookieSecure {
		return fmt.Errorf("config: SESSION_COOKIE_SECURE must be true when APP_ENV=production")
	}
	if c.MaxAttemptsPerRequest <= 0 || c.MaxAttemptsPerRequest > 1000 {
		return fmt.Errorf("config: MAX_ATTEMPTS_PER_REQUEST must be between 1 and 1000")
	}
	return nil
}

// IsProduction reports whether the deployment is a production one.
func (c *Config) IsProduction() bool { return c.AppEnv == "production" }

func env(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
