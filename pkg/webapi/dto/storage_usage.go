package dto

// StorageUsageLocation is one on-disk location Graywolf writes to,
// with the path and the space it currently occupies.
type StorageUsageLocation struct {
	// Key is a stable identifier for the location: "maps", "history",
	// or "config". Clients key colors/labels off this, not off Label.
	Key string `json:"key"`
	// Label is the human-readable name shown in the UI.
	Label string `json:"label"`
	// Path is the absolute path on the server host. Informational —
	// shown to operators so they know where to look / back up.
	Path string `json:"path"`
	// Bytes is the total size in bytes. 0 when the path does not exist
	// yet (e.g. no offline maps downloaded, history logging disabled).
	Bytes int64 `json:"bytes"`
}

// StorageUsageResponse reports where Graywolf keeps its data and how
// much space each location uses. Returned by GET /api/storage/usage and
// rendered identically on every platform's web UI.
type StorageUsageResponse struct {
	Locations  []StorageUsageLocation `json:"locations"`
	TotalBytes int64                  `json:"total_bytes"`
}
