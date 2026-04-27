package kiss

import (
	"sync"
	"testing"
	"time"
)

// fakeClock is a deterministic Clock whose Now returns the last-advanced
// time. Safe for concurrent use; the rate-limiter tests run Allow from a
// single goroutine but the mutex keeps Advance safe if they don't.
type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock { return &fakeClock{now: time.Unix(0, 0)} }

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func TestRateLimiterUnlimitedModes(t *testing.T) {
	cases := []struct {
		name  string
		rate  uint32
		burst uint32
	}{
		{"zero rate", 0, 10},
		{"zero burst", 100, 0},
		{"both zero", 0, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clk := newFakeClock()
			r := NewRateLimiter(tc.rate, tc.burst, clk)
			for i := 0; i < 1000; i++ {
				if !r.Allow() {
					t.Fatalf("iter %d: unlimited limiter denied", i)
				}
			}
			if got := r.Dropped(); got != 0 {
				t.Fatalf("Dropped=%d, want 0 for unlimited", got)
			}
		})
	}
}

func TestRateLimiterBurstExhaustion(t *testing.T) {
	clk := newFakeClock()
	r := NewRateLimiter(10, 5, clk)

	// Burst of 5: first five allowed, remainder denied without any clock advance.
	for i := 0; i < 5; i++ {
		if !r.Allow() {
			t.Fatalf("burst slot %d unexpectedly denied", i)
		}
	}
	for i := 0; i < 3; i++ {
		if r.Allow() {
			t.Fatalf("post-burst call %d unexpectedly allowed", i)
		}
	}
	if got := r.Dropped(); got != 3 {
		t.Fatalf("Dropped=%d, want 3", got)
	}
}

func TestRateLimiterRefillAfterAdvance(t *testing.T) {
	clk := newFakeClock()
	r := NewRateLimiter(10, 2, clk) // 10 tok/s, burst 2

	// Drain the burst.
	for i := 0; i < 2; i++ {
		if !r.Allow() {
			t.Fatalf("initial burst slot %d denied", i)
		}
	}
	if r.Allow() {
		t.Fatal("third call with empty bucket should deny")
	}

	// 100ms @ 10tok/s = 1 fresh token. Allow one, deny the next.
	clk.Advance(100 * time.Millisecond)
	if !r.Allow() {
		t.Fatal("refilled token should allow")
	}
	if r.Allow() {
		t.Fatal("only one refilled token, second should deny")
	}

	// 1s @ 10tok/s = 10 tokens but capped at burst=2.
	clk.Advance(1 * time.Second)
	for i := 0; i < 2; i++ {
		if !r.Allow() {
			t.Fatalf("capped-burst slot %d denied", i)
		}
	}
	if r.Allow() {
		t.Fatal("capped burst should deny the third call")
	}
}

func TestRateLimiterDroppedMatchesDenials(t *testing.T) {
	clk := newFakeClock()
	r := NewRateLimiter(100, 3, clk)

	// 3 allowed, 7 denied.
	allowed, denied := 0, 0
	for i := 0; i < 10; i++ {
		if r.Allow() {
			allowed++
		} else {
			denied++
		}
	}
	if allowed != 3 || denied != 7 {
		t.Fatalf("allowed=%d denied=%d, want 3/7", allowed, denied)
	}
	if got := r.Dropped(); got != uint64(denied) {
		t.Fatalf("Dropped=%d, want %d", got, denied)
	}
}

func TestRateLimiterSteadyState(t *testing.T) {
	clk := newFakeClock()
	r := NewRateLimiter(50, 1, clk) // 50 tok/s, burst 1

	// Drain burst.
	if !r.Allow() {
		t.Fatal("initial token should allow")
	}

	// Advance exactly 20ms between each call: 50Hz → 1 token every 20ms.
	for i := 0; i < 20; i++ {
		clk.Advance(20 * time.Millisecond)
		if !r.Allow() {
			t.Fatalf("steady-state call %d denied", i)
		}
	}
	if got := r.Dropped(); got != 0 {
		t.Fatalf("Dropped=%d, want 0 in steady state", got)
	}
}
