package ax25conn

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestStatsTick_EmitsLinkStatsWhileConnected verifies the 1Hz CONNECTED
// telemetry tick surfaces V(S)/V(R)/V(A), RC, busy flags, and RTT to
// the observer.
func TestStatsTick_EmitsLinkStatsWhileConnected(t *testing.T) {
	var mu sync.Mutex
	var emitted []OutEvent
	s := newTestSession(t, func(c *SessionConfig) {
		c.Observer = func(e OutEvent) {
			mu.Lock()
			defer mu.Unlock()
			emitted = append(emitted, e)
		}
	})
	putState(t, s, StateConnected)
	s.v.RTT = 200 * time.Millisecond
	s.v.VS, s.v.VR, s.v.VA = 3, 5, 2
	s.v.N2Count = 1
	s.v.Cond.Set(CondPeerRxBusy)

	s.handle(context.Background(), Event{Kind: EventStatsTick})

	mu.Lock()
	defer mu.Unlock()
	var got *OutEvent
	for i := range emitted {
		if emitted[i].Kind == OutLinkStats {
			got = &emitted[i]
		}
	}
	if got == nil {
		t.Fatal("no OutLinkStats emitted")
	}
	if got.Stats.RTT != 200*time.Millisecond {
		t.Fatalf("RTT %v want 200ms", got.Stats.RTT)
	}
	if got.Stats.VS != 3 || got.Stats.VR != 5 || got.Stats.VA != 2 {
		t.Fatalf("seq vars %+v", got.Stats)
	}
	if got.Stats.RC != 1 {
		t.Fatalf("RC=%d want 1", got.Stats.RC)
	}
	if !got.Stats.PeerBusy {
		t.Fatal("PeerBusy not surfaced")
	}
}

// TestStatsTick_NoEmitWhenDisconnected ensures the tick is silent when
// the session is not in CONNECTED — keeps the bridge from spamming the
// UI on dead links.
func TestStatsTick_NoEmitWhenDisconnected(t *testing.T) {
	var mu sync.Mutex
	var emitted []OutEvent
	s := newTestSession(t, func(c *SessionConfig) {
		c.Observer = func(e OutEvent) {
			mu.Lock()
			defer mu.Unlock()
			emitted = append(emitted, e)
		}
	})
	// Disconnected by default.
	s.handle(context.Background(), Event{Kind: EventStatsTick})

	mu.Lock()
	defer mu.Unlock()
	for _, e := range emitted {
		if e.Kind == OutLinkStats {
			t.Fatalf("expected no OutLinkStats while disconnected, got %+v", e)
		}
	}
}

// TestStatsTick_TimerArmsOnConnected verifies that entering CONNECTED
// arms the stats timer and leaving CONNECTED stops it.
func TestStatsTick_TimerArmsOnConnected(t *testing.T) {
	clk := newFakeClock()
	s := newTestSession(t, func(c *SessionConfig) { c.Clock = clk })
	if s.tStats.running() {
		t.Fatal("stats timer must not be armed in disconnected")
	}
	s.setState(StateConnected)
	if !s.tStats.running() {
		t.Fatal("stats timer must arm on entering CONNECTED")
	}
	s.setState(StateDisconnected)
	if s.tStats.running() {
		t.Fatal("stats timer must stop on leaving CONNECTED")
	}
}

// TestStatsTick_Cadence drives the fake clock through one tick interval
// and verifies the pending bit fires + handle() emits OutLinkStats.
func TestStatsTick_Cadence(t *testing.T) {
	clk := newFakeClock()
	var mu sync.Mutex
	var emitted []OutEvent
	s := newTestSession(t, func(c *SessionConfig) {
		c.Clock = clk
		c.Observer = func(e OutEvent) {
			mu.Lock()
			defer mu.Unlock()
			emitted = append(emitted, e)
		}
	})
	putState(t, s, StateConnected)
	s.tStats.reset() // arm manually since putState bypasses setState

	clk.advance(s.cfg.StatsTick + 10*time.Millisecond)
	if s.pendingTimers.Load()&pendStats == 0 {
		t.Fatal("pendStats bit not set after fake-clock advance")
	}
	// Drain the pending bit and dispatch.
	s.pendingTimers.Swap(0)
	s.handle(context.Background(), Event{Kind: EventStatsTick})

	mu.Lock()
	defer mu.Unlock()
	gotStats := 0
	for _, e := range emitted {
		if e.Kind == OutLinkStats {
			gotStats++
		}
	}
	if gotStats == 0 {
		t.Fatal("expected at least one OutLinkStats after tick")
	}
	if !s.tStats.running() {
		t.Fatal("stats timer must re-arm after tick while CONNECTED")
	}
}

// TestStatsTick_RTTPropagatedFromVars checks the RTT field reflects
// the EWMA stamped by I-frame N(R) advances (the existing calcRTT
// path) rather than the cfg.T1 default.
func TestStatsTick_RTTPropagatedFromVars(t *testing.T) {
	var mu sync.Mutex
	var lastStats LinkStats
	s := newTestSession(t, func(c *SessionConfig) {
		c.Observer = func(e OutEvent) {
			if e.Kind == OutLinkStats {
				mu.Lock()
				lastStats = e.Stats
				mu.Unlock()
			}
		}
	})
	putState(t, s, StateConnected)
	s.v.RTT = 173 * time.Millisecond
	s.handle(context.Background(), Event{Kind: EventStatsTick})

	mu.Lock()
	defer mu.Unlock()
	if lastStats.RTT != 173*time.Millisecond {
		t.Fatalf("RTT %v want 173ms — stats tick must surface s.v.RTT", lastStats.RTT)
	}
}
