package ax25conn

import (
	"testing"
	"time"
)

func TestNextT1_NoneStrategy(t *testing.T) {
	s := newTestSession(t, func(c *SessionConfig) { c.Backoff = BackoffNone })
	s.v.RTT = 200 * time.Millisecond
	s.v.N2Count = 0
	if got, want := s.nextT1(), 400*time.Millisecond; got != want {
		t.Fatalf("n2=0: got %v want %v", got, want)
	}
	s.v.N2Count = 5
	if got, want := s.nextT1(), 400*time.Millisecond; got != want {
		t.Fatalf("n2=5: got %v want %v (must ignore retries)", got, want)
	}
}

func TestNextT1_LinearStrategy(t *testing.T) {
	s := newTestSession(t, func(c *SessionConfig) { c.Backoff = BackoffLinear })
	s.v.RTT = 100 * time.Millisecond
	cases := []struct {
		n2   int
		want time.Duration
	}{
		{0, 200 * time.Millisecond},
		{1, 400 * time.Millisecond},
		{2, 600 * time.Millisecond},
		{3, 800 * time.Millisecond},
	}
	for _, c := range cases {
		s.v.N2Count = c.n2
		if got := s.nextT1(); got != c.want {
			t.Errorf("n2=%d: got %v want %v", c.n2, got, c.want)
		}
	}
}

func TestNextT1_ExponentialStrategyCapsAt8x(t *testing.T) {
	s := newTestSession(t, func(c *SessionConfig) { c.Backoff = BackoffExponential })
	s.v.RTT = 100 * time.Millisecond
	cases := []struct {
		n2   int
		want time.Duration
	}{
		{0, 200 * time.Millisecond}, // (1<<0)*2*100ms
		{1, 400 * time.Millisecond},
		{2, 800 * time.Millisecond}, // 8*RTT cap
		{3, 800 * time.Millisecond},
		{8, 800 * time.Millisecond},
	}
	for _, c := range cases {
		s.v.N2Count = c.n2
		if got := s.nextT1(); got != c.want {
			t.Errorf("n2=%d: got %v want %v", c.n2, got, c.want)
		}
	}
}

func TestNextT1_RTTClamped(t *testing.T) {
	s := newTestSession(t, func(c *SessionConfig) { c.Backoff = BackoffNone })
	// Below RTTClampLo gets clamped up.
	s.v.RTT = 100 * time.Microsecond
	if got := s.nextT1(); got != 2*RTTClampLo {
		t.Fatalf("low clamp: got %v want %v", got, 2*RTTClampLo)
	}
	// Above RTTClampHi gets clamped down.
	s.v.RTT = 5 * time.Minute
	if got := s.nextT1(); got != 2*RTTClampHi {
		t.Fatalf("high clamp: got %v want %v", got, 2*RTTClampHi)
	}
}

func TestNextT1_SeedsFromHalfT1WhenRTTZero(t *testing.T) {
	s := newTestSession(t, func(c *SessionConfig) { c.Backoff = BackoffNone })
	s.v.RTT = 0
	if got, want := s.nextT1(), 2*(s.cfg.T1/2); got != want {
		t.Fatalf("seed: got %v want %v", got, want)
	}
}

func TestCalcRTT_OnlyOnFirstShot(t *testing.T) {
	clk := newFakeClock()
	s := newTestSession(t, func(c *SessionConfig) { c.Clock = clk })
	s.v.RTT = 100 * time.Millisecond
	s.v.T1Started = clk.Now()
	clk.advance(150 * time.Millisecond)

	// Retransmit (n2>0) — must not update RTT.
	s.v.N2Count = 1
	s.calcRTT()
	if s.v.RTT != 100*time.Millisecond {
		t.Fatalf("retransmit poisoned RTT: %v", s.v.RTT)
	}

	// First-shot ack — EWMA folds in the new sample.
	s.v.N2Count = 0
	s.calcRTT()
	want := (9*100*time.Millisecond + 150*time.Millisecond) / 10
	if s.v.RTT != want {
		t.Fatalf("first-shot RTT: got %v want %v", s.v.RTT, want)
	}
}

func TestCalcRTT_NegativeMeasurementIgnored(t *testing.T) {
	clk := newFakeClock()
	s := newTestSession(t, func(c *SessionConfig) { c.Clock = clk })
	clk.advance(time.Second)
	s.v.RTT = 100 * time.Millisecond
	// T1Started in the future → negative measurement.
	s.v.T1Started = clk.Now().Add(time.Second)
	s.v.N2Count = 0
	s.calcRTT()
	if s.v.RTT != 100*time.Millisecond {
		t.Fatalf("negative measurement updated RTT: %v", s.v.RTT)
	}
}

func TestCalcRTT_ClampsAfterEWMA(t *testing.T) {
	clk := newFakeClock()
	s := newTestSession(t, func(c *SessionConfig) { c.Clock = clk })
	s.v.RTT = 0
	s.v.T1Started = clk.Now()
	clk.advance(2 * time.Hour)
	s.v.N2Count = 0
	s.calcRTT()
	if s.v.RTT > RTTClampHi {
		t.Fatalf("RTT not clamped to high: %v", s.v.RTT)
	}
}

func TestResetT1_StampsStartTimeAndUsesBackoff(t *testing.T) {
	clk := newFakeClock()
	s := newTestSession(t, func(c *SessionConfig) {
		c.Clock = clk
		c.Backoff = BackoffNone
	})
	s.v.RTT = 100 * time.Millisecond
	s.v.N2Count = 0
	s.resetT1()
	if s.v.T1Started.IsZero() {
		t.Fatal("T1Started not stamped")
	}
	if !s.t1.running() {
		t.Fatal("T1 not armed after resetT1")
	}
}
