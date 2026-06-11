package aprs

import "strings"

// IsGenericPathAlias reports whether a path callsign is a generic routing
// alias rather than the call of a real station that retransmitted the
// packet. The asterisk (H-bit) on these entries marks the alias as "used"
// by a digipeater, but the alias itself is not an additional hop — the
// digipeater that consumed it is listed separately (and counts on its own).
//
// Recognised aliases: WIDEn-N / WIDE, RELAY, TRACEn-N / TRACE, and the
// APRS-IS q-constructs (qAC, qAR, qAS, …). The argument may carry a
// trailing '*' and/or an SSID; both are tolerated.
func IsGenericPathAlias(call string) bool {
	upper := strings.ToUpper(strings.TrimSuffix(call, "*"))
	switch {
	case strings.HasPrefix(upper, "WIDE"),
		strings.HasPrefix(upper, "RELAY"),
		strings.HasPrefix(upper, "TRACE"),
		strings.HasPrefix(upper, "QA"):
		return true
	default:
		return false
	}
}

// CountHops returns the number of actual packet retransmissions represented
// by an APRS path, i.e. the count of real digipeaters that set the H-bit
// (trailing '*') on their entry. Generic routing aliases (WIDE, RELAY,
// TRACE, q-constructs) are excluded even when flagged used, because the
// used-alias entry rides alongside the digipeater that consumed it rather
// than representing a hop of its own.
//
// Example: SHEPRD*,WIDE1*,ELY*,WIDE2* counts as 2 hops (SHEPRD and ELY),
// not 4 — the WIDE1/WIDE2 aliases were consumed by those two digipeaters.
func CountHops(path []string) int {
	n := 0
	for _, p := range path {
		if !strings.HasSuffix(p, "*") {
			continue
		}
		if IsGenericPathAlias(p) {
			continue
		}
		n++
	}
	return n
}
