package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. 64 MiB / 3 iterations is the current OWASP-recommended
// baseline and is encoded into the hash string so parameters can be raised later
// without invalidating existing hashes.
const (
	argonMemoryKiB  = 64 * 1024
	argonIterations = 3
	argonParallel   = 2
	argonSaltLen    = 16
	argonKeyLen     = 32
)

var (
	// ErrInvalidHash means the stored hash is not in the expected PHC format.
	ErrInvalidHash = errors.New("auth: password hash is not in PHC format")
	// ErrIncompatibleVersion means the hash was produced by a different argon2 version.
	ErrIncompatibleVersion = errors.New("auth: incompatible argon2 version")
)

// HashPassword returns a PHC-formatted argon2id hash: $argon2id$v=19$m=...,t=...,p=...$salt$hash
func HashPassword(password string) (string, error) {
	if len(password) < 8 {
		return "", errors.New("auth: password must be at least 8 characters")
	}
	if len(password) > 1024 {
		return "", errors.New("auth: password must be at most 1024 characters")
	}
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generating salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonParallel, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonIterations, argonParallel,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword compares a candidate password against a PHC argon2id hash in
// constant time. It returns ErrInvalidHash for malformed input rather than
// treating it as a mismatch, so corruption is visible in logs.
func VerifyPassword(password, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, ErrInvalidHash
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, ErrInvalidHash
	}
	if version != argon2.Version {
		return false, ErrIncompatibleVersion
	}
	var memory uint32
	var iterations uint32
	var parallel uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallel); err != nil {
		return false, ErrInvalidHash
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, ErrInvalidHash
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, ErrInvalidHash
	}
	got := argon2.IDKey([]byte(password), salt, iterations, memory, parallel, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// PasswordIssues lists human-readable reasons a password is unacceptable.
// Kept intentionally small: length + a triviality check beats exotic composition
// rules that push users toward predictable substitutions.
func PasswordIssues(password string) []string {
	var issues []string
	if len([]rune(password)) < 8 {
		issues = append(issues, "must be at least 8 characters")
	}
	if len(password) > 1024 {
		issues = append(issues, "must be at most 1024 characters")
	}
	lower := strings.ToLower(password)
	for _, weak := range []string{"password", "12345678", "qwerty", "letmein", "hanpath"} {
		if strings.Contains(lower, weak) {
			issues = append(issues, "must not contain a common word or sequence")
			break
		}
	}
	return issues
}

// RandomToken returns a URL-safe random token and its byte length.
// The plaintext token is handed to the client once; only its hash is stored.
func RandomToken(bytes int) (string, error) {
	b := make([]byte, bytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// ParseIntOrZero is a small helper shared by handlers parsing path parameters.
func ParseIntOrZero(s string) int {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n
}
