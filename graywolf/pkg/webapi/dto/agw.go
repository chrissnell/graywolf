package dto

import "github.com/chrissnell/graywolf/pkg/configstore"

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
	return AgwResponse{
		ID: m.ID,
		AgwRequest: AgwRequest{
			ListenAddr: m.ListenAddr,
			Callsigns:  m.Callsigns,
			Enabled:    m.Enabled,
		},
	}
}
