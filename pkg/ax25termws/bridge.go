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
	// Ctx scopes the bridge's lifetime. The pump goroutine that
	// drains the observer inbox into Out exits when Ctx is done so
	// no goroutine leaks after the WebSocket closes.
	Ctx context.Context
	// Out is the channel the bridge fills with outbound envelopes;
	// the WebSocket handler drains it. The pump goroutine is the
	// sole sender on this channel.
	Out chan<- Envelope
}

// inboxSize bounds the observer-to-pump queue. The session goroutine
// must never block on the bridge -- it owns the LAPB timers (T1/T2/T3)
// and any blocked observer call would starve frame retransmits and
// keepalives. We choose a buffer large enough to absorb the
// largest realistic burst (a peer sending several windows of paclen
// frames back-to-back plus state/stats interleaving) so non-blocking
// sends from observe() basically never overflow during normal
// operation. On overflow we drop and emit a typed KindError so the
// operator sees that bytes were lost rather than silently dropping
// data the LAPB layer has already ack'd to the peer.
const inboxSize = 1024

// Bridge maps inbound envelopes to ax25conn.Event submissions and
// outbound observer events to envelopes.
//
// The bridge runs one internal pump goroutine that translates
// OutEvent -> Envelope and writes into cfg.Out. observe() is invoked
// directly from the session goroutine and MUST stay non-blocking;
// it enqueues into an internal channel that the pump drains.
//
// The bridge owns an internal context derived from cfg.Ctx so Close()
// can stop the pump even when the parent ctx is still alive (e.g. a
// test that wants to verify Close behavior without tearing down the
// whole http handler).
type Bridge struct {
	cfg      BridgeConfig
	ctx      context.Context
	cancel   context.CancelFunc
	session  *ax25conn.Session
	id       uint64
	inbox    chan ax25conn.OutEvent
	pumpDone chan struct{}
	closed   bool
}

// New constructs a Bridge and starts its pump goroutine. The session
// is opened on the first KindConnect envelope. Callers MUST invoke
// Close exactly once when the WebSocket terminates so any active
// LAPB session receives a clean DISC frame on the wire.
func New(cfg BridgeConfig) *Bridge {
	parent := cfg.Ctx
	if parent == nil {
		parent = context.Background()
	}
	ctx, cancel := context.WithCancel(parent)
	b := &Bridge{
		cfg:      cfg,
		ctx:      ctx,
		cancel:   cancel,
		inbox:    make(chan ax25conn.OutEvent, inboxSize),
		pumpDone: make(chan struct{}),
	}
	go b.pump()
	return b
}

// Close requests a clean LAPB DISC on any active session and waits
// for the pump goroutine to exit. Safe to call multiple times.
//
// Why DISC and not Abort: when an operator closes their browser tab,
// the session WAS in CONNECTED -- LAPB requires a proper disconnect
// handshake (DISC -> UA) so the peer's state machine drops the link
// instead of waiting for N2 retries to time out. The session's
// AWAITING_RELEASE timer guarantees the goroutine still exits even
// if the peer never UAs.
func (b *Bridge) Close() {
	if b.closed {
		return
	}
	b.closed = true
	if b.session != nil {
		b.session.Submit(ax25conn.Event{Kind: ax25conn.EventDisconnect})
	}
	// Cancel the internal ctx so the pump exits even if the parent
	// ctx is still alive. We don't close the inbox: the session
	// goroutine may still emit a final OutStateChange(DISCONNECTED)
	// via cleanup() after our Disconnect submit -- that emit fires
	// synchronously inside the session's Run loop and will land in
	// the inbox if there's room or be dropped silently if not. The
	// pump no longer drains after b.ctx is done either way.
	b.cancel()
	<-b.pumpDone
}

// SessionID returns the manager-assigned session id, or 0 before a
// successful Connect.
func (b *Bridge) SessionID() uint64 { return b.id }

// pumpExited reports whether the internal pump goroutine has finished.
// Tests use it to wait for ctx cancellation to take effect; production
// code should call Close instead.
func (b *Bridge) pumpExited() bool {
	select {
	case <-b.pumpDone:
		return true
	default:
		return false
	}
}

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
		// Surface a typed error envelope so the operator UI can
		// render the reason instead of just never reaching CONNECTED.
		b.emitErrorEnvelope("open", err.Error())
		return fmt.Errorf("ax25termws: open: %w", err)
	}
	b.session = sess
	b.id = id
	sess.Submit(ax25conn.Event{Kind: ax25conn.EventConnect})
	return nil
}

// emitErrorEnvelope pushes a synthesized KindError envelope onto Out
// without going through the session observer path. Used for failures
// that happen before a session exists (Manager.Open rejection) where
// observe() is not in play.
func (b *Bridge) emitErrorEnvelope(code, msg string) {
	env := Envelope{Kind: KindError, Error: &ErrorPayload{Code: code, Message: msg}}
	select {
	case b.cfg.Out <- env:
	case <-b.ctx.Done():
	default:
		b.cfg.Logger.Warn("ax25termws: out buffer full; dropping error envelope",
			"code", code)
	}
}

// observe is the session.Observer callback. It runs INLINE on the
// session goroutine that owns T1/T2/T3 dispatch, RR/RNR generation,
// and frame retransmits, so it MUST be non-blocking. We enqueue into
// the internal inbox channel; the pump goroutine drains it onto
// cfg.Out.
//
// On inbox overflow we drop the event and emit a typed rx_overflow
// error envelope so the operator sees that data was lost rather than
// silently corrupting their session. inboxSize is large enough that
// overflow only happens when the WebSocket writer is itself jammed
// for many seconds.
func (b *Bridge) observe(ev ax25conn.OutEvent) {
	select {
	case b.inbox <- ev:
	default:
		b.cfg.Logger.Warn("ax25termws: observer inbox full; dropping event",
			"kind", ev.Kind)
		// Best-effort overflow signal to the operator. The error
		// envelope itself can be dropped if Out is also full -- at
		// that point the WS is hopelessly behind anyway.
		select {
		case b.cfg.Out <- Envelope{
			Kind:  KindError,
			Error: &ErrorPayload{Code: "rx_overflow", Message: "terminal too slow; bytes lost"},
		}:
		case <-b.ctx.Done():
		default:
		}
	}
}

// pump translates OutEvents into envelopes and serializes them onto
// cfg.Out. Sole sender on cfg.Out. Exits when cfg.Ctx is cancelled by
// the WebSocket handler.
func (b *Bridge) pump() {
	defer close(b.pumpDone)
	for {
		select {
		case <-b.ctx.Done():
			return
		case ev := <-b.inbox:
			env, ok := translateOutEvent(ev)
			if !ok {
				continue
			}
			select {
			case b.cfg.Out <- env:
			case <-b.ctx.Done():
				return
			}
		}
	}
}

// translateOutEvent maps a session OutEvent to its wire envelope.
// Returns (_, false) for OutEvents the bridge intentionally drops
// (none today; here so future kinds can opt out cleanly).
func translateOutEvent(ev ax25conn.OutEvent) (Envelope, bool) {
	switch ev.Kind {
	case ax25conn.OutStateChange:
		return Envelope{Kind: KindState, State: &StatePayload{Name: ev.State.String()}}, true
	case ax25conn.OutDataRX:
		return Envelope{Kind: KindDataRX, Data: ev.Data}, true
	case ax25conn.OutLinkStats:
		return Envelope{Kind: KindLinkStats, Stats: linkStatsToPayload(ev.Stats)}, true
	case ax25conn.OutError:
		return Envelope{Kind: KindError, Error: &ErrorPayload{Code: ev.ErrCode, Message: ev.ErrMsg}}, true
	}
	return Envelope{}, false
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
