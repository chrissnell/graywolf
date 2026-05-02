package ax25conn

import (
	"context"
	"testing"
)

// putState forces the session into the requested state and zeroes the
// counters. Used by transition tests that bypass the Connect path.
func putState(t *testing.T, s *Session, st State) {
	t.Helper()
	s.state = st
	s.stats.State = st
}

func TestAwaitingConnection_RxUAFinalAdvancesToConnected(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	s.handle(context.Background(), Event{Kind: EventConnect})
	sink.frames = sink.frames[:0]

	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateConnected {
		t.Fatalf("state=%v want CONNECTED", s.state)
	}
	if s.t1.running() {
		t.Fatal("T1 must stop on UA(F=1)")
	}
	if !s.t3.running() {
		t.Fatal("T3 must start on UA(F=1)")
	}
	if s.v.VS != 0 || s.v.VR != 0 || s.v.VA != 0 {
		t.Fatalf("seq vars: vs=%d vr=%d va=%d", s.v.VS, s.v.VR, s.v.VA)
	}
}

func TestAwaitingConnection_RxUANotFinalIgnored(t *testing.T) {
	s := newTestSession(t)
	putState(t, s, StateAwaitingConnection)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: false},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingConnection {
		t.Fatalf("state=%v want AWAITING_CONNECTION", s.state)
	}
}

func TestAwaitingConnection_RxSABMReplyUAStaysInState(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	putState(t, s, StateAwaitingConnection)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingConnection {
		t.Fatalf("must stay AWAITING_CONNECTION, got %v", s.state)
	}
	if sink.count() != 1 {
		t.Fatalf("expected one UA, got %d frames", sink.count())
	}
	got := sink.frames[0]
	if got.ConnectedControl[0] != 0x73 { // UA F=1
		t.Fatalf("expected UA(F=1) 0x73, got 0x%02x", got.ConnectedControl[0])
	}
}

func TestAwaitingConnection_RxSABMResponsePolarityDropped(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	putState(t, s, StateAwaitingConnection)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: false,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 0 {
		t.Fatal("malformed SABM-as-rsp must drop silently")
	}
}

func TestAwaitingConnection_RxSABMEUpgradesModulus(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	putState(t, s, StateAwaitingConnection)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameSABME, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if !s.cfg.Mod128 {
		t.Fatal("SABME must enable mod-128")
	}
	if s.cfg.Window != DefaultWindowMod128 {
		t.Fatalf("window must update to mod-128 default, got %d", s.cfg.Window)
	}
	if sink.count() != 1 {
		t.Fatalf("expected one UA, got %d", sink.count())
	}
}

func TestAwaitingConnection_RxDMOnMod8DisconnectsWithError(t *testing.T) {
	emits := make([]OutEvent, 0)
	s := newTestSession(t, func(c *SessionConfig) {
		c.Observer = func(e OutEvent) { emits = append(emits, e) }
	})
	putState(t, s, StateAwaitingConnection)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameDM, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateDisconnected {
		t.Fatalf("state=%v want DISCONNECTED", s.state)
	}
	var sawErr, sawState bool
	for _, e := range emits {
		if e.Kind == OutError && e.ErrCode == "peer-rejected" {
			sawErr = true
		}
		if e.Kind == OutStateChange && e.State == StateDisconnected {
			sawState = true
		}
	}
	if !sawErr {
		t.Fatalf("expected peer-rejected OutError; got %+v", emits)
	}
	if !sawState {
		t.Fatalf("expected DISCONNECTED state-change emit")
	}
}

func TestAwaitingConnection_RxDMOnMod128DowngradesAndStays(t *testing.T) {
	s := newTestSession(t, func(c *SessionConfig) { c.Mod128 = true })
	putState(t, s, StateAwaitingConnection)
	if !s.cfg.Mod128 {
		t.Fatal("setup: mod128 not set")
	}
	s.t1.reset() // simulate T1 still running
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameDM, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.cfg.Mod128 {
		t.Fatal("mod128 must downgrade to mod-8 on DM(F=1)")
	}
	if s.cfg.Window != DefaultWindowMod8 {
		t.Fatalf("window not downgraded: %d", s.cfg.Window)
	}
	if !s.t1.running() {
		t.Fatal("T1 must keep running after mod-128 downgrade")
	}
	if s.state != StateAwaitingConnection {
		t.Fatalf("state must stay AWAITING_CONNECTION, got %v", s.state)
	}
}

// After DM(F=1) downgrades a mod-128 attempt, the next T1 expiry must
// resend SABM (mod-8), not SABME. AX.25 v2.2 §6.3.4 "the calling DXE
// shall fall back to a v2.0 setup mechanism."
func TestAwaitingConnection_RxDMOnMod128ReissuesSABMOnNextT1(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) {
		c.TxSink = sink
		c.Mod128 = true
	})
	putState(t, s, StateAwaitingConnection)
	s.t1.reset()
	// Peer rejects SABME with DM(F=1).
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameDM, PF: true},
	}})
	if s.cfg.Mod128 {
		t.Fatal("must downgrade to mod-8")
	}
	sink.frames = sink.frames[:0]
	// Next T1 expiry resends as SABM (U-frame 0x3F = SABM with P=1).
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	if sink.count() != 1 {
		t.Fatalf("expected one re-issued frame, got %d", sink.count())
	}
	got := sink.frames[0]
	if got.ConnectedControl[0] != 0x3F {
		t.Fatalf("expected SABM(P=1) 0x3F, got 0x%02x", got.ConnectedControl[0])
	}
}

func TestAwaitingConnection_RxDMNotFinalIgnored(t *testing.T) {
	s := newTestSession(t)
	putState(t, s, StateAwaitingConnection)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameDM, PF: false},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingConnection {
		t.Fatalf("state=%v want AWAITING_CONNECTION", s.state)
	}
}

func TestAwaitingConnection_RxDISCRespondsDM(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	putState(t, s, StateAwaitingConnection)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameDISC, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if sink.count() != 1 {
		t.Fatalf("expected DM, got %d", sink.count())
	}
	if sink.frames[0].ConnectedControl[0] != 0x1F {
		t.Fatalf("expected DM(F=1) 0x1F, got 0x%02x", sink.frames[0].ConnectedControl[0])
	}
}

func TestAwaitingConnection_T1RetriesUntilN2(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) {
		c.TxSink = sink
		c.N2 = 3
	})
	s.handle(context.Background(), Event{Kind: EventConnect})
	// EventConnect emits one SABM. Now drive T1 expiries.
	for i := 0; i < 3; i++ {
		s.handle(context.Background(), Event{Kind: EventT1Expiry})
		if s.state != StateAwaitingConnection {
			t.Fatalf("retry %d: state=%v want AWAITING_CONNECTION", i, s.state)
		}
	}
	// Fourth expiry hits N2 cap → DISCONNECTED.
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	if s.state != StateDisconnected {
		t.Fatalf("after N2 cap: state=%v want DISCONNECTED", s.state)
	}
}

func TestAwaitingConnection_T1Mod128ExhaustionFallsBackToMod8(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) {
		c.TxSink = sink
		c.Mod128 = true
		c.N2 = 2
	})
	s.handle(context.Background(), Event{Kind: EventConnect})
	// First two T1 expiries exhaust N2 budget while mod128.
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	// Third expiry trips the n2>=N2 check; mod-128 must downgrade rather than disconnect.
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	if s.cfg.Mod128 {
		t.Fatal("mod128 must downgrade after N2 exhaustion")
	}
	if s.v.N2Count != 0 {
		t.Fatalf("n2count must reset on downgrade: %d", s.v.N2Count)
	}
	if s.state != StateAwaitingConnection {
		t.Fatalf("state=%v want AWAITING_CONNECTION", s.state)
	}
	last := sink.frames[len(sink.frames)-1]
	if last.ConnectedControl[0] != 0x3F { // SABM(P=1)
		t.Fatalf("expected SABM after fallback, got 0x%02x", last.ConnectedControl[0])
	}
}

func TestAwaitingConnection_DisconnectGoesToAwaitingRelease(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	s.handle(context.Background(), Event{Kind: EventConnect})
	sink.frames = sink.frames[:0]
	s.handle(context.Background(), Event{Kind: EventDisconnect})
	if s.state != StateAwaitingRelease {
		t.Fatalf("state=%v want AWAITING_RELEASE", s.state)
	}
	if s.v.N2Count != 0 {
		t.Fatalf("n2count must reset: %d", s.v.N2Count)
	}
	if !s.t1.running() {
		t.Fatal("T1 must restart for DISC retry")
	}
	if sink.count() != 1 {
		t.Fatalf("expected DISC frame, got %d", sink.count())
	}
	if sink.frames[0].ConnectedControl[0] != 0x53 { // DISC(P=1)
		t.Fatalf("expected DISC(P=1) 0x53, got 0x%02x", sink.frames[0].ConnectedControl[0])
	}
}
