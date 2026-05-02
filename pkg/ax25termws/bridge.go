package ax25termws

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/ax25conn"
	"github.com/chrissnell/graywolf/pkg/packetlog"
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
	// the WebSocket handler drains it. The bridge sends from three
	// goroutines: the pump (observer events), rawTailPump
	// (packetlog subscriber entries), and the reader's
	// emitErrorEnvelope path. Each sender selects on ctx.Done() so
	// teardown drains them without closing the channel.
	Out chan<- Envelope
	// OnFirstConnected, if set, is invoked once per session the first
	// time the link reaches CONNECTED. Wiring uses it to upsert a
	// recent profile so the pre-connect form's recents list reflects
	// the new connection. The callback runs on a fresh goroutine so
	// it cannot stall the session loop.
	OnFirstConnected func(args ConnectArgs)
	// Transcripts persists per-session recordings when the operator
	// toggles transcript on. Optional; nil disables transcript support
	// entirely (the bridge surfaces a typed error envelope on toggle).
	Transcripts TranscriptRecorder
	// RawPacketLog drives the raw-tail mode (Plan §3f). Optional; nil
	// disables raw-tail support so KindRawTailSubscribe surfaces a
	// typed error envelope.
	RawPacketLog *packetlog.Log
}

// TranscriptRecorder is the persistence interface the bridge calls to
// record one session's transcript. Implementations adapt the
// configstore (see pkg/webapi/ax25_terminal.go for the wiring).
type TranscriptRecorder interface {
	// Begin opens a new transcript session keyed to the connect args.
	// Returns the persistent session id.
	Begin(ctx context.Context, channelID uint32, peerCall string, peerSSID uint8, viaPath string) (uint32, error)
	// Append persists one transcript entry.
	Append(ctx context.Context, sessionID uint32, ts time.Time, direction, kind string, payload []byte) error
	// End stamps the wrap-up fields on a transcript session.
	End(ctx context.Context, sessionID uint32, reason string, bytes, frames uint64) error
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
	// closeOnce serializes Close() so concurrent invocations (e.g.
	// deferred + explicit teardown) cannot race the inner DISC submit
	// or the cancel-then-wait pumpDone read.
	closeOnce sync.Once

	// connect holds the args used for the most recent KindConnect, so
	// OnFirstConnected can upsert a recent profile keyed by them.
	connect ConnectArgs
	// firedConnected guards OnFirstConnected against re-entry on
	// repeated CONNECTED transitions (e.g. CONNECTED -> TIMER_RECOVERY
	// -> CONNECTED).
	firedConnected bool

	// Transcript-recording state. transcriptID is non-zero while the
	// bridge is actively persisting envelopes; the byte/frame counters
	// roll up the session totals so EndAX25TranscriptSession sees the
	// final numbers. The reader goroutine flips the toggle (Begin/End)
	// while the pump goroutine appends entries -- the mutex serializes
	// both sides.
	transcriptMu         sync.Mutex
	transcriptID         uint32
	transcriptByteCount  uint64
	transcriptFrameCount uint64

	// Raw-tail subscription state. rawTailCancel stops the active
	// fanout goroutine; rawTailArgs carries the active filter so the
	// goroutine can decide what to forward.
	rawTailMu     sync.Mutex
	rawTailCancel context.CancelFunc
	rawTailArgs   *RawTailSubscribeArgs
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
	b.closeOnce.Do(func() {
		// Stop the raw-tail goroutine before any session teardown.
		b.stopRawTail()
		// Wrap up an active transcript before tearing the session down
		// so EndedAt + counters land regardless of how the WebSocket
		// closes.
		_ = b.endTranscript("session-closed")
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
	})
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
		b.recordTranscriptTX(env.Data)
		b.session.Submit(ax25conn.Event{Kind: ax25conn.EventDataTX, Data: env.Data})
	case KindDisconnect:
		if b.session != nil {
			b.session.Submit(ax25conn.Event{Kind: ax25conn.EventDisconnect})
		}
	case KindAbort:
		if b.session != nil {
			b.session.Submit(ax25conn.Event{Kind: ax25conn.EventAbort})
		}
	case KindTranscriptSet:
		if env.Transcript == nil {
			return errors.New("ax25termws: transcript_set: missing payload")
		}
		return b.handleTranscriptSet(env.Transcript.Enabled)
	case KindRawTailSubscribe:
		if env.RawTailSub == nil {
			return errors.New("ax25termws: raw_tail_subscribe: missing payload")
		}
		return b.handleRawTailSubscribe(env.RawTailSub)
	case KindRawTailUnsub:
		b.stopRawTail()
		return nil
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
	b.connect = *c
	b.firedConnected = false
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
		// Best-effort overflow signal to the operator. Non-blocking
		// send so a jammed Out cannot stall the session goroutine
		// observe call. We deliberately do not race with ctx.Done()
		// here: when ctx is already cancelled the select would
		// otherwise pick the ctx case at random and silently drop
		// the overflow envelope while Out still has room. Out being
		// full has the same effect via the default arm.
		select {
		case b.cfg.Out <- Envelope{
			Kind:  KindError,
			Error: &ErrorPayload{Code: "rx_overflow", Message: "terminal too slow; bytes lost"},
		}:
		default:
		}
	}
}

// pump translates OutEvents into envelopes and serializes them onto
// cfg.Out. One of three senders on cfg.Out (see BridgeConfig.Out).
// Exits when cfg.Ctx is cancelled by the WebSocket handler.
func (b *Bridge) pump() {
	defer close(b.pumpDone)
	for {
		select {
		case <-b.ctx.Done():
			return
		case ev := <-b.inbox:
			b.maybeFireConnected(ev)
			b.recordTranscript(ev)
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

// handleTranscriptSet processes a client transcript_set envelope. The
// READER goroutine runs Handle, but the pump goroutine owns the
// transcriptID + counter fields, so the actual Begin/End calls happen
// here on the reader side using a write barrier through the inbox.
//
// Simpler approach: do the Begin / End synchronously here. The pump
// goroutine reads transcriptID via a single load on each envelope.
// Concurrent session goroutine writes don't touch these fields.
func (b *Bridge) handleTranscriptSet(enabled bool) error {
	if b.cfg.Transcripts == nil {
		b.emitErrorEnvelope("transcript_unsupported", "transcript recording is not configured on the server")
		return errors.New("ax25termws: transcript: no recorder")
	}
	// Reject pre-connect toggles up front so the operator gets a typed
	// error envelope instead of an SQL "PeerCall required" rejection
	// surfaced as transcript_begin. The connect args (channel, peer
	// callsign, via path) populate the transcript-session row, so they
	// must exist before Begin runs.
	if b.session == nil {
		b.emitErrorEnvelope("transcript_no_session", "open a session before toggling transcript")
		return errors.New("ax25termws: transcript: no active session")
	}
	if enabled {
		b.transcriptMu.Lock()
		alreadyOn := b.transcriptID != 0
		b.transcriptMu.Unlock()
		if alreadyOn {
			return nil
		}
		via := joinVia(b.connect.Via)
		id, err := b.cfg.Transcripts.Begin(b.ctx,
			b.connect.ChannelID,
			b.connect.DestCall, b.connect.DestSSID, via)
		if err != nil {
			b.emitErrorEnvelope("transcript_begin", err.Error())
			return err
		}
		b.transcriptMu.Lock()
		b.transcriptID = id
		b.transcriptByteCount = 0
		b.transcriptFrameCount = 0
		b.transcriptMu.Unlock()
		return nil
	}
	return b.endTranscript("operator-stop")
}

// endTranscript closes the active transcript session. Idempotent.
func (b *Bridge) endTranscript(reason string) error {
	b.transcriptMu.Lock()
	id := b.transcriptID
	bytes := b.transcriptByteCount
	frames := b.transcriptFrameCount
	b.transcriptID = 0
	b.transcriptMu.Unlock()
	if id == 0 || b.cfg.Transcripts == nil {
		return nil
	}
	// Detach from b.ctx — Close() cancels the context, but a final
	// End() must still land. Use a fresh context with a short timeout.
	endCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return b.cfg.Transcripts.End(endCtx, id, reason, bytes, frames)
}

// recordTranscript appends one envelope to the active transcript
// session. No-op when transcript is off. Runs on the pump goroutine.
func (b *Bridge) recordTranscript(ev ax25conn.OutEvent) {
	b.transcriptMu.Lock()
	id := b.transcriptID
	if id == 0 || b.cfg.Transcripts == nil {
		b.transcriptMu.Unlock()
		return
	}
	now := time.Now().UTC()
	switch ev.Kind {
	case ax25conn.OutDataRX:
		b.transcriptByteCount += uint64(len(ev.Data))
		b.transcriptFrameCount++
		b.transcriptMu.Unlock()
		_ = b.cfg.Transcripts.Append(b.ctx, id, now, "rx", "data", ev.Data)
	case ax25conn.OutStateChange:
		b.transcriptMu.Unlock()
		_ = b.cfg.Transcripts.Append(b.ctx, id, now, "rx", "event", []byte("state="+ev.State.String()))
	case ax25conn.OutError:
		b.transcriptMu.Unlock()
		_ = b.cfg.Transcripts.Append(b.ctx, id, now, "rx", "event",
			[]byte("error code="+ev.ErrCode+" msg="+ev.ErrMsg))
	default:
		b.transcriptMu.Unlock()
	}
}

// handleRawTailSubscribe replaces any active raw-tail subscription
// with one bound to the new args. Spawns a goroutine that pulls from
// the packetlog fanout and translates each entry into an envelope on
// b.cfg.Out (non-blocking; envelopes drop if Out is jammed).
func (b *Bridge) handleRawTailSubscribe(args *RawTailSubscribeArgs) error {
	if b.cfg.RawPacketLog == nil {
		b.emitErrorEnvelope("raw_tail_unsupported", "raw-packet feed is not configured on the server")
		return errors.New("ax25termws: raw_tail: no log")
	}
	b.stopRawTail()
	tailCtx, cancel := context.WithCancel(b.ctx)
	b.rawTailMu.Lock()
	b.rawTailCancel = cancel
	b.rawTailArgs = args
	b.rawTailMu.Unlock()
	ch := b.cfg.RawPacketLog.Subscribe(tailCtx)
	go b.rawTailPump(tailCtx, ch, args)
	return nil
}

// stopRawTail cancels the active raw-tail fanout if any. Idempotent.
func (b *Bridge) stopRawTail() {
	b.rawTailMu.Lock()
	cancel := b.rawTailCancel
	b.rawTailCancel = nil
	b.rawTailArgs = nil
	b.rawTailMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// rawTailPump drains the packetlog subscriber and writes RawTail
// envelopes to b.cfg.Out. Filters by the args fields; non-blocking
// send so a stuck WebSocket cannot back-pressure the packetlog.
func (b *Bridge) rawTailPump(ctx context.Context, ch <-chan packetlog.Entry, args *RawTailSubscribeArgs) {
	for {
		select {
		case <-ctx.Done():
			return
		case e, ok := <-ch:
			if !ok {
				return
			}
			if !rawTailMatches(args, e) {
				continue
			}
			env := Envelope{Kind: KindRawTail, RawTail: rawEntryToEnvelope(e)}
			select {
			case b.cfg.Out <- env:
			case <-ctx.Done():
				return
			default:
				// Drop on full; the operator-visible miss is via the
				// packetlog SubscribeStats counter rather than an
				// inline error envelope (would just compound the jam).
			}
		}
	}
}

func rawTailMatches(args *RawTailSubscribeArgs, e packetlog.Entry) bool {
	if args == nil {
		return true
	}
	if args.ChannelID != 0 && args.ChannelID != e.Channel {
		return false
	}
	if args.Source != "" && args.Source != e.Source {
		return false
	}
	if args.Type != "" && args.Type != e.Type {
		return false
	}
	if args.Direction != "" && string(e.Direction) != args.Direction {
		return false
	}
	if args.SubstringMatch != "" {
		needle := strings.ToUpper(args.SubstringMatch)
		hay := ""
		if e.Decoded != nil {
			hay = strings.ToUpper(e.Decoded.Source + " " + e.Decoded.Dest)
		}
		hay += " " + strings.ToUpper(string(e.Raw))
		if !strings.Contains(hay, needle) {
			return false
		}
	}
	return true
}

func rawEntryToEnvelope(e packetlog.Entry) *RawTailEntry {
	out := &RawTailEntry{
		TS:        e.Timestamp,
		Source:    e.Source,
		Type:      e.Type,
		Direction: string(e.Direction),
		ChannelID: e.Channel,
		Raw:       formatTNC2(e),
	}
	if e.Decoded != nil && e.Decoded.Source != "" {
		out.From = e.Decoded.Source
		return out
	}
	// Decoder failed (or APRS parse rejected the info field) but the
	// raw AX.25 bytes are intact. Pull the source callsign-SSID from
	// the address block so the operator still sees a callsign in the
	// "from" column rather than the subsystem tag (kiss/agw/etc).
	if len(e.Raw) > 0 {
		if hdr, err := ax25.DecodeAddressBlock(e.Raw); err == nil {
			out.From = hdr.Source.String()
		}
	}
	return out
}

// formatTNC2 renders the entry as a direwolf-style monitor line:
// "SRC>DEST[,DIGI*,...]:info". Falls back to a printable-byte
// sanitization of the raw frame if decoding fails -- the operator
// would rather see "?" placeholders than a wall of binary control
// codes that corrupt the xterm rendering.
func formatTNC2(e packetlog.Entry) string {
	if len(e.Raw) > 0 {
		if f, err := ax25.Decode(e.Raw); err == nil && f != nil {
			return sanitizeForTerminal(f.String())
		}
	}
	if e.Decoded != nil {
		var b strings.Builder
		b.WriteString(e.Decoded.Source)
		b.WriteByte('>')
		b.WriteString(e.Decoded.Dest)
		for _, p := range e.Decoded.Path {
			b.WriteByte(',')
			b.WriteString(p)
		}
		if e.Decoded.Comment != "" || e.Decoded.Status != "" {
			b.WriteByte(':')
			if e.Decoded.Status != "" {
				b.WriteString(e.Decoded.Status)
			} else {
				b.WriteString(e.Decoded.Comment)
			}
			return sanitizeForTerminal(b.String())
		}
		return sanitizeForTerminal(b.String())
	}
	return sanitizeForTerminal(string(e.Raw))
}

// sanitizeForTerminal replaces non-printable bytes (control codes,
// 8-bit binary, DEL) with '?' so an xterm-rendering client never sees
// escape sequences or raw byte values that would corrupt the display.
// Tab and newline are kept; the caller is responsible for line
// framing.
func sanitizeForTerminal(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '\t':
			b.WriteByte(' ')
		case c == '\r' || c == '\n':
			// Drop -- the monitor session adds its own framing.
		case c < 0x20 || c == 0x7f || c >= 0x80:
			b.WriteByte('?')
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// recordTranscriptTX writes an operator-typed data buffer to the
// active transcript. Called from Handle when a KindData envelope
// arrives with transcript on.
func (b *Bridge) recordTranscriptTX(data []byte) {
	b.transcriptMu.Lock()
	id := b.transcriptID
	if id == 0 || b.cfg.Transcripts == nil {
		b.transcriptMu.Unlock()
		return
	}
	b.transcriptByteCount += uint64(len(data))
	b.transcriptFrameCount++
	b.transcriptMu.Unlock()
	_ = b.cfg.Transcripts.Append(b.ctx, id, time.Now().UTC(), "tx", "data", data)
}

func joinVia(via []string) string {
	if len(via) == 0 {
		return ""
	}
	out := ""
	for i, v := range via {
		if i > 0 {
			out += ","
		}
		out += v
	}
	return out
}

// maybeFireConnected dispatches the OnFirstConnected callback once per
// session the first time the link reaches CONNECTED. Runs on the pump
// goroutine; spawns a side goroutine so a slow recorder cannot block
// envelope delivery to the WebSocket.
func (b *Bridge) maybeFireConnected(ev ax25conn.OutEvent) {
	if b.firedConnected || b.cfg.OnFirstConnected == nil {
		return
	}
	if ev.Kind != ax25conn.OutStateChange || ev.State != ax25conn.StateConnected {
		return
	}
	b.firedConnected = true
	args := b.connect
	cb := b.cfg.OnFirstConnected
	go cb(args)
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
