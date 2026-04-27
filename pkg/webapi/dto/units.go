package dto

import "errors"

// UnitsConfigRequest is the body accepted by PUT /api/preferences/units.
// System must be "imperial" or "metric".
type UnitsConfigRequest struct {
	System string `json:"system"`
}

// Validate rejects anything other than the two recognized systems so
// a bad payload doesn't reach the store (which would reject it anyway)
// and the client gets a clean 400 instead of a 500.
func (r UnitsConfigRequest) Validate() error {
	if r.System != "imperial" && r.System != "metric" {
		return errors.New("system must be 'imperial' or 'metric'")
	}
	return nil
}

// UnitsConfigResponse is the body returned by GET and PUT on
// /api/preferences/units.
type UnitsConfigResponse struct {
	System string `json:"system"`
}
