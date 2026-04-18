package dto

// HealthResponse is the body returned by GET /api/health. Used by
// orchestration (systemd, docker healthcheck) and the web UI header.
//
// Field order matches the alphabetical key order produced by the prior
// map[string]any encoding so the emitted JSON is byte-identical.
type HealthResponse struct {
	StartedAt string `json:"started_at"` // process start time, RFC3339
	Status    string `json:"status"`     // "ok"
	Time      string `json:"time"`       // current UTC time, RFC3339
}
