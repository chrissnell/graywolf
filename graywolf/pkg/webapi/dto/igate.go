package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// IGateConfigRequest is the body accepted by PUT /api/igate/config.
type IGateConfigRequest struct {
	Enabled         bool   `json:"enabled"`
	Server          string `json:"server"`
	Port            uint32 `json:"port"`
	Callsign        string `json:"callsign"`
	Passcode        string `json:"passcode"`
	ServerFilter    string `json:"server_filter"`
	SimulationMode  bool   `json:"simulation_mode"`
	GateRfToIs      bool   `json:"gate_rf_to_is"`
	GateIsToRf      bool   `json:"gate_is_to_rf"`
	RfChannel       uint32 `json:"rf_channel"`
	MaxMsgHops      uint32 `json:"max_msg_hops"`
	SoftwareName    string `json:"software_name"`
	SoftwareVersion string `json:"software_version"`
	TxChannel       uint32 `json:"tx_channel"`
}

func (r IGateConfigRequest) Validate() error { return nil }

func (r IGateConfigRequest) ToModel() configstore.IGateConfig {
	return configstore.IGateConfig{
		Enabled:         r.Enabled,
		Server:          r.Server,
		Port:            r.Port,
		Callsign:        r.Callsign,
		Passcode:        r.Passcode,
		ServerFilter:    r.ServerFilter,
		SimulationMode:  r.SimulationMode,
		GateRfToIs:      r.GateRfToIs,
		GateIsToRf:      r.GateIsToRf,
		RfChannel:       r.RfChannel,
		MaxMsgHops:      r.MaxMsgHops,
		SoftwareName:    r.SoftwareName,
		SoftwareVersion: r.SoftwareVersion,
		TxChannel:       r.TxChannel,
	}
}

type IGateConfigResponse struct {
	ID uint32 `json:"id"`
	IGateConfigRequest
}

func IGateConfigFromModel(m configstore.IGateConfig) IGateConfigResponse {
	return IGateConfigResponse{
		ID: m.ID,
		IGateConfigRequest: IGateConfigRequest{
			Enabled:         m.Enabled,
			Server:          m.Server,
			Port:            m.Port,
			Callsign:        m.Callsign,
			Passcode:        m.Passcode,
			ServerFilter:    m.ServerFilter,
			SimulationMode:  m.SimulationMode,
			GateRfToIs:      m.GateRfToIs,
			GateIsToRf:      m.GateIsToRf,
			RfChannel:       m.RfChannel,
			MaxMsgHops:      m.MaxMsgHops,
			SoftwareName:    m.SoftwareName,
			SoftwareVersion: m.SoftwareVersion,
			TxChannel:       m.TxChannel,
		},
	}
}

// IGateRfFilterRequest is the body accepted by POST /api/igate/filters
// and PUT /api/igate/filters/{id}.
type IGateRfFilterRequest struct {
	Channel  uint32 `json:"channel"`
	Type     string `json:"type"`
	Pattern  string `json:"pattern"`
	Action   string `json:"action"`
	Priority uint32 `json:"priority"`
	Enabled  bool   `json:"enabled"`
}

func (r IGateRfFilterRequest) Validate() error {
	if r.Type == "" {
		return fmt.Errorf("type is required")
	}
	if r.Pattern == "" {
		return fmt.Errorf("pattern is required")
	}
	return nil
}

func (r IGateRfFilterRequest) ToModel() configstore.IGateRfFilter {
	return configstore.IGateRfFilter{
		Channel:  r.Channel,
		Type:     r.Type,
		Pattern:  r.Pattern,
		Action:   r.Action,
		Priority: r.Priority,
		Enabled:  r.Enabled,
	}
}

func (r IGateRfFilterRequest) ToUpdate(id uint32) configstore.IGateRfFilter {
	m := r.ToModel()
	m.ID = id
	return m
}

type IGateRfFilterResponse struct {
	ID uint32 `json:"id"`
	IGateRfFilterRequest
}

func IGateRfFilterFromModel(m configstore.IGateRfFilter) IGateRfFilterResponse {
	return IGateRfFilterResponse{
		ID: m.ID,
		IGateRfFilterRequest: IGateRfFilterRequest{
			Channel:  m.Channel,
			Type:     m.Type,
			Pattern:  m.Pattern,
			Action:   m.Action,
			Priority: m.Priority,
			Enabled:  m.Enabled,
		},
	}
}

func IGateRfFiltersFromModels(ms []configstore.IGateRfFilter) []IGateRfFilterResponse {
	out := make([]IGateRfFilterResponse, len(ms))
	for i, m := range ms {
		out[i] = IGateRfFilterFromModel(m)
	}
	return out
}
