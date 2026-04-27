package callsign

import "strings"

// APRSPasscode computes the APRS-IS verification passcode for a callsign.
// The SSID is stripped and only the base call (uppercased, truncated to 10
// characters) is hashed. This is a direct port of the canonical JavaScript
// algorithm used across the APRS ecosystem.
//
// Malformed input yields the hash of the empty string (0x73e2 & 0x7fff =
// 0x73e2 = 29666). This behaviour is intentionally permissive; callers that
// need input validation should use Parse or Resolve first.
func APRSPasscode(callsign string) int {
	base, _, _ := Parse(callsign)
	real := strings.ToUpper(base)
	if len(real) > 10 {
		real = real[:10]
	}
	hash := 0x73e2
	for i := 0; i < len(real); i += 2 {
		hash ^= int(real[i]) << 8
		if i+1 < len(real) {
			hash ^= int(real[i+1])
		}
	}
	return hash & 0x7fff
}
