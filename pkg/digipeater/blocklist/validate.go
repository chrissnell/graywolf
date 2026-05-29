// Package blocklist implements pattern validation and matching for the
// digipeater's source-address deny list. The patterns mirror the igate
// filter's CALL / CALL-N / CALL-* convention but the matcher is scoped
// strictly to the digipeater engine — see docs/wiki/invariants.md for
// the no-sharing rule.
package blocklist

import (
	"fmt"
	"strconv"
	"strings"
)

// ValidatePattern parses and canonicalizes a block-list pattern. On
// success it returns the trimmed, uppercase canonical form so the
// stored Pattern and the matcher input agree byte-for-byte.
//
// Accepted forms:
//
//	CALL       — bare callsign (1..6 alphanumerics), matches SSID 0
//	CALL-N     — N in 0..15
//	CALL-*     — any non-zero SSID
//
// Rejected: empty/whitespace, "*", "-", "-*", missing call, oversized
// call, wildcard inside the callsign, SSID out of range, non-numeric
// SSID, any character AX.25 disallows in a callsign.
func ValidatePattern(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("pattern is empty")
	}
	s = strings.ToUpper(s)

	call := s
	suffix := ""
	if i := strings.IndexByte(s, '-'); i >= 0 {
		call = s[:i]
		suffix = s[i+1:]
	}

	if call == "" {
		return "", fmt.Errorf("callsign is empty")
	}
	if len(call) > 6 {
		return "", fmt.Errorf("callsign %q exceeds 6 characters", call)
	}
	for i := 0; i < len(call); i++ {
		c := call[i]
		if !((c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')) {
			return "", fmt.Errorf("invalid character %q in callsign", c)
		}
	}

	if strings.IndexByte(s, '-') < 0 {
		return call, nil
	}

	if suffix == "*" {
		return call + "-*", nil
	}
	if suffix == "" {
		return "", fmt.Errorf("missing SSID after '-'")
	}
	n, err := strconv.Atoi(suffix)
	if err != nil {
		return "", fmt.Errorf("invalid SSID %q", suffix)
	}
	if n < 0 || n > 15 {
		return "", fmt.Errorf("SSID %d out of range 0..15", n)
	}
	return fmt.Sprintf("%s-%d", call, n), nil
}
