package dto

import (
	"fmt"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// First-run defaults seeded into the response DTO when the source model
// field is the Go zero value. These mirror the gorm column defaults on
// configstore.IGateConfig so GET /api/igate/config on a fresh install
// (empty store) returns a populated, UI-ready config. Users who
// explicitly save these fields as zero will see them overwritten on the
// next GET; this is acceptable for singleton config where "no row yet"
// and "saved as zero" are not meaningfully distinguishable anyway.
const (
	DefaultIGateServer          = "rotate.aprs2.net"
	DefaultIGatePort            = 14580
	DefaultIGateRfChannel       = 1
	DefaultIGateMaxMsgHops      = 2
	DefaultIGateSoftwareName    = "graywolf"
	DefaultIGateSoftwareVersion = "0.1"
	DefaultIGateTxChannel       = 1
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
	server := m.Server
	if server == "" {
		server = DefaultIGateServer
	}
	port := m.Port
	if port == 0 {
		port = DefaultIGatePort
	}
	rfChannel := m.RfChannel
	if rfChannel == 0 {
		rfChannel = DefaultIGateRfChannel
	}
	maxMsgHops := m.MaxMsgHops
	if maxMsgHops == 0 {
		maxMsgHops = DefaultIGateMaxMsgHops
	}
	softwareName := m.SoftwareName
	if softwareName == "" {
		softwareName = DefaultIGateSoftwareName
	}
	softwareVersion := m.SoftwareVersion
	if softwareVersion == "" {
		softwareVersion = DefaultIGateSoftwareVersion
	}
	txChannel := m.TxChannel
	if txChannel == 0 {
		txChannel = DefaultIGateTxChannel
	}
	return IGateConfigResponse{
		ID: m.ID,
		IGateConfigRequest: IGateConfigRequest{
			Enabled:         m.Enabled,
			Server:          server,
			Port:            port,
			Callsign:        m.Callsign,
			Passcode:        m.Passcode,
			ServerFilter:    m.ServerFilter,
			SimulationMode:  m.SimulationMode,
			GateRfToIs:      m.GateRfToIs,
			GateIsToRf:      m.GateIsToRf,
			RfChannel:       rfChannel,
			MaxMsgHops:      maxMsgHops,
			SoftwareName:    softwareName,
			SoftwareVersion: softwareVersion,
			TxChannel:       txChannel,
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
	if err := validateIGateRfFilterPattern(r.Type, r.Pattern); err != nil {
		return err
	}
	return nil
}

// validateIGateRfFilterPattern enforces the wildcard-safety rules shared
// by POST and PUT on /api/igate/filters. The three rules derive directly
// from the engine semantics in pkg/igate/filters: `*` is only meaningful
// as a trailing suffix on TypeMessageDest / TypeObject patterns, a
// pattern that trims to "" or "*" never matches at the engine level
// (flooding guard), and `*` in a TypeCallsign / TypePrefix pattern would
// silently never match. Rejecting these at save time keeps the UI and
// the engine from disagreeing about what a rule "means".
//
// Whitespace semantics: only leading/trailing whitespace is trimmed for
// the empty/bare-wildcard check (matching matchPattern in the engine,
// which uses strings.TrimSpace). Internal whitespace is preserved —
// patterns like "FOO BAR" or "FOO *" are evaluated as-is, which means
// an internal `*` in "FOO *" is rejected by the trailing-only rule.
//
// Client-side mirror: web/src/routes/Igate.svelte validatePattern()
// enforces the same rules in the same order so the UI surfaces an
// error before Save. Client copy uses UI-idiomatic sentence case; the
// error values here use idiomatic Go (lowercase, no period) per
// staticcheck ST1005. Keep the rule set and check order in sync.
func validateIGateRfFilterPattern(ruleType, pattern string) error {
	trimmed := strings.TrimSpace(pattern)
	if trimmed == "" || trimmed == "*" {
		return fmt.Errorf("pattern must not be empty or a bare wildcard")
	}
	// Callsign / Prefix: `*` has no wildcard meaning in the engine for
	// these types, so a literal `*` would silently never match real
	// APRS strings. Reject before persistence.
	if ruleType == filtersTypeCallsign || ruleType == filtersTypePrefix {
		if strings.Contains(trimmed, "*") {
			return fmt.Errorf("`*` wildcard is only supported for message_dest and object types")
		}
	}
	// Any type: if `*` appears, it must be the final character of the
	// (trimmed) pattern. Leading / mid-string `*` are silent no-ops in
	// the engine (literal-match against APRS strings that never contain
	// `*`), so reject to avoid a footgun. Trimming before the position
	// check mirrors the engine's strings.TrimSpace on the pattern.
	if idx := strings.Index(trimmed, "*"); idx != -1 && idx != len(trimmed)-1 {
		return fmt.Errorf("`*` is only supported as a trailing wildcard")
	}
	return nil
}

// filtersType* mirror the string values of pkg/igate/filters RuleType
// constants. Duplicated here rather than imported so the DTO layer
// doesn't pull the filter engine (and its aprs dependency) into every
// handler build. The values must stay in sync with
// pkg/igate/filters/filters.go:17.
const (
	filtersTypeCallsign = "callsign"
	filtersTypePrefix   = "prefix"
)

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
