package dto

import (
	"fmt"
	"regexp"
	"strings"
)

// StationConfigRequest is the body accepted by PUT /api/station/config.
// An empty Callsign is permitted and triggers the clear-with-auto-disable
// flow defined in the centralized station-callsign plan (D7 rule 2):
// iGate and Digipeater Enabled are flipped to false atomically when
// they were previously true.
type StationConfigRequest struct {
	Callsign string `json:"callsign"`
	SSID     int    `json:"ssid"`
}

// Validate checks that Callsign is alphanumeric (no dash, max 6 chars)
// and SSID is 0–15. Empty Callsign is allowed (clear-callsign path).
func (r StationConfigRequest) Validate() error {
	if r.SSID < 0 || r.SSID > 15 {
		return fmt.Errorf("ssid %d is out of range: must be 0–15", r.SSID)
	}
	normalized := strings.ToUpper(strings.TrimSpace(r.Callsign))
	if normalized != "" && !stationCallsignRe.MatchString(normalized) {
		return fmt.Errorf("callsign %q is not valid: must be 1–6 alphanumeric characters with no SSID suffix", r.Callsign)
	}
	return nil
}

// stationCallsignRe matches a valid FCC amateur callsign base (no SSID).
var stationCallsignRe = regexp.MustCompile(`^[A-Z0-9]{1,6}$`)

// StationConfigResponse is the body returned by both GET and PUT on
// /api/station/config. Disabled is populated only on the PUT path
// when the clear-with-auto-disable rule fired; on GET it is omitted
// from the JSON envelope (omitempty).
//
// Disabled values are the canonical feature names "igate" and
// "digipeater" (lowercase, exactly those strings) — clients can match
// on them to surface a "these features were disabled because you
// cleared the station callsign" notice.
type StationConfigResponse struct {
	Callsign string   `json:"callsign"`
	SSID     int      `json:"ssid"`
	Disabled []string `json:"disabled,omitempty"`
}
