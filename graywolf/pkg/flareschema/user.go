package flareschema

import "time"

// User holds the optional free-form fields the operator gave the CLI via
// flags. Every field is omitempty — a user submitting anonymously sends
// {} here.
type User struct {
	Email          string `json:"email,omitempty"`
	Notes          string `json:"notes,omitempty"`
	RadioModel     string `json:"radio_model,omitempty"`
	AudioInterface string `json:"audio_interface,omitempty"`
}

// Meta carries provenance for the submission: which graywolf and
// graywolf-modem build produced it, the schema version of the payload,
// the (irreversible, hashed) hostname, and the submission timestamp.
//
// Both the Go binary and the Rust binary carry their own version+commit
// because a "dev-install mismatch" — operator running a freshly built Go
// graywolf against a stale graywolf-modem — is a known support pattern
// that we want to spot in the UI without extra collector logic.
type Meta struct {
	SchemaVersion        int       `json:"schema_version"`
	GraywolfVersion      string    `json:"graywolf_version"`
	GraywolfCommit       string    `json:"graywolf_commit,omitempty"`
	GraywolfModemVersion string    `json:"graywolf_modem_version,omitempty"`
	GraywolfModemCommit  string    `json:"graywolf_modem_commit,omitempty"`
	HostnameHash         string    `json:"hostname_hash,omitempty"`
	SubmittedAt          time.Time `json:"submitted_at"`
}
