// Package callsign provides shared parsing, N0CALL detection, resolution, and
// APRS-IS passcode computation for amateur-radio callsigns. It is a leaf
// package: it imports nothing from the rest of graywolf.
package callsign

import (
	"errors"
	"strings"
)

// Sentinel errors returned by Resolve. Callers can branch on these via
// errors.Is to distinguish the two failure modes.
var (
	// ErrCallsignEmpty is returned by Resolve when neither the override
	// nor the station callsign supplies a non-empty value.
	ErrCallsignEmpty = errors.New("callsign: empty")

	// ErrCallsignN0Call is returned by Resolve when the chosen value is
	// N0CALL (case-insensitive, SSID-agnostic). N0CALL is the default
	// "unconfigured" placeholder and must never reach the air.
	ErrCallsignN0Call = errors.New("callsign: N0CALL")
)

// Parse splits a callsign string of the form "CALL" or "CALL-SSID" into its
// base and SSID components. The input is trimmed of leading and trailing
// whitespace before parsing; the returned base preserves the original case
// (uppercasing is the caller's responsibility — see Resolve).
//
// ok is false for obviously malformed input:
//   - empty or whitespace-only
//   - leading dash ("-9")
//   - trailing dash ("KE7XYZ-")
//   - multiple dashes ("K-E-7")
//   - internal whitespace ("KE7 XYZ")
//
// The SSID is returned as a string, not an int, because SSIDs like "0",
// "15", and "16" all appear in real configs and numeric validity is a
// display/validation concern, not a parsing one.
func Parse(s string) (base, ssid string, ok bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", "", false
	}
	// Reject any internal whitespace.
	if strings.ContainsAny(s, " \t\r\n") {
		return "", "", false
	}
	// No dash: whole string is the base.
	base, ssid, hasDash := strings.Cut(s, "-")
	if !hasDash {
		return base, "", true
	}
	// Reject multiple dashes (Cut stopped at the first).
	if strings.Contains(ssid, "-") {
		return "", "", false
	}
	// Reject leading or trailing dash.
	if base == "" || ssid == "" {
		return "", "", false
	}
	return base, ssid, true
}

// IsN0Call reports whether s refers to N0CALL, the default "unconfigured"
// placeholder callsign. Matching is case-insensitive and SSID-agnostic, so
// "N0CALL", "n0call", "N0CALL-7", and "  n0call  " all return true. An
// empty string or otherwise malformed input returns false; callers should
// do a separate empty-check when they care about that case.
func IsN0Call(s string) bool {
	base, _, ok := Parse(s)
	if !ok {
		return false
	}
	return strings.EqualFold(base, "N0CALL")
}

// Resolve returns a usable station callsign or an error.
//
// Selection rule:
//   - a non-empty (post-trim) override wins
//   - otherwise station is used
//
// If the chosen value is empty, ErrCallsignEmpty is returned. If the chosen
// value is N0CALL (case-insensitive, SSID-agnostic), ErrCallsignN0Call is
// returned. On success, the value is returned trimmed and uppercased.
//
// Callers can use errors.Is(err, ErrCallsignEmpty) or
// errors.Is(err, ErrCallsignN0Call) to distinguish the two failure modes.
func Resolve(override, station string) (string, error) {
	chosen := strings.TrimSpace(override)
	if chosen == "" {
		chosen = strings.TrimSpace(station)
	}
	if chosen == "" {
		return "", ErrCallsignEmpty
	}
	if IsN0Call(chosen) {
		return "", ErrCallsignN0Call
	}
	return strings.ToUpper(chosen), nil
}
