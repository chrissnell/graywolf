package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// DigipeaterConfigRequest is the body accepted by PUT /api/digipeater.
type DigipeaterConfigRequest struct {
	Enabled             bool   `json:"enabled"`
	DedupeWindowSeconds uint32 `json:"dedupe_window_seconds"`
	MyCall              string `json:"my_call"`
}

func (r DigipeaterConfigRequest) Validate() error { return nil }

func (r DigipeaterConfigRequest) ToModel() configstore.DigipeaterConfig {
	return configstore.DigipeaterConfig{
		Enabled:             r.Enabled,
		DedupeWindowSeconds: r.DedupeWindowSeconds,
		MyCall:              r.MyCall,
	}
}

type DigipeaterConfigResponse struct {
	ID uint32 `json:"id"`
	DigipeaterConfigRequest
}

func DigipeaterConfigFromModel(m configstore.DigipeaterConfig) DigipeaterConfigResponse {
	return DigipeaterConfigResponse{
		ID: m.ID,
		DigipeaterConfigRequest: DigipeaterConfigRequest{
			Enabled:             m.Enabled,
			DedupeWindowSeconds: m.DedupeWindowSeconds,
			MyCall:              m.MyCall,
		},
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
