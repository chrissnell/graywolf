// Package webauth provides authentication primitives for the graywolf web UI:
// password hashing, session tokens, and HTTP middleware.
package webauth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

const (
	bcryptCost = 10
	tokenBytes = 32
	// sessionCookie is the name of the HTTP cookie that carries a
	// session token. Prefixed with "graywolf_" so graywolf running
	// behind a reverse proxy that multiplexes several apps on the
	// same origin does not collide with some other app's generic
	// "session" cookie.
	sessionCookie = "graywolf_session"
)

// HashPassword returns a bcrypt hash suitable for storage.
func HashPassword(password string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(h), nil
}

// CheckPassword compares a bcrypt hash with a plaintext password.
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

// GenerateSessionToken returns a cryptographically random 32-byte hex token.
func GenerateSessionToken() (string, error) {
	b := make([]byte, tokenBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b), nil
}
