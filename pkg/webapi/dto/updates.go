package dto

// UpdatesConfigRequest is the body accepted by PUT /api/updates/config.
// Enabled controls whether the daily GitHub update check runs at all.
// Disabling stops the ticker and causes GET /api/updates/status to
// report status="disabled".
type UpdatesConfigRequest struct {
	Enabled bool `json:"enabled"`
}

// Validate is a no-op. A single bool field has no input shape that
// can fail validation; the method exists for symmetry with the other
// singleton-config request DTOs that implement dto.Validator.
func (r UpdatesConfigRequest) Validate() error { return nil }

// UpdatesConfigResponse is the body returned by GET and PUT on
// /api/updates/config. Mirrors UpdatesConfigRequest — the stored state
// is a single bool.
type UpdatesConfigResponse struct {
	Enabled bool `json:"enabled"`
}

// UpdatesStatusResponse is the body returned by GET /api/updates/status.
// Status is a server-computed enum with exactly four values: "disabled",
// "pending", "current", "available" (D6). Latest / URL / CheckedAt are
// omitted from the JSON envelope when empty so a "disabled" or "pending"
// response collapses to a minimal shape.
type UpdatesStatusResponse struct {
	Status    string `json:"status"`
	Current   string `json:"current"`
	Latest    string `json:"latest,omitempty"`
	URL       string `json:"url,omitempty"`
	CheckedAt string `json:"checked_at,omitempty"` // RFC3339, omitted if zero
}
