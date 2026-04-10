package beacon

import (
	"math"
	"testing"
	"time"
)

func TestSmartBeacon_IntervalAtThresholds(t *testing.T) {
	s := DefaultSmartBeacon()
	if got := s.Interval(0); got != s.SlowRate {
		t.Errorf("speed 0: got %v want %v", got, s.SlowRate)
	}
	if got := s.Interval(s.SlowSpeed); got != s.SlowRate {
		t.Errorf("speed slow_speed: got %v", got)
	}
	if got := s.Interval(s.FastSpeed); got != s.FastRate {
		t.Errorf("speed fast_speed: got %v", got)
	}
	if got := s.Interval(s.FastSpeed + 50); got != s.FastRate {
		t.Errorf("speed above fast: got %v", got)
	}
}

func TestSmartBeacon_IntervalBetween(t *testing.T) {
	s := DefaultSmartBeacon()
	// At half of fast_speed, interval should be ~2x fast_rate.
	got := s.Interval(s.FastSpeed / 2)
	want := 2 * s.FastRate
	if diff := absDuration(got - want); diff > time.Second {
		t.Errorf("half fast_speed: got %v want %v", got, want)
	}
	// Monotonic: higher speed → shorter interval.
	var prev time.Duration = 1<<63 - 1
	for v := s.SlowSpeed + 1; v < s.FastSpeed; v += 5 {
		cur := s.Interval(v)
		if cur > prev {
			t.Errorf("non-monotonic at %v: %v > %v", v, cur, prev)
		}
		prev = cur
	}
}

func TestSmartBeacon_TurnThreshold(t *testing.T) {
	s := DefaultSmartBeacon()
	if !math.IsInf(s.TurnThreshold(0), 1) {
		t.Errorf("stopped should be infinite threshold")
	}
	// At 10 kt: 30 + 255/10 = 55.5 deg
	if got := s.TurnThreshold(10); math.Abs(got-55.5) > 0.01 {
		t.Errorf("threshold at 10kt: %v", got)
	}
	// At 85 kt: 30 + 255/85 = 33 deg
	if got := s.TurnThreshold(85); math.Abs(got-33) > 0.01 {
		t.Errorf("threshold at 85kt: %v", got)
	}
	// Higher speed → tighter threshold.
	if s.TurnThreshold(80) >= s.TurnThreshold(10) {
		t.Errorf("threshold not decreasing with speed")
	}
}

func TestHeadingDelta(t *testing.T) {
	cases := []struct {
		a, b, want float64
	}{
		{0, 10, 10},
		{10, 0, 10},
		{350, 10, 20},
		{10, 350, 20},
		{0, 180, 180},
		{0, 181, 179},
	}
	for _, c := range cases {
		if got := HeadingDelta(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("delta(%v,%v)=%v want %v", c.a, c.b, got, c.want)
		}
	}
}

func absDuration(d time.Duration) time.Duration {
	if d < 0 {
		return -d
	}
	return d
}
