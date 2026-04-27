package dto

import "github.com/chrissnell/graywolf/pkg/configstore"

// First-run defaults seeded into the response DTO when the source model
// field is the Go zero value. These mirror the gorm column defaults on
// configstore.AgwConfig so GET /api/agw on a fresh install returns a
// populated, UI-ready config. See IGate defaults for the "zero means
// unset" caveat.
const (
	DefaultAgwListenAddr = "0.0.0.0:8000"
	DefaultAgwCallsigns  = "N0CALL"
)

// AgwRequest is the body accepted by PUT /api/agw (singleton).
type AgwRequest struct {
	ListenAddr string `json:"listen_addr"`
	Callsigns  string `json:"callsigns"`
	Enabled    bool   `json:"enabled"`
}

func (r AgwRequest) Validate() error { return nil }

func (r AgwRequest) ToModel() configstore.AgwConfig {
	return configstore.AgwConfig{
		ListenAddr: r.ListenAddr,
		Callsigns:  r.Callsigns,
		Enabled:    r.Enabled,
	}
}

// AgwResponse is the body returned by GET/PUT for the singleton.
type AgwResponse struct {
	ID uint32 `json:"id"`
	AgwRequest
}

func AgwFromModel(m configstore.AgwConfig) AgwResponse {
	listenAddr := m.ListenAddr
	if listenAddr == "" {
		listenAddr = DefaultAgwListenAddr
	}
	callsigns := m.Callsigns
	if callsigns == "" {
		callsigns = DefaultAgwCallsigns
	}
	return AgwResponse{
		ID: m.ID,
		AgwRequest: AgwRequest{
			ListenAddr: listenAddr,
			Callsigns:  callsigns,
			Enabled:    m.Enabled,
		},
	}
}
