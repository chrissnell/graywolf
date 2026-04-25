package dto

import "time"

// DownloadStatus is the response shape for /api/maps/downloads endpoints.
// State is one of: "absent" | "pending" | "downloading" | "complete" | "error".
//
// DownloadedAt has no `omitempty` — Go's encoding/json silently treats
// `omitempty` on a struct value as a no-op (only the empty interface,
// nil pointer, etc. trigger omission), so the field always serializes.
// A zero-value timestamp on the wire signals "not complete yet";
// clients must use State, not the timestamp, to decide whether the
// download finished.
type DownloadStatus struct {
	Slug            string    `json:"slug"`
	State           string    `json:"state"`
	BytesTotal      int64     `json:"bytes_total"`
	BytesDownloaded int64     `json:"bytes_downloaded"`
	DownloadedAt    time.Time `json:"downloaded_at"`
	ErrorMessage    string    `json:"error_message,omitempty"`
}
