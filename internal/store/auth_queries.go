package store

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

const userColumns = `id, email, display_name, password_hash, role, email_verified_at, timezone,
	daily_goal_value, daily_goal_kind, pinyin_default, target_curriculum, target_level,
	placement_completed_at, created_at, updated_at`

func scanUser(row pgx.Row) (*User, error) {
	var u User
	err := row.Scan(&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.EmailVerifiedAt,
		&u.Timezone, &u.DailyGoalValue, &u.DailyGoalKind, &u.PinyinDefault, &u.TargetCurriculum,
		&u.TargetLevel, &u.PlacementCompletedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &u, nil
}

// CreateUser inserts a learner. Email is stored as given and matched
// case-insensitively by the functional unique index.
func (s *Store) CreateUser(ctx context.Context, email, displayName, passwordHash, timezone string) (*User, error) {
	row := s.Pool.QueryRow(ctx, `
		INSERT INTO users (id, email, display_name, password_hash, timezone)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING `+userColumns,
		uuid.New(), email, displayName, passwordHash, timezone)
	return scanUser(row)
}

// UserByEmail looks up a learner case-insensitively.
func (s *Store) UserByEmail(ctx context.Context, email string) (*User, error) {
	row := s.Pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE lower(email) = lower($1)`, email)
	return scanUser(row)
}

// UserByID loads one learner.
func (s *Store) UserByID(ctx context.Context, id uuid.UUID) (*User, error) {
	row := s.Pool.QueryRow(ctx, `SELECT `+userColumns+` FROM users WHERE id = $1`, id)
	return scanUser(row)
}

// UpdateProfile writes the learner-owned preference fields only. Role and
// verification state are deliberately not writable from here.
func (s *Store) UpdateProfile(ctx context.Context, id uuid.UUID, displayName, timezone, goalKind string, goalValue int, pinyinDefault string, targetCurriculum *string, targetLevel *int) (*User, error) {
	row := s.Pool.QueryRow(ctx, `
		UPDATE users SET
			display_name = $2, timezone = $3, daily_goal_kind = $4, daily_goal_value = $5,
			pinyin_default = $6, target_curriculum = $7, target_level = $8, updated_at = now()
		WHERE id = $1
		RETURNING `+userColumns,
		id, displayName, timezone, goalKind, goalValue, pinyinDefault, targetCurriculum, targetLevel)
	return scanUser(row)
}

// CompletePlacement records placement results.
func (s *Store) CompletePlacement(ctx context.Context, id uuid.UUID, curriculumVersion string, level int) (*User, error) {
	row := s.Pool.QueryRow(ctx, `
		UPDATE users SET target_curriculum = $2, target_level = $3,
			placement_completed_at = now(), updated_at = now()
		WHERE id = $1
		RETURNING `+userColumns, id, curriculumVersion, level)
	return scanUser(row)
}

// MarkEmailVerified stamps verification.
func (s *Store) MarkEmailVerified(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET email_verified_at = now(), updated_at = now() WHERE id = $1 AND email_verified_at IS NULL`, id)
	return mapErr(err)
}

// UpdatePasswordHash replaces the password hash.
func (s *Store) UpdatePasswordHash(ctx context.Context, id uuid.UUID, hash string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE users SET password_hash = $2, updated_at = now() WHERE id = $1`, id, hash)
	return mapErr(err)
}

// --- sessions --------------------------------------------------------------

// CreateSession stores a session row keyed by sha256(token).
func (s *Store) CreateSession(ctx context.Context, userID uuid.UUID, tokenHash []byte, csrfToken, userAgent, ip string, expiresAt time.Time) (*Session, error) {
	var sess Session
	var ipVal any
	if ip != "" {
		ipVal = ip
	}
	err := s.Pool.QueryRow(ctx, `
		INSERT INTO sessions (id, user_id, token_hash, csrf_token, user_agent, ip, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, user_id, csrf_token, created_at, last_seen_at, expires_at, revoked_at`,
		uuid.New(), userID, tokenHash, csrfToken, userAgent, ipVal, expiresAt).
		Scan(&sess.ID, &sess.UserID, &sess.CSRFToken, &sess.CreatedAt, &sess.LastSeenAt, &sess.ExpiresAt, &sess.RevokedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &sess, nil
}

// SessionByTokenHash resolves an active, unexpired session and its learner.
func (s *Store) SessionByTokenHash(ctx context.Context, tokenHash []byte) (*Session, *User, error) {
	row := s.Pool.QueryRow(ctx, `
		SELECT s.id, s.user_id, s.csrf_token, s.created_at, s.last_seen_at, s.expires_at, s.revoked_at,
		       `+prefixed("u", userColumns)+`
		FROM sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL AND s.expires_at > now()`, tokenHash)

	var sess Session
	var u User
	err := row.Scan(&sess.ID, &sess.UserID, &sess.CSRFToken, &sess.CreatedAt, &sess.LastSeenAt,
		&sess.ExpiresAt, &sess.RevokedAt,
		&u.ID, &u.Email, &u.DisplayName, &u.PasswordHash, &u.Role, &u.EmailVerifiedAt, &u.Timezone,
		&u.DailyGoalValue, &u.DailyGoalKind, &u.PinyinDefault, &u.TargetCurriculum, &u.TargetLevel,
		&u.PlacementCompletedAt, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, nil, mapErr(err)
	}
	return &sess, &u, nil
}

// TouchSession refreshes last_seen_at at most once a minute to avoid a write per request.
func (s *Store) TouchSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `
		UPDATE sessions SET last_seen_at = now()
		WHERE id = $1 AND last_seen_at < now() - interval '1 minute'`, id)
	return mapErr(err)
}

// RevokeSession logs one session out.
func (s *Store) RevokeSession(ctx context.Context, id uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE id = $1 AND revoked_at IS NULL`, id)
	return mapErr(err)
}

// RevokeAllSessions logs the learner out everywhere (used after a password reset).
func (s *Store) RevokeAllSessions(ctx context.Context, userID uuid.UUID) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `UPDATE sessions SET revoked_at = now() WHERE user_id = $1 AND revoked_at IS NULL`, userID)
	if err != nil {
		return 0, mapErr(err)
	}
	return tag.RowsAffected(), nil
}

// DeleteExpiredSessions prunes finished sessions; called by the worker.
func (s *Store) DeleteExpiredSessions(ctx context.Context) (int64, error) {
	tag, err := s.Pool.Exec(ctx, `DELETE FROM sessions WHERE expires_at < now() - interval '7 days'`)
	if err != nil {
		return 0, mapErr(err)
	}
	return tag.RowsAffected(), nil
}

// --- single-use auth tokens ------------------------------------------------

// CreateAuthToken stores a hashed single-use token.
func (s *Store) CreateAuthToken(ctx context.Context, userID uuid.UUID, kind string, tokenHash []byte, expiresAt time.Time) error {
	_, err := s.Pool.Exec(ctx, `
		INSERT INTO auth_tokens (id, user_id, kind, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, uuid.New(), userID, kind, tokenHash, expiresAt)
	return mapErr(err)
}

// ConsumeAuthToken marks a token used and returns it. The UPDATE ... WHERE used_at
// IS NULL makes consumption atomic, so two concurrent requests cannot both
// succeed with the same token.
func (s *Store) ConsumeAuthToken(ctx context.Context, kind string, tokenHash []byte) (*AuthToken, error) {
	var t AuthToken
	err := s.Pool.QueryRow(ctx, `
		UPDATE auth_tokens SET used_at = now()
		WHERE kind = $1 AND token_hash = $2 AND used_at IS NULL AND expires_at > now()
		RETURNING id, user_id, kind, expires_at, used_at`, kind, tokenHash).
		Scan(&t.ID, &t.UserID, &t.Kind, &t.ExpiresAt, &t.UsedAt)
	if err != nil {
		return nil, mapErr(err)
	}
	return &t, nil
}

// InvalidateAuthTokens voids outstanding tokens of a kind.
func (s *Store) InvalidateAuthTokens(ctx context.Context, userID uuid.UUID, kind string) error {
	_, err := s.Pool.Exec(ctx, `UPDATE auth_tokens SET used_at = now() WHERE user_id = $1 AND kind = $2 AND used_at IS NULL`, userID, kind)
	return mapErr(err)
}

func prefixed(alias, cols string) string {
	parts := strings.Split(cols, ",")
	for i, p := range parts {
		parts[i] = alias + "." + strings.TrimSpace(p)
	}
	return strings.Join(parts, ", ")
}

var _ = fmt.Sprintf
