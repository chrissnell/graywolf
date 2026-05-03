package remoteactions

import (
	"encoding/base32"
	"errors"
	"fmt"
	"strings"

	"github.com/chrissnell/graywolf/pkg/actions"
)

// MaxActionNameLen mirrors the inbound parser's hard limit.
const MaxActionNameLen = actions.MaxActionNameLen

// MaxLabelLen is the macro tile label cap. Roughly fits a one-line
// drawer button on mobile.
const MaxLabelLen = 64

// MaxArgsStringLen is a generous cap on the args portion of a macro.
// The wire-format budget check happens at fire time; this is a
// belt-and-suspenders ceiling on what the DB will store.
const MaxArgsStringLen = 200

// ValidateActionName accepts a name that the inbound parser would
// also accept. Empty string is rejected.
func ValidateActionName(s string) error {
	if s == "" {
		return errors.New("action name required")
	}
	if len(s) > MaxActionNameLen {
		return fmt.Errorf("action name exceeds %d chars", MaxActionNameLen)
	}
	if !actions.ValidActionName(s) {
		return errors.New("action name has invalid characters")
	}
	return nil
}

// ValidateBase32Secret reports whether s decodes as a base32 TOTP
// secret. Whitespace and padding are tolerated; case is normalized.
// Length must be at least 16 chars after normalization (covers all
// RFC 6238 secret lengths used in practice — 80 bits = 16 base32 chars
// for SHA1; longer for SHA256/SHA512).
func ValidateBase32Secret(s string) error {
	norm, err := NormalizeBase32Secret(s)
	if err != nil {
		return err
	}
	if len(norm) < 16 {
		return errors.New("secret too short")
	}
	return nil
}

// NormalizeBase32Secret strips whitespace, uppercases, and validates
// that the result decodes as base32 (with or without padding). Returns
// the uppercased no-whitespace form on success — this is the form
// stored in the SecretB32 column.
func NormalizeBase32Secret(s string) (string, error) {
	clean := strings.ToUpper(strings.ReplaceAll(s, " ", ""))
	clean = strings.TrimRight(clean, "=")
	if clean == "" {
		return "", errors.New("secret required")
	}
	// Re-pad to a multiple of 8 for decode validation.
	padded := clean
	if pad := (8 - len(clean)%8) % 8; pad > 0 {
		padded = clean + strings.Repeat("=", pad)
	}
	if _, err := base32.StdEncoding.DecodeString(padded); err != nil {
		return "", fmt.Errorf("invalid base32: %w", err)
	}
	return clean, nil
}

// NormalizeTargetCall uppercases and validates a target callsign.
// Accepts the same shape as APRS addressees: 1..6 alphanumeric base
// call, optional `-` SSID of 1..2 chars. Total max 9 chars (callsign-
// SSID with separator).
func NormalizeTargetCall(s string) (string, error) {
	clean := strings.ToUpper(strings.TrimSpace(s))
	if clean == "" {
		return "", errors.New("target callsign required")
	}
	if len(clean) > 9 {
		return "", errors.New("target callsign too long")
	}
	base, ssid, hasSSID := strings.Cut(clean, "-")
	if base == "" || len(base) > 6 {
		return "", errors.New("base callsign 1..6 chars")
	}
	if !isAlnumASCII(base) {
		return "", errors.New("base callsign letters/digits only")
	}
	if hasSSID {
		if len(ssid) < 1 || len(ssid) > 2 || !isAlnumASCII(ssid) {
			return "", errors.New("ssid 1..2 alphanumeric")
		}
	}
	return clean, nil
}

func isAlnumASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z':
		case c >= 'a' && c <= 'z':
		case c >= '0' && c <= '9':
		default:
			return false
		}
	}
	return true
}
