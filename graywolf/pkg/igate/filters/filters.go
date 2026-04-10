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
		return strings.EqualFold(strings.TrimSpace(pkt.Message.Addressee), strings.TrimSpace(r.Pattern))
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
		return strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(r.Pattern))
	}
	return false
}
