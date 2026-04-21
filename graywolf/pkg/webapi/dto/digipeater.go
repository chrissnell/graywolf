package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// DigipeaterConfigRequest is the body accepted by PUT /api/digipeater.
//
// MyCall is a per-station callsign override (see centralized
// station-callsign plan, D2/D3). The request DTO uses *string so the
// three meaningful states are expressible independently:
//
//   - nil         → field omitted; leave the stored value unchanged.
//   - ""          → inherit from StationConfig at transmit time.
//   - non-empty   → explicit override (e.g. "MTNTOP-1").
//
// The response DTO carries MyCall as plain string — an empty value in
// the response means "inherits from station callsign". The override is
// stored verbatim (no case normalization here; the operator's casing
// is the source of truth for what they typed).
type DigipeaterConfigRequest struct {
	Enabled             bool    `json:"enabled"`
	DedupeWindowSeconds uint32  `json:"dedupe_window_seconds"`
	MyCall              *string `json:"my_call"`
}

func (r DigipeaterConfigRequest) Validate() error { return nil }

// ApplyToModel merges the request fields onto an existing stored
// DigipeaterConfig. Fields whose request representation is a pointer
// only overwrite the target when the pointer is non-nil, preserving
// "field omitted = leave unchanged" semantics on this PUT endpoint.
// Other fields are written unconditionally (consistent with the
// replace-style PUT pattern used elsewhere in webapi).
func (r DigipeaterConfigRequest) ApplyToModel(existing configstore.DigipeaterConfig) configstore.DigipeaterConfig {
	existing.Enabled = r.Enabled
	existing.DedupeWindowSeconds = r.DedupeWindowSeconds
	if r.MyCall != nil {
		existing.MyCall = *r.MyCall
	}
	return existing
}

type DigipeaterConfigResponse struct {
	ID                  uint32 `json:"id"`
	Enabled             bool   `json:"enabled"`
	DedupeWindowSeconds uint32 `json:"dedupe_window_seconds"`
	MyCall              string `json:"my_call"`
}

func DigipeaterConfigFromModel(m configstore.DigipeaterConfig) DigipeaterConfigResponse {
	return DigipeaterConfigResponse{
		ID:                  m.ID,
		Enabled:             m.Enabled,
		DedupeWindowSeconds: m.DedupeWindowSeconds,
		MyCall:              m.MyCall,
	}
}

// DigipeaterRuleRequest is the body accepted by POST /api/digipeater/rules
// and PUT /api/digipeater/rules/{id}.
type DigipeaterRuleRequest struct {
	FromChannel uint32 `json:"from_channel"`
	ToChannel   uint32 `json:"to_channel"`
	Alias       string `json:"alias"`
	AliasType   string `json:"alias_type"`
	MaxHops     uint32 `json:"max_hops"`
	Action      string `json:"action"`
	Priority    uint32 `json:"priority"`
	Enabled     bool   `json:"enabled"`
}

func (r DigipeaterRuleRequest) Validate() error {
	if r.Alias == "" {
		return fmt.Errorf("alias is required")
	}
	return nil
}

func (r DigipeaterRuleRequest) ToModel() configstore.DigipeaterRule {
	return configstore.DigipeaterRule{
		FromChannel: r.FromChannel,
		ToChannel:   r.ToChannel,
		Alias:       r.Alias,
		AliasType:   r.AliasType,
		MaxHops:     r.MaxHops,
		Action:      r.Action,
		Priority:    r.Priority,
		Enabled:     r.Enabled,
	}
}

func (r DigipeaterRuleRequest) ToUpdate(id uint32) configstore.DigipeaterRule {
	m := r.ToModel()
	m.ID = id
	return m
}

type DigipeaterRuleResponse struct {
	ID uint32 `json:"id"`
	DigipeaterRuleRequest
}

func DigipeaterRuleFromModel(m configstore.DigipeaterRule) DigipeaterRuleResponse {
	return DigipeaterRuleResponse{
		ID: m.ID,
		DigipeaterRuleRequest: DigipeaterRuleRequest{
			FromChannel: m.FromChannel,
			ToChannel:   m.ToChannel,
			Alias:       m.Alias,
			AliasType:   m.AliasType,
			MaxHops:     m.MaxHops,
			Action:      m.Action,
			Priority:    m.Priority,
			Enabled:     m.Enabled,
		},
	}
}

func DigipeaterRulesFromModels(ms []configstore.DigipeaterRule) []DigipeaterRuleResponse {
	out := make([]DigipeaterRuleResponse, len(ms))
	for i, m := range ms {
		out[i] = DigipeaterRuleFromModel(m)
	}
	return out
}
