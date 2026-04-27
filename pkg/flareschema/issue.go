package flareschema

// CollectorIssue is appended to a section's Issues slice when a collector
// hits a recoverable problem (missing permission, absent dependency,
// unparseable system file). It is intentionally shape-stable across
// sections so the operator UI can render every section's issues with one
// component.
//
// Kind is a short snake_case tag (e.g. "permission_denied",
// "modem_unavailable", "parse_failed") that the operator UI groups by.
// Message is a free-text human-readable explanation. Path is the
// filesystem path or device the collector tripped over, when applicable.
type CollectorIssue struct {
	Kind    string `json:"kind"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}
