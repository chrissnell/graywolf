package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// PositionLogRequest is the body accepted by PUT /api/position-log.
type PositionLogRequest struct {
	Enabled bool   `json:"enabled"`
	DBPath  string `json:"db_path"`
}

func (r PositionLogRequest) Validate() error {
	if r.Enabled && r.DBPath == "" {
		return fmt.Errorf("db_path is required when enabled")
	}
	return nil
}

func (r PositionLogRequest) ToModel() configstore.PositionLogConfig {
	return configstore.PositionLogConfig{
		Enabled: r.Enabled,
		DBPath:  r.DBPath,
	}
}

// PositionLogResponse is the body returned by GET/PUT for the singleton.
type PositionLogResponse struct {
	ID      uint32 `json:"id"`
	Enabled bool   `json:"enabled"`
	DBPath  string `json:"db_path"`
}

func PositionLogFromModel(m configstore.PositionLogConfig) PositionLogResponse {
	return PositionLogResponse{
		ID:      m.ID,
		Enabled: m.Enabled,
		DBPath:  m.DBPath,
	}
}
