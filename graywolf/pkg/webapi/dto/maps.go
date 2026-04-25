package dto

import (
	"errors"
	"regexp"
	"strings"
	"time"
)

// MapsConfigRequest is the body for PUT /api/preferences/maps. Only
// Source is updatable from the client; Callsign and Token are managed
// by the /register sub-endpoint to keep the registration ceremony
// out of generic preference writes.
type MapsConfigRequest struct {
	Source string `json:"source"`
}

func (r MapsConfigRequest) Validate() error {
	if r.Source != "osm" && r.Source != "graywolf" {
		return errors.New("source must be 'osm' or 'graywolf'")
	}
	return nil
}

// MapsConfigResponse is what GET /api/preferences/maps and the PUT
// echo back. Token is omitted unless ?include_token=1 is set on the
// GET — see the handler. Registered is true iff a token is present.
//
// omitempty on RegisteredAt is a no-op for time.Time (struct value);
// the handler will populate it only when registered, so the zero-value
// case will still serialize. Acceptable here — the Registered bool is
// the source of truth for the UI.
type MapsConfigResponse struct {
	Source       string    `json:"source"`
	Callsign     string    `json:"callsign,omitempty"`
	Registered   bool      `json:"registered"`
	RegisteredAt time.Time `json:"registered_at"`
	Token        string    `json:"token,omitempty"`
}

// RegisterRequest is the body for POST /api/preferences/maps/register.
type RegisterRequest struct {
	Callsign string `json:"callsign"`
}

var callsignRE = regexp.MustCompile(`^[A-Z0-9]{3,9}$`)

// NormalizeCallsign uppercases, strips -SSID, and validates the format.
// Returns the cleaned callsign or an error matching the server's
// "must include at least one digit" rule. Used both by the client-side
// pre-flight (in JS, mirrored) and the backend handler.
func NormalizeCallsign(in string) (string, error) {
	s := strings.ToUpper(strings.TrimSpace(in))
	if i := strings.Index(s, "-"); i >= 0 {
		s = s[:i]
	}
	if !callsignRE.MatchString(s) {
		return "", errors.New("callsign must be 3-9 characters, letters and digits only")
	}
	hasDigit := false
	for _, r := range s {
		if r >= '0' && r <= '9' {
			hasDigit = true
			break
		}
	}
	if !hasDigit {
		return "", errors.New("callsign must contain at least one digit")
	}
	return s, nil
}

// RegisterResponse mirrors MapsConfigResponse — after a successful
// registration, the endpoint returns the same shape the GET would
// return next, including the freshly issued token (always, just this
// once, so the UI can offer the operator an export-token-to-file flow
// before it goes back to being suppressed).
type RegisterResponse = MapsConfigResponse
