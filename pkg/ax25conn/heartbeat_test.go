package ax25conn

import (
	"testing"
	"time"
)

func TestHeartbeat_NoOpInDisconnected(t *testing.T) {
	clk := newFakeClock()
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) {
		c.Clock = clk
		c.TxSink = sink
	})
	s.hb.reset() // arm hb
	clk.advance(s.cfg.Heartbeat + 50*time.Millisecond)
	// Drain pendingTimers via heartbeatTick directly.
	s.heartbeatTick()
	if sink.count() != 0 {
		t.Fatalf("disconnected heartbeat must emit nothing, got %d", sink.count())
	}
}

func TestHeartbeat_NoOpWhenNotBusy(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	s.state = StateConnected
	s.heartbeatTick()
	if sink.count() != 0 {
		t.Fatalf("non-busy heartbeat must emit nothing, got %d", sink.count())
	}
}

func TestHeartbeat_ClearsBusyAndEmitsRR(t *testing.T) {
	sink := newCaptureSink()
	s := newTestSession(t, func(c *SessionConfig) { c.TxSink = sink })
	s.state = StateConnected
	s.v.Cond.Set(CondOwnRxBusy)
	s.v.Cond.Set(CondACKPending)
	s.v.VR = 5
	s.heartbeatTick()
	if s.v.Cond.Has(CondOwnRxBusy) {
		t.Fatal("CondOwnRxBusy not cleared")
	}
	if s.v.Cond.Has(CondACKPending) {
		t.Fatal("CondACKPending not cleared")
	}
	if sink.count() != 1 {
		t.Fatalf("expected one RR frame, got %d", sink.count())
	}
	got := sink.frames[0]
	// RR rsp, NR=5, F=0 → 0xA0 | 0x01 = 0xA1
	if got.ConnectedControl[0] != 0xA1 {
		t.Fatalf("expected RR rsp NR=5 F=0 (0xA1), got 0x%02x", got.ConnectedControl[0])
	}
	if got.CommandResp {
		t.Fatal("housekeeping RR must be response polarity")
	}
}

func TestHeartbeat_ReArmsUnconditionally(t *testing.T) {
	clk := newFakeClock()
	s := newTestSession(t, func(c *SessionConfig) { c.Clock = clk })
	s.hb.reset()
	if !s.hb.running() {
		t.Fatal("hb timer must be armed after reset")
	}
	// Force tick (no-op because state=Disconnected); should rearm.
	s.heartbeatTick()
	if !s.hb.running() {
		t.Fatal("hb timer must re-arm after no-op tick")
	}
	clk.advance(s.cfg.Heartbeat + time.Millisecond)
	// After advance the deadline passes, but the fake-clock callback
	// only signals the run loop via signalTimer; running() flag
	// represents prior arming.
	_ = s
}

func TestHeartbeat_StoppedOnCleanup(t *testing.T) {
	s := newTestSession(t)
	s.hb.reset()
	s.cleanup()
	if s.hb.running() {
		t.Fatal("hb timer must stop on cleanup")
	}
}
