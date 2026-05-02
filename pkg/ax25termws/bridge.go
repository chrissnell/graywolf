package ax25termws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/ax25conn"
)

// BridgeConfig configures one per-WebSocket bridge instance.
type BridgeConfig struct {
	// Manager opens and tracks ax25conn sessions. Required.
	Manager *ax25conn.Manager
	// Logger receives bridge-side warnings (e.g. dropped envelopes).
	// Required.
	Logger *slog.Logger
	// Operator is the authenticated user identity, used by the
	// manager for per-operator session caps.
	Operator string
	// Ctx scopes the bridge's lifetime. Blocking sends on Out unblock
	// when Ctx is done so the session goroutine cannot leak after the
	// WebSocket closes.
	Ctx context.Context
	// Out is the channel the bridge fills with outbound envelopes;
	// the WebSocket handler drains it.
	Out chan<- Envelope
}

// Bridge maps inbound envelopes to ax25conn.Event submissions and
// outbound observer events to envelopes.
type Bridge struct {
	cfg     BridgeConfig
	session *ax25conn.Session
	id      uint64
}

// New constructs a Bridge. The session is opened on the first
// KindConnect envelope.
func New(cfg BridgeConfig) *Bridge { return &Bridge{cfg: cfg} }

// SessionID returns the manager-assigned session id, or 0 before a
// successful Connect.
func (b *Bridge) SessionID() uint64 { return b.id }

// Handle dispatches one inbound envelope to the appropriate side
// effect. Returns an error if the message cannot be processed.
//
// Handle is not safe for concurrent use; the caller (the WebSocket
// reader goroutine) is the only goroutine touching b.session.
func (b *Bridge) Handle(ctx context.Context, env Envelope) error {
	_ = ctx
	switch env.Kind {
	case KindConnect:
		if b.session != nil {
			return errors.New("ax25termws: session already open on this bridge")
		}
		if env.Connect == nil {
			return errors.New("ax25termws: connect: missing args")
		}
		return b.handleConnect(env.Connect)
	case KindData:
		if b.session == nil {
			return errors.New("ax25termws: not connected")
		}
		b.session.Submit(ax25conn.Event{Kind: ax25conn.EventDataTX, Data: env.Data})
	case KindDisconnect:
		if b.session != nil {
			b.session.Submit(ax25conn.Event{Kind: ax25conn.EventDisconnect})
		}
	case KindAbort:
		if b.session != nil {
			b.session.Submit(ax25conn.Event{Kind: ax25conn.EventAbort})
		}
	default:
		return fmt.Errorf("ax25termws: unknown kind: %q", env.Kind)
	}
	return nil
}

func (b *Bridge) handleConnect(c *ConnectArgs) error {
	local, err := ax25.ParseAddress(formatAddr(c.LocalCall, c.LocalSSID))
	if err != nil {
		return fmt.Errorf("ax25termws: local address: %w", err)
	}
	peer, err := ax25.ParseAddress(formatAddr(c.DestCall, c.DestSSID))
	if err != nil {
		return fmt.Errorf("ax25termws: dest address: %w", err)
	}
	path := make([]ax25.Address, 0, len(c.Via))
	for _, p := range c.Via {
		a, err := ax25.ParseAddress(p)
		if err != nil {
			return fmt.Errorf("ax25termws: via %q: %w", p, err)
		}
		path = append(path, a)
	}
	scfg := ax25conn.SessionConfig{
		Local:    local,
		Peer:     peer,
		Path:     path,
		Channel:  c.ChannelID,
		Mod128:   c.Mod128,
		N2:       c.N2,
		Paclen:   c.Paclen,
		Window:   c.Maxframe,
		Logger:   b.cfg.Logger,
		Observer: b.observe,
	}
	if c.T1MS > 0 {
		scfg.T1 = millis(c.T1MS)
	}
	if c.T2MS > 0 {
		scfg.T2 = millis(c.T2MS)
	}
	if c.T3MS > 0 {
		scfg.T3 = millis(c.T3MS)
	}
	if bo, ok := parseBackoff(c.Backoff); ok {
		scfg.Backoff = bo
	}
	id, sess, err := b.cfg.Manager.Open(scfg, b.cfg.Operator)
	if err != nil {
		return fmt.Errorf("ax25termws: open: %w", err)
	}
	b.session = sess
	b.id = id
	sess.Submit(ax25conn.Event{Kind: ax25conn.EventConnect})
	return nil
}

// observe is the session.Observer callback. Translates OutEvent into
// envelopes pushed onto cfg.Out. Send semantics differ by kind:
//
//   - OutDataRX uses BLOCKING send (with ctx cancellation). The byte
//     stream from the BBS is operator-visible -- LAPB has already
//     ack'd the I-frame to the peer, so a dropped envelope is
//     unrecoverable. Back-pressure must propagate into the session:
//     if the bridge can't drain, the session goroutine blocks here,
//     which delays kick()/RR generation and naturally throttles the
//     peer via the LAPB window. This is the desired behavior.
//
//   - OutStateChange / OutLinkStats / OutError use NON-BLOCKING send
//     with drop-on-full + warn log. These are idempotent snapshots
//     (latest state/stats supersede earlier ones) and safe to drop.
func (b *Bridge) observe(ev ax25conn.OutEvent) {
	var env Envelope
	switch ev.Kind {
	case ax25conn.OutStateChange:
		env = Envelope{Kind: KindState, State: &StatePayload{Name: ev.State.String()}}
	case ax25conn.OutDataRX:
		env = Envelope{Kind: KindDataRX, Data: ev.Data}
		select {
		case b.cfg.Out <- env:
		case <-b.cfg.Ctx.Done():
		}
		return
	case ax25conn.OutLinkStats:
		env = Envelope{Kind: KindLinkStats, Stats: linkStatsToPayload(ev.Stats)}
	case ax25conn.OutError:
		env = Envelope{Kind: KindError, Error: &ErrorPayload{Code: ev.ErrCode, Message: ev.ErrMsg}}
	default:
		return
	}
	select {
	case b.cfg.Out <- env:
	case <-b.cfg.Ctx.Done():
	default:
		b.cfg.Logger.Warn("ax25termws: out buffer full; dropping envelope",
			"kind", env.Kind)
	}
}

func formatAddr(call string, ssid uint8) string {
	call = strings.ToUpper(strings.TrimSpace(call))
	if ssid == 0 {
		return call
	}
	return fmt.Sprintf("%s-%d", call, ssid)
}

func millis(n int) time.Duration { return time.Duration(n) * time.Millisecond }

func parseBackoff(s string) (ax25conn.Backoff, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "":
		return 0, false
	case "none":
		return ax25conn.BackoffNone, true
	case "linear":
		return ax25conn.BackoffLinear, true
	case "exponential", "exp":
		return ax25conn.BackoffExponential, true
	}
	return 0, false
}

func linkStatsToPayload(s ax25conn.LinkStats) *StatsPayload {
	return &StatsPayload{
		State:    s.State.String(),
		VS:       s.VS,
		VR:       s.VR,
		VA:       s.VA,
		RC:       s.RC,
		PeerBusy: s.PeerBusy,
		OwnBusy:  s.OwnBusy,
		FramesTX: s.FramesTX,
		FramesRX: s.FramesRX,
		BytesTX:  s.BytesTX,
		BytesRX:  s.BytesRX,
		RTTMS:    int(s.RTT / time.Millisecond),
	}
}
