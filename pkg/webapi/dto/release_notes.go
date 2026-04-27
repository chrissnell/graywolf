package dto

// ReleaseNotesResponse is the envelope returned by
// GET /api/release-notes and GET /api/release-notes/unseen.
//
// SchemaVersion represents the response-envelope schema. Clients that
// know about envelope version N can safely ignore notes whose per-note
// schema_version exceeds their own MAX_SCHEMA (forward-compat; see
// plan D9).
type ReleaseNotesResponse struct {
	SchemaVersion int    `json:"schema_version"`
	Current       string `json:"current"`
	// LastSeen is the authenticated caller's last acknowledged release
	// version at the moment the request was served. Empty on the
	// /api/release-notes endpoint (caller-agnostic) and on /unseen for
	// a user who has never acked. The frontend uses this to render a
	// "Since your last visit · vA → vB" subtitle in the news popup.
	LastSeen string           `json:"last_seen,omitempty"`
	Notes    []ReleaseNoteDTO `json:"notes"`
}

// ReleaseNoteDTO is a single release-note entry. Body carries
// server-sanitized, server-rendered HTML — the frontend binds it via
// {@html ...} untouched.
type ReleaseNoteDTO struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Date          string `json:"date"`  // ISO YYYY-MM-DD
	Style         string `json:"style"` // "info" | "cta"
	Title         string `json:"title"`
	Body          string `json:"body"` // pre-sanitized HTML
}
