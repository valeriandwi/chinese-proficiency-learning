package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/valeriandwipurnomo/hanpath/internal/auth"
	"github.com/valeriandwipurnomo/hanpath/internal/mail"
	"github.com/valeriandwipurnomo/hanpath/internal/progress"
	"github.com/valeriandwipurnomo/hanpath/internal/store"
)

// Version is stamped at build time with -ldflags.
var Version = "dev"

// dummyHash equalises login timing between "unknown email" and "wrong password",
// so response latency cannot be used to enumerate registered accounts.
var dummyHash = func() string {
	h, err := auth.HashPassword("hanpath-timing-equaliser-never-a-real-password")
	if err != nil {
		return ""
	}
	return h
}()

// --- registration and login ------------------------------------------------

type registerRequest struct {
	Email            string `json:"email"`
	DisplayName      string `json:"display_name"`
	Password         string `json:"password"`
	Timezone         string `json:"timezone"`
	DailyGoalMinutes int    `json:"daily_goal_minutes"`
}

type authResponse struct {
	User      *store.User `json:"user"`
	CSRFToken string      `json:"csrf_token"`
	Message   string      `json:"message,omitempty"`
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	req.DisplayName = strings.TrimSpace(req.DisplayName)

	details := validateRegister(req)
	if len(details) > 0 {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "the request has invalid fields", details)
		return
	}
	tz := req.Timezone
	if tz == "" {
		tz = "Asia/Jakarta"
	}
	goal := req.DailyGoalMinutes
	if goal <= 0 {
		goal = 20
	}

	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	user, err := s.store.CreateUser(r.Context(), req.Email, req.DisplayName, hash, tz)
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeError(w, r, http.StatusConflict, "email_taken", "that email address is already registered", nil)
			return
		}
		s.respondStoreError(w, r, err)
		return
	}
	if goal != 20 {
		if _, err := s.store.UpdateProfile(r.Context(), user.ID, user.DisplayName, user.Timezone,
			"minutes", goal, user.PinyinDefault, user.TargetCurriculum, user.TargetLevel); err == nil {
			user.DailyGoalValue = goal
		}
	}

	s.sendVerificationEmail(r.Context(), user)
	sess, err := s.issueSession(r, w, user)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	s.log.InfoContext(r.Context(), "user registered", "user_id", user.ID)
	writeJSON(w, http.StatusCreated, authResponse{
		User:      user,
		CSRFToken: sess.CSRFToken,
		Message:   "check your inbox to verify your email address",
	})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.Email = strings.TrimSpace(strings.ToLower(req.Email))
	if req.Email == "" || req.Password == "" {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "email and password are required", nil)
		return
	}

	user, err := s.store.UserByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			// Burn equivalent CPU so timing does not reveal whether the account exists.
			_, _ = auth.VerifyPassword(req.Password, dummyHash)
			writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect", nil)
			return
		}
		s.respondStoreError(w, r, err)
		return
	}
	ok, err := auth.VerifyPassword(req.Password, user.PasswordHash)
	if err != nil || !ok {
		writeError(w, r, http.StatusUnauthorized, "invalid_credentials", "email or password is incorrect", nil)
		return
	}
	sess, err := s.issueSession(r, w, user)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, authResponse{User: user, CSRFToken: sess.CSRFToken})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if sess, ok := sessionFrom(r.Context()); ok {
		if err := s.store.RevokeSession(r.Context(), sess.ID); err != nil {
			s.respondStoreError(w, r, err)
			return
		}
	}
	s.clearSessionCookies(w)
	w.WriteHeader(http.StatusNoContent)
}

// handleSessionInfo is the SPA's boot call: it reports whether a session exists
// and hands back the CSRF token to use for subsequent writes.
func (s *Server) handleSessionInfo(w http.ResponseWriter, r *http.Request) {
	user, ok := userFrom(r.Context())
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"authenticated": false})
		return
	}
	sess, _ := sessionFrom(r.Context())
	resp := map[string]any{"authenticated": true, "user": user}
	if sess != nil {
		resp["csrf_token"] = sess.CSRFToken
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) issueSession(r *http.Request, w http.ResponseWriter, user *store.User) (*store.Session, error) {
	token, hash, err := auth.NewSessionToken()
	if err != nil {
		return nil, err
	}
	csrf, err := auth.NewCSRFToken()
	if err != nil {
		return nil, err
	}
	sess, err := s.store.CreateSession(r.Context(), user.ID, hash, csrf,
		r.UserAgent(), clientIP(r), time.Now().Add(s.cfg.SessionTTL))
	if err != nil {
		return nil, err
	}
	s.setSessionCookies(w, token, csrf, s.cfg.SessionTTL)
	return sess, nil
}

func validateRegister(req registerRequest) []fieldError {
	var out []fieldError
	if req.Email == "" || !strings.Contains(req.Email, "@") || strings.Contains(req.Email, " ") {
		out = append(out, fieldError{Field: "email", Message: "must be a valid email address"})
	}
	if n := len([]rune(req.DisplayName)); n < 1 || n > 60 {
		out = append(out, fieldError{Field: "display_name", Message: "must be between 1 and 60 characters"})
	}
	for _, issue := range auth.PasswordIssues(req.Password) {
		out = append(out, fieldError{Field: "password", Message: issue})
	}
	if req.Timezone != "" {
		if _, err := time.LoadLocation(req.Timezone); err != nil {
			out = append(out, fieldError{Field: "timezone", Message: "must be a valid IANA timezone name"})
		}
	}
	if req.DailyGoalMinutes < 0 || req.DailyGoalMinutes > 600 {
		out = append(out, fieldError{Field: "daily_goal_minutes", Message: "must be between 0 and 600"})
	}
	return out
}

// --- email verification and password reset ---------------------------------

type tokenRequest struct {
	Token string `json:"token"`
}

func (s *Server) sendVerificationEmail(ctx context.Context, user *store.User) {
	token, err := auth.RandomToken(32)
	if err != nil {
		s.log.ErrorContext(ctx, "generating verification token", "error", err)
		return
	}
	if err := s.store.CreateAuthToken(ctx, user.ID, "email_verify", auth.TokenHash(token), time.Now().Add(auth.EmailVerificationTTL)); err != nil {
		s.log.ErrorContext(ctx, "storing verification token", "error", err)
		return
	}
	link := fmt.Sprintf("%s/verify-email?token=%s", s.cfg.PublicBase, token)
	if err := s.mail.Send(ctx, mail.Message{
		To:      user.Email,
		Subject: "Verify your hanpath email",
		Body:    verificationBody(user.DisplayName, link),
	}); err != nil {
		s.log.ErrorContext(ctx, "sending verification email", "error", err)
	}
}

func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	var req tokenRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	t, err := s.store.ConsumeAuthToken(r.Context(), "email_verify", auth.TokenHash(req.Token))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_token", "this verification link is invalid, expired, or already used", nil)
		return
	}
	if err := s.store.MarkEmailVerified(r.Context(), t.UserID); err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verified": true})
}

type passwordResetRequest struct {
	Email string `json:"email"`
}

// handlePasswordResetRequest always answers 202 regardless of whether the address
// is registered: a 404 here would be an account-enumeration oracle.
func (s *Server) handlePasswordResetRequest(w http.ResponseWriter, r *http.Request) {
	var req passwordResetRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if email != "" {
		if user, err := s.store.UserByEmail(r.Context(), email); err == nil {
			if err := s.store.InvalidateAuthTokens(r.Context(), user.ID, "password_reset"); err != nil {
				s.log.ErrorContext(r.Context(), "invalidating reset tokens", "error", err)
			}
			token, err := auth.RandomToken(32)
			if err == nil {
				if err := s.store.CreateAuthToken(r.Context(), user.ID, "password_reset", auth.TokenHash(token), time.Now().Add(auth.PasswordResetTTL)); err == nil {
					link := fmt.Sprintf("%s/reset-password?token=%s", s.cfg.PublicBase, token)
					if err := s.mail.Send(r.Context(), mail.Message{
						To:      user.Email,
						Subject: "Reset your hanpath password",
						Body:    resetBody(user.DisplayName, link),
					}); err != nil {
						s.log.ErrorContext(r.Context(), "sending reset email", "error", err)
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusAccepted, map[string]any{
		"message": "if that address is registered, a reset link is on its way",
	})
}

type passwordResetConfirm struct {
	Token    string `json:"token"`
	Password string `json:"password"`
}

func (s *Server) handlePasswordResetConfirm(w http.ResponseWriter, r *http.Request) {
	var req passwordResetConfirm
	if !decodeJSON(w, r, &req) {
		return
	}
	if issues := auth.PasswordIssues(req.Password); len(issues) > 0 {
		details := make([]fieldError, 0, len(issues))
		for _, i := range issues {
			details = append(details, fieldError{Field: "password", Message: i})
		}
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "the request has invalid fields", details)
		return
	}
	t, err := s.store.ConsumeAuthToken(r.Context(), "password_reset", auth.TokenHash(req.Token))
	if err != nil {
		writeError(w, r, http.StatusBadRequest, "invalid_token", "this reset link is invalid, expired, or already used", nil)
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	if err := s.store.UpdatePasswordHash(r.Context(), t.UserID, hash); err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	// Every other session is invalidated: a password reset must end existing logins.
	n, err := s.store.RevokeAllSessions(r.Context(), t.UserID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	s.log.InfoContext(r.Context(), "password reset completed", "user_id", t.UserID, "sessions_revoked", n)
	writeJSON(w, http.StatusOK, map[string]any{"reset": true, "sessions_revoked": n})
}

// --- profile ---------------------------------------------------------------

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	writeJSON(w, http.StatusOK, user)
}

type updateMeRequest struct {
	DisplayName      *string `json:"display_name"`
	Timezone         *string `json:"timezone"`
	DailyGoalKind    *string `json:"daily_goal_kind"`
	DailyGoalValue   *int    `json:"daily_goal_value"`
	PinyinDefault    *string `json:"pinyin_default"`
	TargetCurriculum *string `json:"target_curriculum"`
	TargetLevel      *int    `json:"target_level"`
}

func (s *Server) handleUpdateMe(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	var req updateMeRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	var details []fieldError
	displayName := user.DisplayName
	if req.DisplayName != nil {
		displayName = strings.TrimSpace(*req.DisplayName)
		if n := len([]rune(displayName)); n < 1 || n > 60 {
			details = append(details, fieldError{Field: "display_name", Message: "must be between 1 and 60 characters"})
		}
	}
	tz := user.Timezone
	if req.Timezone != nil {
		tz = *req.Timezone
		if _, err := time.LoadLocation(tz); err != nil {
			details = append(details, fieldError{Field: "timezone", Message: "must be a valid IANA timezone name"})
		}
	}
	goalKind := user.DailyGoalKind
	if req.DailyGoalKind != nil {
		goalKind = *req.DailyGoalKind
		switch goalKind {
		case "minutes", "xp", "lessons":
		default:
			details = append(details, fieldError{Field: "daily_goal_kind", Message: "must be minutes, xp or lessons"})
		}
	}
	goalValue := user.DailyGoalValue
	if req.DailyGoalValue != nil {
		goalValue = *req.DailyGoalValue
		if goalValue < 1 || goalValue > 600 {
			details = append(details, fieldError{Field: "daily_goal_value", Message: "must be between 1 and 600"})
		}
	}
	pinyin := user.PinyinDefault
	if req.PinyinDefault != nil {
		pinyin = *req.PinyinDefault
		switch pinyin {
		case "always", "on_tap", "never":
		default:
			details = append(details, fieldError{Field: "pinyin_default", Message: "must be always, on_tap or never"})
		}
	}
	targetCurriculum := user.TargetCurriculum
	if req.TargetCurriculum != nil {
		targetCurriculum = req.TargetCurriculum
		if _, err := s.store.CurriculumByVersion(r.Context(), *targetCurriculum); err != nil {
			details = append(details, fieldError{Field: "target_curriculum", Message: "unknown curriculum version"})
		}
	}
	targetLevel := user.TargetLevel
	if req.TargetLevel != nil {
		targetLevel = req.TargetLevel
		if *targetLevel < 1 || *targetLevel > 9 {
			details = append(details, fieldError{Field: "target_level", Message: "must be between 1 and 9"})
		}
	}
	if len(details) > 0 {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "the request has invalid fields", details)
		return
	}

	updated, err := s.store.UpdateProfile(r.Context(), user.ID, displayName, tz, goalKind, goalValue, pinyin, targetCurriculum, targetLevel)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	// A timezone or goal change invalidates every derived daily bucket, so
	// recompute immediately instead of letting the dashboard describe the old tz.
	if req.Timezone != nil || req.DailyGoalKind != nil || req.DailyGoalValue != nil {
		if err := s.store.RecomputeUserProgress(r.Context(), user.ID); err != nil {
			s.log.ErrorContext(r.Context(), "recomputing progress after profile change", "error", err)
		}
	}
	writeJSON(w, http.StatusOK, updated)
}

// --- placement -------------------------------------------------------------

func (s *Server) handlePlacementQuestions(w http.ResponseWriter, r *http.Request) {
	version := r.URL.Query().Get("version")
	if version == "" {
		version = s.defaultCurriculumVersion(r)
	}
	level := parseLimitQuery(r, "level", 1)
	limit := parseLimit(r, 20, 50)

	items, err := s.store.VocabularySample(r.Context(), version, level, limit)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"version":     version,
		"level":       level,
		"items":       items,
		"self_report": "This is a recognition self-check, not a scored test: answer honestly about which words you already know.",
	})
}

type placementSubmission struct {
	Version        string      `json:"version"`
	KnownLexemeIDs []uuid.UUID `json:"known_lexeme_ids"`
	Presented      int         `json:"presented"`
	LevelProbed    int         `json:"level_probed"`
}

func (s *Server) handleSubmitPlacement(w http.ResponseWriter, r *http.Request) {
	user, _ := userFrom(r.Context())
	var req placementSubmission
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Version == "" {
		req.Version = s.defaultCurriculumVersion(r)
	}
	c, err := s.store.CurriculumByVersion(r.Context(), req.Version)
	if err != nil {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "unknown curriculum version", nil)
		return
	}
	levels, err := s.store.Levels(r.Context(), c.ID)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	presented := req.Presented
	if presented <= 0 {
		presented = len(req.KnownLexemeIDs)
	}
	if presented == 0 {
		writeError(w, r, http.StatusUnprocessableEntity, "validation_failed", "presented must be greater than zero", nil)
		return
	}
	score := float64(len(req.KnownLexemeIDs)) / float64(presented)
	if score > 1 {
		score = 1
	}
	recommended := progress.PlacementLevel(score, len(levels))

	updated, err := s.store.CompletePlacement(r.Context(), user.ID, req.Version, recommended)
	if err != nil {
		s.respondStoreError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"user":        updated,
		"score":       score,
		"recommended": recommended,
		"note":        "placement is a heuristic recommendation you can override in your profile",
	})
}
