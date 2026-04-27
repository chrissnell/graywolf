package dto

// PositionLogRequest is the body accepted by PUT /api/position-log.
// The database path is controlled by the -history-db flag, not the API.
type PositionLogRequest struct {
	Enabled bool `json:"enabled"`
}

// PositionLogResponse is returned by GET/PUT for the singleton.
// DBPath is informational (read-only from the client's perspective).
type PositionLogResponse struct {
	Enabled bool   `json:"enabled"`
	DBPath  string `json:"db_path"`
}
