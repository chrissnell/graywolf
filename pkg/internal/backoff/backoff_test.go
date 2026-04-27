package backoff

import (
	"math/rand"
	"testing"
	"time"
)

func TestBackoffExponentialNoJitter(t *testing.T) {
	b := New(Config{Initial: time.Second, Max: time.Minute})
	want := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
		32 * time.Second,
		60 * time.Second, // cap
		60 * time.Second, // cap holds
	}
	for i, w := range want {
		got := b.Next()
		if got != w {
			t.Errorf("step %d: got %s, want %s", i, got, w)
		}
	}
}

func TestBackoffResetReturnsToInitial(t *testing.T) {
	b := New(Config{Initial: time.Second, Max: time.Minute})
	for i := 0; i < 4; i++ {
		b.Next()
	}
	b.Reset()
	if got := b.Next(); got != time.Second {
		t.Errorf("after reset: got %s, want 1s", got)
	}
	if got := b.Next(); got != 2*time.Second {
		t.Errorf("step after reset: got %s, want 2s", got)
	}
}

func TestBackoffJitterWithinBounds(t *testing.T) {
	b := New(Config{
		Initial:    time.Second,
		Max:        5 * time.Minute,
		JitterFrac: 0.25,
		Rand:       rand.New(rand.NewSource(42)),
	})
	base := []time.Duration{
		1 * time.Second,
		2 * time.Second,
		4 * time.Second,
		8 * time.Second,
		16 * time.Second,
	}
	for i, b0 := range base {
		d := b.Next()
		// Int63n returns [0, max), so d is in [base, base+base/4).
		if d < b0 || d >= b0+b0/4 {
			t.Errorf("step %d: got %s, want in [%s, %s)", i, d, b0, b0+b0/4)
		}
	}
}

func TestBackoffJitterIsDeterministicWithSeededRand(t *testing.T) {
	// Two backoffs with identically seeded RNGs must produce the
	// same sequence; this is what igate's tests previously relied on.
	a := New(Config{Initial: time.Second, JitterFrac: 0.25, Rand: rand.New(rand.NewSource(7))})
	b := New(Config{Initial: time.Second, JitterFrac: 0.25, Rand: rand.New(rand.NewSource(7))})
	for i := 0; i < 10; i++ {
		if x, y := a.Next(), b.Next(); x != y {
			t.Fatalf("step %d: seeds diverged: %s vs %s", i, x, y)
		}
	}
}

func TestBackoffCapAppliesBeforeJitter(t *testing.T) {
	b := New(Config{
		Initial:    time.Second,
		Max:        5 * time.Second,
		JitterFrac: 0.25,
		Rand:       rand.New(rand.NewSource(1)),
	})
	// Pull until we are capped; then verify we never exceed Max +
	// Max/4 on subsequent calls.
	for i := 0; i < 20; i++ {
		d := b.Next()
		if d < time.Second || d >= 5*time.Second+5*time.Second/4 {
			t.Errorf("step %d: got %s, expected in [1s, 6.25s)", i, d)
		}
	}
}

func TestBackoffZeroMaxIsUnbounded(t *testing.T) {
	b := New(Config{Initial: time.Millisecond})
	// After enough doublings the delay is huge; we just verify it
	// grows monotonically and never saturates at some spurious cap.
	prev := time.Duration(0)
	for i := 0; i < 20; i++ {
		d := b.Next()
		if d <= prev && prev > 0 {
			t.Errorf("step %d: delay did not grow: %s <= %s", i, d, prev)
		}
		prev = d
	}
}

func TestBackoffCustomFactor(t *testing.T) {
	b := New(Config{Initial: time.Second, Factor: 3, Max: time.Minute})
	want := []time.Duration{
		1 * time.Second,
		3 * time.Second,
		9 * time.Second,
		27 * time.Second,
		60 * time.Second, // 81s would exceed, cap at 60
		60 * time.Second,
	}
	for i, w := range want {
		got := b.Next()
		if got != w {
			t.Errorf("step %d: got %s, want %s", i, got, w)
		}
	}
}

func TestBackoffPanicsOnBadConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{"zero initial", Config{Initial: 0}},
		{"negative initial", Config{Initial: -1}},
		{"jitter too high", Config{Initial: time.Second, JitterFrac: 1.5}},
		{"jitter negative", Config{Initial: time.Second, JitterFrac: -0.1}},
		{"factor too small", Config{Initial: time.Second, Factor: 0.5}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r == nil {
					t.Errorf("expected panic for %s", tc.name)
				}
			}()
			_ = New(tc.cfg)
		})
	}
}
