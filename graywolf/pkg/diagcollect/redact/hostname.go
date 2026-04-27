package redact

import (
	"crypto/sha256"
	"encoding/hex"
)

// HashHostname returns the first 8 hex chars of sha256(name).
// An empty input returns an empty string — see TestHashHostname_EmptyInput
// for rationale.
//
// Stable across runs and across machines: the hash is purely a
// function of the input. The same physical host always produces the
// same 8-hex tag, so if a user submits multiple flares the operator UI
// can correlate them as "same machine" without ever seeing the real
// hostname.
func HashHostname(name string) string {
	if name == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(name))
	return hex.EncodeToString(sum[:])[:8]
}
