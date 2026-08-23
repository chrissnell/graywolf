// Package filters implements the IS->RF gating rule engine. Rules are
// evaluated in priority order (lowest Priority value first); the first
// matching rule's Action determines the outcome. If no rule matches,
// traffic is denied — the iGate must never blindly forward APRS-IS
// traffic to RF.
package filters

import (
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// RuleType classifies a filter rule.
type RuleType string

const (
	TypeCallsign    RuleType = "callsign"     // exact source match (with SSID)
	TypePrefix      RuleType = "prefix"       // source callsign prefix (no SSID)
	TypeMessageDest RuleType = "message_dest" // message addressee match
	TypeObject      RuleType = "object"       // object/item name match
	TypePacketType  RuleType = "packet_type"  // APRS packet-type category match
)

// Action is the outcome when a rule matches.
type Action string

const (
	Allow Action = "allow"
	Deny  Action = "deny"
)

// Rule is a single filter entry. Priority orders evaluation (lower first).
type Rule struct {
	ID       uint32
	Priority int
	Type     RuleType
	Pattern  string // interpretation depends on Type
	Action   Action
}

// packetTypeCategories maps a TypePacketType rule Pattern (a stable
// category key exposed in the UI and validated by the DTO layer) to the
// set of aprs.PacketType values it covers. The keys mirror the aprs.is
// javAPRSFilter `t/...` classes (https://www.aprs-is.net/javAPRSFilter.aspx):
//
//	message (t/m), position (t/p), weather (t/w), object (t/o),
//	item (t/i), telemetry (t/t), status (t/s), query (t/q).
//
// `position` folds in Mic-E, which is a position report at heart. The key
// set is duplicated (as strings, no aprs import) in the DTO validator
// pkg/webapi/dto.packetTypeCategoryKeys — keep the two in sync.
var packetTypeCategories = map[string][]aprs.PacketType{
	"message":   {aprs.PacketMessage},
	"position":  {aprs.PacketPosition, aprs.PacketMicE},
	"weather":   {aprs.PacketWeather},
	"object":    {aprs.PacketObject},
	"item":      {aprs.PacketItem},
	"telemetry": {aprs.PacketTelemetry},
	"status":    {aprs.PacketStatus},
	"query":     {aprs.PacketQuery},
}

// PacketTypeCategory reports whether s is a recognized TypePacketType
// category key (case-insensitive, whitespace-trimmed, matching matches()).
func PacketTypeCategory(s string) bool {
	_, ok := packetTypeCategories[strings.ToLower(strings.TrimSpace(s))]
	return ok
}

// Engine evaluates rules against decoded packets.
type Engine struct {
	rules []Rule
}

// New builds an Engine with rules sorted by Priority asc, then ID asc.
func New(rules []Rule) *Engine {
	sorted := make([]Rule, len(rules))
	copy(sorted, rules)
	// Simple insertion sort keeps the API dependency-free and
	// rule lists are small (dozens, not thousands).
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0; j-- {
			if less(sorted[j], sorted[j-1]) {
				sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
				continue
			}
			break
		}
	}
	return &Engine{rules: sorted}
}

func less(a, b Rule) bool {
	if a.Priority != b.Priority {
		return a.Priority < b.Priority
	}
	return a.ID < b.ID
}

// Rules returns a copy of the engine's rules in evaluation order.
func (e *Engine) Rules() []Rule {
	out := make([]Rule, len(e.rules))
	copy(out, e.rules)
	return out
}

// Allow reports whether pkt should be forwarded IS->RF. The first
// matching rule decides; absent a match, the default is deny.
func (e *Engine) Allow(pkt *aprs.DecodedAPRSPacket) bool {
	if pkt == nil {
		return false
	}
	for _, r := range e.rules {
		if matches(r, pkt) {
			return r.Action == Allow
		}
	}
	return false
}

func matches(r Rule, pkt *aprs.DecodedAPRSPacket) bool {
	switch r.Type {
	case TypeCallsign:
		return strings.EqualFold(strings.TrimSpace(r.Pattern), pkt.Source)
	case TypePrefix:
		src := pkt.Source
		if i := strings.IndexByte(src, '-'); i >= 0 {
			src = src[:i]
		}
		return strings.HasPrefix(strings.ToUpper(src), strings.ToUpper(strings.TrimSpace(r.Pattern)))
	case TypeMessageDest:
		if pkt.Message == nil {
			return false
		}
		// A bare `*` means "any addressee". Unlike a broad source-side
		// rule, this cannot flood RF: the hardcoded tier-1 heard-direct
		// check (shouldForwardISToRF) already restricts directed-message
		// delivery to stations physically heard directly on RF within
		// heardDirectTTL, so the blast radius is bounded by radio
		// reality. It lets an operator express the textbook iGate
		// behavior — "deliver to whoever we just heard" — in one rule.
		if strings.TrimSpace(r.Pattern) == "*" {
			return true
		}
		return matchPattern(r.Pattern, pkt.Message.Addressee)
	case TypeObject:
		name := ""
		switch {
		case pkt.Object != nil:
			name = pkt.Object.Name
		case pkt.Item != nil:
			name = pkt.Item.Name
		default:
			return false
		}
		return matchPattern(r.Pattern, name)
	case TypePacketType:
		cats, ok := packetTypeCategories[strings.ToLower(strings.TrimSpace(r.Pattern))]
		if !ok {
			return false
		}
		for _, t := range cats {
			if pkt.Type == t {
				return true
			}
		}
		return false
	}
	return false
}

// matchPattern returns true if value matches pattern.
// A trailing '*' in pattern is a case-insensitive prefix wildcard.
// Otherwise the comparison is case-insensitive equality.
// A pattern that trims to "" or "*" never matches (flooding guard).
func matchPattern(pattern, value string) bool {
	p := strings.TrimSpace(pattern)
	v := strings.TrimSpace(value)
	if p == "" {
		return false
	}
	if strings.HasSuffix(p, "*") {
		prefix := p[:len(p)-1]
		if prefix == "" {
			return false
		}
		return strings.HasPrefix(strings.ToUpper(v), strings.ToUpper(prefix))
	}
	return strings.EqualFold(p, v)
}
