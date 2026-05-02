package ax25conn

// Phase 1 baseline integration tests: a single Session is driven
// through full handshake / I-frame exchange / disconnect against a
// scripted peer. Production-grade replay against captured kernel
// traces ships in Phase 4 (along with real captures); Phase 1 is
// "spec-correct synthetic" only — see CREDITS.md.

import (
	"context"
	"testing"
	"time"
)

// scenarioRunner drives a Session synchronously: each step pushes one
// event through s.handle() and timer expiries are dispatched after
// advance_clock crosses their deadlines. The run loop is bypassed so
// every step is deterministic.
type scenarioRunner struct {
	t    *testing.T
	clk  *fakeClock
	s    *Session
	sink *captureSink
}

func newScenarioRunner(t *testing.T, opts ...func(*SessionConfig)) *scenarioRunner {
	t.Helper()
	clk := newFakeClock()
	sink := newCaptureSink()
	cfg := SessionConfig{
		Local:   mustParse(t, "KE7XYZ-1"),
		Peer:    mustParse(t, "BBS-3"),
		Channel: 1,
		TxSink:  sink,
		Clock:   clk,
	}
	for _, o := range opts {
		o(&cfg)
	}
	s, err := NewSession(cfg)
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	return &scenarioRunner{t: t, clk: clk, s: s, sink: sink}
}

func (r *scenarioRunner) connect() {
	r.s.handle(context.Background(), Event{Kind: EventConnect})
}

func (r *scenarioRunner) rx(f *Frame) {
	r.s.handle(context.Background(), Event{Kind: EventFrameRX, Frame: f})
}

func (r *scenarioRunner) data(b []byte) {
	r.s.handle(context.Background(), Event{Kind: EventDataTX, Data: b})
}

func (r *scenarioRunner) disconnect() {
	r.s.handle(context.Background(), Event{Kind: EventDisconnect})
}

// advance moves the fake clock forward by d, then dispatches any
// timer expiries the advancement triggered. Mirrors the
// drain-pending-timers loop in Session.Run.
func (r *scenarioRunner) advance(d time.Duration) {
	r.clk.advance(d)
	for {
		bits := r.s.pendingTimers.Swap(0)
		if bits == 0 {
			return
		}
		if bits&pendT1 != 0 {
			r.s.handle(context.Background(), Event{Kind: EventT1Expiry})
		}
		if bits&pendT2 != 0 {
			r.s.handle(context.Background(), Event{Kind: EventT2Expiry})
		}
		if bits&pendT3 != 0 {
			r.s.handle(context.Background(), Event{Kind: EventT3Expiry})
		}
		if bits&pendHB != 0 {
			r.s.handle(context.Background(), Event{Kind: EventHeartbeat})
		}
	}
}

// expectTX pops the next captured frame and asserts its first control
// byte matches want.
func (r *scenarioRunner) expectTX(want byte, label string) {
	r.t.Helper()
	if len(r.sink.frames) == 0 {
		r.t.Fatalf("%s: expected TX 0x%02x, sink empty", label, want)
	}
	got := r.sink.frames[0].ConnectedControl[0]
	if got != want {
		r.t.Fatalf("%s: expected 0x%02x, got 0x%02x", label, want, got)
	}
	r.sink.frames = r.sink.frames[1:]
	r.sink.chans = r.sink.chans[1:]
	r.sink.srcs = r.sink.srcs[1:]
}

// expectSilence asserts the sink has no captured frames left.
func (r *scenarioRunner) expectSilence(label string) {
	r.t.Helper()
	if len(r.sink.frames) != 0 {
		r.t.Fatalf("%s: expected silence, got %d frames first 0x%02x",
			label, len(r.sink.frames), r.sink.frames[0].ConnectedControl[0])
	}
}

func TestIntegration_Handshake_IExchange_Disconnect(t *testing.T) {
	r := newScenarioRunner(t)

	// 1. Operator presses Connect → SABM(P=1) goes out.
	r.connect()
	r.expectTX(0x3F, "SABM(P=1)")
	if r.s.state != StateAwaitingConnection {
		t.Fatalf("post-connect state=%v", r.s.state)
	}

	// 2. Peer answers UA(F=1) → CONNECTED.
	r.rx(&Frame{
		Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: true},
	})
	if r.s.state != StateConnected {
		t.Fatalf("post-UA state=%v", r.s.state)
	}
	r.expectSilence("post-UA")

	// 3. Operator types "hi" → I-frame(NS=0, NR=0, P=0) sent.
	r.data([]byte("hi"))
	r.expectTX(0x00, "I NS=0 NR=0 P=0")

	// 4. Peer ACKs with RR(NR=1, rsp) → VA advances, T1 stops, T3 starts.
	r.rx(&Frame{
		Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control:   Control{Kind: FrameRR, NR: 1, PF: false},
		IsCommand: false,
	})
	if r.s.v.VA != 1 {
		t.Fatalf("VA=%d want 1", r.s.v.VA)
	}
	if r.s.t1.running() {
		t.Fatal("T1 must stop after full ack")
	}
	if !r.s.t3.running() {
		t.Fatal("T3 must arm after full ack")
	}

	// 5. Peer sends I-frame "yo" (NS=0, NR=1) → we deliver, set ACK_PENDING + T2.
	r.rx(&Frame{
		Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control:   Control{Kind: FrameI, NS: 0, NR: 1, PF: false},
		PID:       0xF0,
		Info:      []byte("yo"),
		IsCommand: true,
	})
	if r.s.v.VR != 1 {
		t.Fatalf("VR=%d want 1", r.s.v.VR)
	}
	if !r.s.v.Cond.Has(CondACKPending) {
		t.Fatal("CondACKPending must set")
	}
	r.expectSilence("post-I-RX (ACK piggyback pending)")

	// 6. Advance past T2 (3s default) → RR(F=0,rsp) emitted.
	r.advance(r.s.cfg.T2 + 100*time.Millisecond)
	// RR rsp NR=1 F=0 → 0x21
	r.expectTX(0x21, "T2-flushed RR(F=0,rsp)")

	// 7. Operator pulls disconnect → DISC(P=1) + AWAITING_RELEASE.
	r.disconnect()
	r.expectTX(0x53, "DISC(P=1)")
	if r.s.state != StateAwaitingRelease {
		t.Fatalf("post-disconnect state=%v", r.s.state)
	}

	// 8. Peer answers UA(F=1) → DISCONNECTED.
	r.rx(&Frame{
		Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: true},
	})
	if r.s.state != StateDisconnected {
		t.Fatalf("post-UA-of-DISC state=%v", r.s.state)
	}
}

func TestIntegration_T1ExpiryEntersTimerRecoveryAndRecoversOnEnquiryAck(t *testing.T) {
	r := newScenarioRunner(t)
	r.connect()
	r.expectTX(0x3F, "SABM")
	r.rx(&Frame{Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: true}})
	if r.s.state != StateConnected {
		t.Fatalf("post-UA state=%v", r.s.state)
	}

	// Send an I-frame. T1 starts.
	r.data([]byte("x"))
	r.expectTX(0x00, "I NS=0")

	// Advance past T1 → enquiry RR(cmd, P=1) and TIMER_RECOVERY.
	r.advance(r.s.cfg.T1 + 100*time.Millisecond)
	// RR cmd NR=0 P=1 → 0x11
	r.expectTX(0x11, "T1-enquiry RR(cmd,P=1)")
	if r.s.state != StateTimerRecovery {
		t.Fatalf("post-T1 state=%v", r.s.state)
	}

	// Peer's enquiry response RR(rsp, F=1, NR=1) → recovery: ack frame, return to CONNECTED.
	r.rx(&Frame{Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control: Control{Kind: FrameRR, NR: 1, PF: true}})
	if r.s.state != StateConnected {
		t.Fatalf("post-recovery state=%v", r.s.state)
	}
	if r.s.v.VA != 1 {
		t.Fatalf("VA=%d want 1", r.s.v.VA)
	}
}

func TestIntegration_REJRequeuesAndResends(t *testing.T) {
	r := newScenarioRunner(t, func(c *SessionConfig) { c.Paclen = 2; c.Window = 3 })
	r.connect()
	r.expectTX(0x3F, "SABM")
	r.rx(&Frame{Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: true}})
	r.data([]byte("ababab"))
	r.expectTX(0x00, "I NS=0") // "ab"
	r.expectTX(0x02, "I NS=1") // "ab" — NS=1<<1 = 0x02
	r.expectTX(0x04, "I NS=2") // "ab" — NS=2<<1 = 0x04

	// Peer rejects from NR=1 (frame 0 was fine, frames 1+2 must resend).
	r.rx(&Frame{Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control:   Control{Kind: FrameREJ, NR: 1, PF: false},
		IsCommand: false})
	r.expectTX(0x02, "resend NS=1")
	r.expectTX(0x04, "resend NS=2")
	r.expectSilence("post-REJ")
}

func TestIntegration_DISCFromPeerCleansUp(t *testing.T) {
	r := newScenarioRunner(t)
	r.connect()
	r.expectTX(0x3F, "SABM")
	r.rx(&Frame{Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control: Control{Kind: FrameUA, PF: true}})

	r.rx(&Frame{Source: r.s.cfg.Peer, Dest: r.s.cfg.Local,
		Control:   Control{Kind: FrameDISC, PF: true},
		IsCommand: true})
	r.expectTX(0x73, "UA reply to peer DISC")
	if r.s.state != StateDisconnected {
		t.Fatalf("state=%v want DISCONNECTED", r.s.state)
	}
}
