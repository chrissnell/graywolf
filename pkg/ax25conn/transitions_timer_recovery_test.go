package ax25conn

import (
	"context"
	"testing"
)

// inTimerRecovery transitions a session into TIMER_RECOVERY by firing
// T1 from CONNECTED. Returns the session and clears the captureSink.
func inTimerRecovery(t *testing.T, sink *captureSink, opts ...func(*SessionConfig)) *Session {
	t.Helper()
	s := connected(t, sink, opts...)
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	if s.state != StateTimerRecovery {
		t.Fatalf("setup: state=%v want TIMER_RECOVERY", s.state)
	}
	sink.frames = sink.frames[:0]
	sink.chans = sink.chans[:0]
	sink.srcs = sink.srcs[:0]
	return s
}

func TestTimerRecovery_RxRRRspFinalDrainedBackToConnected(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameRR, NR: 0, PF: true},
		IsCommand: false,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateConnected {
		t.Fatalf("RR rsp F=1 with vs==va must return to CONNECTED; state=%v", s.state)
	}
	if s.v.N2Count != 0 {
		t.Fatalf("n2count must reset on recovery; got %d", s.v.N2Count)
	}
	if !s.t3.running() {
		t.Fatal("T3 must arm on full recovery")
	}
}

func TestTimerRecovery_RxRRRspFinalPartialStaysAndKicks(t *testing.T) {
	sink := newCaptureSink()
	s := connected(t, sink, func(c *SessionConfig) { c.Paclen = 2; c.Window = 3 })
	s.handle(context.Background(), Event{Kind: EventDataTX, Data: []byte("ababab")})
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	sink.frames = sink.frames[:0]

	// RR rsp F=1 ack'ing only the first frame.
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameRR, NR: 1, PF: true},
		IsCommand: false,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateTimerRecovery {
		t.Fatalf("partial ack must stay in TIMER_RECOVERY; state=%v", s.state)
	}
	if s.v.VA != 1 {
		t.Fatalf("VA=%d want 1", s.v.VA)
	}
}

func TestTimerRecovery_RxRRRspFinalInvalidNRReestablishes(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameRR, NR: 5, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingConnection {
		t.Fatalf("invalid N(R) must re-establish; state=%v", s.state)
	}
}

func TestTimerRecovery_RxSABMRebindsAndReturnsToConnected(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateConnected {
		t.Fatalf("SABM rebind must return to CONNECTED; state=%v", s.state)
	}
	if s.v.VS != 0 || s.v.VR != 0 || s.v.VA != 0 {
		t.Fatalf("rebind must zero seq vars: %+v", s.v)
	}
}

func TestTimerRecovery_RxDISCDisconnects(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameDISC, PF: true},
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateDisconnected {
		t.Fatalf("DISC must disconnect; state=%v", s.state)
	}
}

func TestTimerRecovery_RxDMResets(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameDM, PF: true},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateDisconnected {
		t.Fatalf("DM must disconnect; state=%v", s.state)
	}
}

func TestTimerRecovery_T1RetriesUntilN2(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink, func(c *SessionConfig) { c.N2 = 3 })
	// We're in TR with n2count=1 (set on entry). Drive 2 more retries.
	for i := 0; i < 2; i++ {
		s.handle(context.Background(), Event{Kind: EventT1Expiry})
		if s.state != StateTimerRecovery {
			t.Fatalf("retry %d: state=%v want TR", i, s.state)
		}
	}
	// Next expiry hits N2 → DM + disconnect.
	s.handle(context.Background(), Event{Kind: EventT1Expiry})
	if s.state != StateDisconnected {
		t.Fatalf("N2 cap must disconnect; state=%v", s.state)
	}
	last := sink.frames[len(sink.frames)-1]
	// DM rsp F=1 → 0x1F
	if last.ConnectedControl[0] != 0x1F {
		t.Fatalf("expected DM(F=1) rsp; got 0x%02x", last.ConnectedControl[0])
	}
}

func TestTimerRecovery_T2ExpiryFlushesACKPending(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	s.v.Cond.Set(CondACKPending)
	s.handle(context.Background(), Event{Kind: EventT2Expiry})
	if s.v.Cond.Has(CondACKPending) {
		t.Fatal("T2 must clear CondACKPending in TR")
	}
}

func TestTimerRecovery_DisconnectGoesToAwaitingRelease(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	s.handle(context.Background(), Event{Kind: EventDisconnect})
	if s.state != StateAwaitingRelease {
		t.Fatalf("EventDisconnect from TR must go to AWAITING_RELEASE; state=%v", s.state)
	}
}

func TestTimerRecovery_RxIFrameInvalidNRReestablishes(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameI, NS: 0, NR: 5, PF: false},
		PID:     0xF0, Info: []byte("x"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingConnection {
		t.Fatalf("invalid N(R) must re-establish; state=%v", s.state)
	}
}

func TestTimerRecovery_RxIFrameInSequenceDelivers(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	emits := make([]OutEvent, 0)
	s.cfg.Observer = func(e OutEvent) { emits = append(emits, e) }
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 0, NR: 0, PF: false},
		PID:       0xF0, Info: []byte("yo"),
		IsCommand: true,
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.v.VR != 1 {
		t.Fatalf("VR=%d want 1", s.v.VR)
	}
	var sawData bool
	for _, e := range emits {
		if e.Kind == OutDataRX && string(e.Data) == "yo" {
			sawData = true
		}
	}
	if !sawData {
		t.Fatal("expected OutDataRX in TR")
	}
}

func TestTimerRecovery_RxFRMRReestablishes(t *testing.T) {
	sink := newCaptureSink()
	s := inTimerRecovery(t, sink)
	in := &Frame{
		Source: s.cfg.Peer, Dest: s.cfg.Local,
		Control: Control{Kind: FrameFRMR, PF: false},
	}
	s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: in})
	if s.state != StateAwaitingConnection {
		t.Fatalf("FRMR must re-establish; state=%v", s.state)
	}
}
