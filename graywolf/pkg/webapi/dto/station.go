package dto

// StationConfigRequest is the body accepted by PUT /api/station/config.
// An empty Callsign is permitted and triggers the clear-with-auto-disable
// flow defined in the centralized station-callsign plan (D7 rule 2):
// iGate and Digipeater Enabled are flipped to false atomically when
// they were previously true.
type StationConfigRequest struct {
	Callsign string `json:"callsign"`
}

// Validate is a no-op. Any non-empty callsign that fails shape
// validation is normalized (trim + uppercase) at the store boundary
// via configstore.UpsertStationConfig; completely invalid strings
// (internal whitespace etc.) are persisted verbatim and filtered at
// the resolve site. This mirrors other singleton request DTOs.
func (r StationConfigRequest) Validate() error { return nil }

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
	Disabled []string `json:"disabled,omitempty"`
}
