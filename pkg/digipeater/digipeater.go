// Package digipeater implements a WIDEn-N / TRACEn-N APRS digipeater
// with preemptive digipeating and cross-channel routing driven by
// per-channel rules stored in the configstore.
//
// The engine is a pure function of (rules, mycall, rx frame). It does
// not own the RX frame source or the TX sink; cmd/graywolf wires it in:
//
//	digi := digipeater.New(digipeater.Config{...})
//	// on RX:
//	digi.Handle(ctx, rxChannel, frame, ingress.Modem())
//
// Handle walks the path looking for the first unconsumed (H-bit clear)
// entry that matches a rule and, if it finds one, submits a cloned
// frame with the appropriate path mutation to the txgovernor at
// PriorityDigipeated. The RX frame is never mutated.
package digipeater

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/callsign"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/internal/dedup"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// Rule is a digipeater-local form of configstore.DigipeaterRule,
// decoupled from GORM.
type Rule struct {
	FromChannel uint32
	ToChannel   uint32
	Alias       string // "WIDE", "TRACE", or an exact callsign for AliasTypeExact
	AliasType   string // "widen"|"trace"|"exact"
	MaxHops     uint32
	Action      string // "repeat"|"drop"
	Priority    uint32
}

// Config configures a Digipeater.
type Config struct {
	// MyCall is the per-digipeater callsign override. Empty string means
	// "inherit from StationCallsign". A non-empty value wins (e.g. a
	// mountaintop digi running under MTNTOP-1 distinct from the operator's
	// personal call). The resolved callsign is used for preemptive digi
	// (if a path slot equals it, the frame is repeated regardless of
	// WIDEn-N rules) and for TRACEn-N insertion.
	MyCall string

	// StationCallsign is the resolved station callsign fallback used when
	// MyCall is empty. The wiring layer resolves this once per
	// start/reload from StationConfig and hands it in; the digipeater
	// package does not read configstore directly.
	StationCallsign string

	// DedupeWindow is the time window within which an identical frame
	// will be dropped. Default 30s if zero.
	DedupeWindow time.Duration

	// Rules lists all digipeater rules (across all channels). The
	// engine filters by FromChannel on each call. The slice may be
	// replaced via SetRules for live reconfig.
	Rules []Rule

	// Submit is the TX sink. Required. Typically
	// func(ctx, ch, f, src) error { return gov.Submit(...) }.
	Submit func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error

	// Logger is optional.
	Logger *slog.Logger

	// OnPacket is an optional hook invoked after a successful digipeat,
	// carrying a short human-readable note. Used by packetlog to
	// annotate entries.
	OnPacket func(note string, fromChan, toChan uint32, f *ax25.Frame)

	// OnDedup is an optional hook invoked when a frame is dropped as a
	// duplicate within the dedup window.
	OnDedup func()

	// ChannelModes resolves Channel.Mode at RX time. Rules whose RX
	// channel is "packet" cause Handle to short-circuit; rules whose
	// ToChannel is "packet" are skipped per-rule. Nil = treat every
	// channel as ChannelModeAPRS (preserves pre-Phase-0 behavior).
	// Lookup errors are silently ignored (fail-open).
	ChannelModes configstore.ChannelModeLookup
}

// Stats exposes counters.
type Stats struct {
	Packets uint64 // successfully digipeated
	Deduped uint64 // dropped as duplicate
}

// Digipeater is the engine.
type Digipeater struct {
	mu      sync.RWMutex
	enabled bool
	mycall  ax25.Address
	rules   []Rule
	submit  func(ctx context.Context, channel uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error
	logger  *slog.Logger
	onPkt   func(note string, fromChan, toChan uint32, f *ax25.Frame)
	onDedup func()

	channelModes configstore.ChannelModeLookup

	dedup *dedup.Window[string, struct{}]
	stats Stats
}

// New builds a Digipeater. Resolves cfg.MyCall (override) against
// cfg.StationCallsign (fallback) via callsign.Resolve. An empty/N0CALL
// result is not fatal at construction — the engine starts disabled and
// Handle short-circuits when mycall is empty, so a freshly-wired
// digipeater with no station callsign yet simply no-ops until a valid
// callsign arrives via SetMyCall + SetEnabled on reload.
func New(cfg Config) (*Digipeater, error) {
	if cfg.Submit == nil {
		return nil, errors.New("digipeater: Submit required")
	}
	if cfg.DedupeWindow <= 0 {
		cfg.DedupeWindow = 30 * time.Second
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	var myaddr ax25.Address
	resolved, err := callsign.Resolve(cfg.MyCall, cfg.StationCallsign)
	if err == nil {
		myaddr, err = ax25.ParseAddress(resolved)
		if err != nil {
			return nil, fmt.Errorf("digipeater: parse resolved callsign %q: %w", resolved, err)
		}
	}
	// err != nil (empty/N0CALL) leaves myaddr zero; Handle's per-frame
	// guard below drops frames with an empty mycall. This mirrors the
	// plan's D6 runtime guard: refuse to source-address a digipeated
	// frame when no valid callsign is configured.
	return &Digipeater{
		// Default off. Callers must SetEnabled(true) once the engine
		// has been populated with rules / mycall / window from config.
		// This avoids a brief window where a freshly-constructed engine
		// could accept frames with empty rules and no callsign.
		enabled:      false,
		mycall:       myaddr,
		rules:        append([]Rule(nil), cfg.Rules...),
		submit:       cfg.Submit,
		logger:       cfg.Logger.With("component", "digipeater"),
		onPkt:        cfg.OnPacket,
		onDedup:      cfg.OnDedup,
		channelModes: cfg.ChannelModes,
		dedup:        dedup.New[string, struct{}](dedup.Config{TTL: cfg.DedupeWindow}),
	}, nil
}

// SetEnabled toggles the engine on or off for live reconfig. When
// disabled, Handle short-circuits and returns false without touching
// the dedup map or rules.
func (d *Digipeater) SetEnabled(on bool) {
	d.mu.Lock()
	d.enabled = on
	d.mu.Unlock()
}

// SetDedupeWindow updates the dedupe window. A non-positive duration is
// ignored to preserve a sane default. Existing entries stay in the
// cache and are re-evaluated against the new window on their next
// touch, matching the previous in-place update behavior.
func (d *Digipeater) SetDedupeWindow(w time.Duration) {
	d.dedup.SetTTL(w)
}

// SetRules replaces the rule set under the lock. Safe for live reconfig.
func (d *Digipeater) SetRules(rules []Rule) {
	d.mu.Lock()
	d.rules = append([]Rule(nil), rules...)
	d.mu.Unlock()
}

// SetMyCall updates the local callsign for preemptive digi.
func (d *Digipeater) SetMyCall(a ax25.Address) {
	d.mu.Lock()
	d.mycall = a
	d.mu.Unlock()
}

// Stats returns a snapshot.
func (d *Digipeater) Stats() Stats {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.stats
}

// RulesFromStore converts configstore rows to local Rule values.
func RulesFromStore(rows []configstore.DigipeaterRule) []Rule {
	out := make([]Rule, 0, len(rows))
	for _, r := range rows {
		if !r.Enabled {
			continue
		}
		out = append(out, Rule{
			FromChannel: r.FromChannel,
			ToChannel:   r.ToChannel,
			Alias:       r.Alias,
			AliasType:   r.AliasType,
			MaxHops:     r.MaxHops,
			Action:      r.Action,
			Priority:    r.Priority,
		})
	}
	return out
}

// Handle evaluates frame arriving on rxChannel against the rules. If a
// rule matches and Action is "repeat", a cloned+mutated frame is
// submitted to the TX sink. The RX frame is never mutated. Returns
// true if the frame was digipeated.
//
// src identifies where the frame entered graywolf. Phase 1 threads it
// through without affecting behavior; later phases may use it to tighten
// dedup or suppress self-feedback for KISS-TNC-sourced frames.
func (d *Digipeater) Handle(ctx context.Context, rxChannel uint32, frame *ax25.Frame, src ingress.Source) bool {
	if frame == nil || !frame.IsUI() {
		return false
	}
	// Short-circuit the entire rule pipeline when the RX channel is
	// packet-mode. channelModes is set once in New() and is safe to
	// read without a lock.
	if d.channelModes != nil {
		mode, _ := d.channelModes.ModeForChannel(ctx, rxChannel)
		if mode == configstore.ChannelModePacket {
			return false
		}
	}
	d.mu.Lock()
	if !d.enabled {
		d.mu.Unlock()
		return false
	}
	mycall := d.mycall
	rules := d.rules
	d.mu.Unlock()

	// D6 runtime guard: never source-address a digipeated frame under an
	// empty or N0CALL callsign. The config-time guard (enable requires a
	// station callsign) prevents this in the happy path, but a bad state
	// (hand-edited DB, stale wiring) must not leak our identity as
	// N0CALL onto RF. Dropping with a WARN surfaces the misconfig.
	if mycall.Call == "" || callsign.IsN0Call(mycall.String()) {
		d.logger.Warn("digipeater: dropping frame — mycall is unset or N0CALL",
			"source", frame.Source.String(),
			"mycall", mycall.String())
		return false
	}

	d.mu.Lock()
	// Dedup key is computed from the RX frame including its path so
	// two identical payloads heard via different geographic paths are
	// kept distinct (collapsing them would eat a legitimate hop).
	// Seen() records the key even on a hit so concurrent duplicates
	// are caught even if submit fails later.
	key := frame.PathDedupKey()
	_, hit := d.dedup.Seen(key, struct{}{})
	if hit {
		d.stats.Deduped++
		cb := d.onDedup
		d.mu.Unlock()
		if cb != nil {
			cb()
		}
		return false
	}
	d.mu.Unlock()

	// Locate first unconsumed path slot.
	slot := firstUnconsumed(frame.Path)
	if slot < 0 {
		return false // fully-digipeated frame
	}

	// Don't re-digi our own transmissions.
	if addressEqual(frame.Source, mycall) {
		return false
	}

	// Preemptive digi: if any earlier-or-equal slot is our callsign, we
	// insert ourselves there regardless of rules.
	for i := slot; i < len(frame.Path); i++ {
		if frame.Path[i].Repeated {
			continue
		}
		if addressEqual(frame.Path[i], mycall) {
			clone := cloneFrame(frame)
			// Mark all prior unconsumed slots and this one as repeated
			// (preemptive); a strict reading of the spec leaves earlier
			// WIDE hops alone, but direwolf preempts the chain.
			for j := 0; j <= i; j++ {
				if !clone.Path[j].Repeated {
					clone.Path[j].Repeated = true
				}
			}
			// ToChannel defaults to rxChannel for preemptive digi
			// unless a matching exact rule redirects. Skip redirect
			// targets that are packet-mode.
			toCh := rxChannel
			for _, r := range rules {
				if r.FromChannel != rxChannel || r.AliasType != "exact" {
					continue
				}
				if !ruleMatchesCall(r, mycall) {
					continue
				}
				if d.channelModes != nil && r.ToChannel != rxChannel {
					mode, _ := d.channelModes.ModeForChannel(ctx, r.ToChannel)
					if mode == configstore.ChannelModePacket {
						continue
					}
				}
				toCh = r.ToChannel
				break
			}
			return d.submitClone(ctx, rxChannel, toCh, clone, "preemptive "+mycall.String())
		}
	}

	// Rule-driven match against the first unconsumed slot.
	slotAddr := frame.Path[slot]
	var matched *Rule
	for i := range rules {
		r := rules[i]
		if r.FromChannel != rxChannel {
			continue
		}
		if !ruleMatches(r, slotAddr) {
			continue
		}
		// Skip rules whose ToChannel is packet-mode; another rule may
		// still match on an APRS-capable channel.
		if d.channelModes != nil && r.ToChannel != rxChannel {
			mode, _ := d.channelModes.ModeForChannel(ctx, r.ToChannel)
			if mode == configstore.ChannelModePacket {
				continue
			}
		}
		matched = &r
		break
	}
	if matched == nil {
		return false
	}
	if matched.Action == "drop" {
		d.logger.Debug("digi drop rule matched", "slot", slotAddr.String())
		return false
	}

	clone := cloneFrame(frame)
	note, ok := applyMatch(clone, slot, *matched, mycall)
	if !ok {
		return false
	}
	return d.submitClone(ctx, rxChannel, matched.ToChannel, clone, note)
}

func (d *Digipeater) submitClone(ctx context.Context, fromCh, toCh uint32, clone *ax25.Frame, note string) bool {
	d.mu.Lock()
	d.stats.Packets++
	d.mu.Unlock()
	err := d.submit(ctx, toCh, clone, txgovernor.SubmitSource{
		Kind:     "digipeater",
		Detail:   note,
		Priority: txgovernor.PriorityDigipeated,
	})
	if err != nil {
		d.logger.Warn("digi submit", "err", err)
		return false
	}
	if d.onPkt != nil {
		d.onPkt(note, fromCh, toCh, clone)
	}
	d.logger.Debug("digipeated", "from", fromCh, "to", toCh, "note", note)
	return true
}

// firstUnconsumed returns the index of the first path slot with H bit
// clear, or -1 if all slots have been repeated.
func firstUnconsumed(path []ax25.Address) int {
	for i, a := range path {
		if !a.Repeated {
			return i
		}
	}
	return -1
}

// ruleMatches reports whether r matches the unconsumed path slot addr.
func ruleMatches(r Rule, addr ax25.Address) bool {
	switch r.AliasType {
	case "exact":
		return ruleMatchesCall(r, addr)
	case "widen":
		return matchesWidenAlias(r.Alias, r.MaxHops, addr)
	case "trace":
		return matchesWidenAlias(r.Alias, r.MaxHops, addr)
	}
	return false
}

func ruleMatchesCall(r Rule, addr ax25.Address) bool {
	parsed, err := ax25.ParseAddress(r.Alias)
	if err != nil {
		return false
	}
	return addressEqual(parsed, addr)
}

// matchesWidenAlias reports whether addr is a WIDEn-N style entry for
// the given base alias (e.g. "WIDE"). Accepted forms:
//
//	WIDE1-1   Call="WIDE1"  SSID=1   (first-hop, 1 remaining) — n=1, N=1
//	WIDE2-2   Call="WIDE2"  SSID=2   — n=2, N=2
//	WIDE2-1   Call="WIDE2"  SSID=1   — n=2, N=1 (already been through one hop)
//	WIDE-1    Call="WIDE"   SSID=1   — generic WIDE, 1 remaining (accepted but
//	                                    not recommended per New-N paradigm)
//
// The SSID (remaining hops) must be > 0 for the entry to be repeatable.
// The base n (embedded in the callsign) must be <= MaxHops.
func matchesWidenAlias(base string, maxHops uint32, addr ax25.Address) bool {
	base = strings.ToUpper(base)
	call := strings.ToUpper(addr.Call)
	if !strings.HasPrefix(call, base) {
		return false
	}
	suffix := call[len(base):]
	// SSID == remaining hops; 0 means the slot is exhausted (should not
	// normally reach here because callers only look at unconsumed slots,
	// but direwolf "WIDE2-0" frames are junk and we refuse).
	if addr.SSID == 0 {
		return false
	}
	if suffix == "" {
		// Generic "WIDE-1" style.
		return addr.SSID <= uint8(maxHops)
	}
	// Parse the embedded n digit(s).
	n, err := strconv.Atoi(suffix)
	if err != nil || n <= 0 {
		return false
	}
	if uint32(n) > maxHops {
		return false
	}
	// Remaining (SSID) must never exceed n.
	if int(addr.SSID) > n {
		return false
	}
	return true
}

// applyMatch mutates clone.Path[slot] according to the matched rule and
// returns a human-readable note for logging. Preconditions:
//   - clone.Path[slot] is the unconsumed matched entry
//   - r matches it (ruleMatches was true)
func applyMatch(clone *ax25.Frame, slot int, r Rule, mycall ax25.Address) (string, bool) {
	entry := clone.Path[slot]
	switch r.AliasType {
	case "exact":
		clone.Path[slot].Repeated = true
		return "exact " + entry.String(), true
	case "widen", "trace":
		// Decrement remaining-hops (SSID). When it reaches 0, the alias
		// is fully consumed: mark repeated and leave the SSID at 0
		// (direwolf-style "WIDE2*" display).
		if entry.SSID == 0 {
			return "", false
		}
		entry.SSID--
		if entry.SSID == 0 {
			entry.Repeated = true
		}
		clone.Path[slot] = entry
		if r.AliasType == "trace" {
			// Insert mycall just before the alias slot so the packet
			// carries an audit trail. Only if there's room.
			if len(clone.Path) < ax25.MaxRepeaters && mycall.Call != "" {
				inserted := ax25.Address{Call: mycall.Call, SSID: mycall.SSID, Repeated: true}
				newPath := make([]ax25.Address, 0, len(clone.Path)+1)
				newPath = append(newPath, clone.Path[:slot]...)
				newPath = append(newPath, inserted)
				newPath = append(newPath, clone.Path[slot:]...)
				clone.Path = newPath
			}
		}
		return r.AliasType + " " + entry.String(), true
	}
	return "", false
}

func cloneFrame(f *ax25.Frame) *ax25.Frame {
	c := *f
	c.Path = append([]ax25.Address(nil), f.Path...)
	c.Info = append([]byte(nil), f.Info...)
	return &c
}

func addressEqual(a, b ax25.Address) bool {
	return strings.EqualFold(a.Call, b.Call) && a.SSID == b.SSID
}
