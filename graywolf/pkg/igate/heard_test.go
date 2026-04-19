package igate

import (
	"context"
	"sync"
	"testing"
	"time"
)

func TestHeardDirectRecordAndLookup(t *testing.T) {
	h := newHeardDirect()
	base := time.Unix(1_700_000_000, 0)
	h.now = func() time.Time { return base }
	h.Record("W5ABC-7")
	if !h.HeardWithin("W5ABC-7", time.Minute) {
		t.Fatal("exact-match lookup should succeed")
	}
	// Case-insensitive, whitespace-tolerant.
	if !h.HeardWithin("  w5abc-7  ", time.Minute) {
		t.Fatal("case/whitespace-tolerant lookup should succeed")
	}
}

// TestHeardDirectBaseCallMatch verifies a message addressed to the
// base callsign ("N0CALL") matches an earlier record of "N0CALL-1".
// Addressees on the wire are frequently SSID-less even when the
// station transmits with an SSID; the matcher must cover that case.
func TestHeardDirectBaseCallMatch(t *testing.T) {
	h := newHeardDirect()
	base := time.Unix(1_700_000_000, 0)
	h.now = func() time.Time { return base }
	h.Record("N0CALL-1")
	if !h.HeardWithin("N0CALL", time.Minute) {
		t.Fatal("base-call lookup should match SSID-qualified record")
	}
	if !h.HeardWithin("N0CALL-1", time.Minute) {
		t.Fatal("SSID-qualified lookup should still match")
	}
}

// TestHeardDirectReverseBaseMatch verifies the reverse case: we heard
// the base call, a message comes in addressed to an SSID-qualified
// form. That also matches — the underlying operator is the same.
func TestHeardDirectReverseBaseMatch(t *testing.T) {
	h := newHeardDirect()
	base := time.Unix(1_700_000_000, 0)
	h.now = func() time.Time { return base }
	h.Record("N0CALL")
	if !h.HeardWithin("N0CALL-7", time.Minute) {
		t.Fatal("SSID-qualified addressee should match base-call record")
	}
}

func TestHeardDirectExpiry(t *testing.T) {
	h := newHeardDirect()
	base := time.Unix(1_700_000_000, 0)
	h.now = func() time.Time { return base }
	h.Record("W5ABC")
	h.now = func() time.Time { return base.Add(29 * time.Minute) }
	if !h.HeardWithin("W5ABC", 30*time.Minute) {
		t.Fatal("within TTL should still match")
	}
	h.now = func() time.Time { return base.Add(31 * time.Minute) }
	if h.HeardWithin("W5ABC", 30*time.Minute) {
		t.Fatal("past TTL should not match")
	}
}

func TestHeardDirectEmptyInputs(t *testing.T) {
	h := newHeardDirect()
	h.Record("") // must not panic or poison the map
	if h.HeardWithin("", time.Minute) {
		t.Fatal("empty lookup must never match")
	}
	if h.HeardWithin("anyone", 0) {
		t.Fatal("zero ttl must never match")
	}
	if h.HeardWithin("anyone", -time.Second) {
		t.Fatal("negative ttl must never match")
	}
}

func TestPathIsDirect(t *testing.T) {
	cases := []struct {
		name string
		path []string
		want bool
	}{
		{"no-path", nil, true},
		{"empty", []string{}, true},
		{"unrepeated widen", []string{"WIDE1-1", "WIDE2-2"}, true},
		{"digi repeated", []string{"WIDE1-1*"}, false},
		{"second hop repeated", []string{"WIDE1-1", "WIDE2-2*"}, false},
		{"whitespace tolerant", []string{" WIDE1-1* "}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := pathIsDirect(c.path); got != c.want {
				t.Fatalf("pathIsDirect(%v) = %v, want %v", c.path, got, c.want)
			}
		})
	}
}

// TestHeardDirectSweepRemovesExpired verifies the sweeper drops entries
// older than ttl and leaves younger entries intact. The periodic
// goroutine exists to bound memory on busy channels; correctness at
// lookup time is already covered by the TTL check in HeardWithin.
func TestHeardDirectSweepRemovesExpired(t *testing.T) {
	h := newHeardDirect()
	base := time.Unix(1_700_000_000, 0)
	h.now = func() time.Time { return base }
	h.Record("OLDCALL")
	h.now = func() time.Time { return base.Add(29 * time.Minute) }
	h.Record("YOUNGCALL")

	// Advance past OLDCALL's TTL; YOUNGCALL is still within it.
	h.now = func() time.Time { return base.Add(31 * time.Minute) }
	h.sweep(30 * time.Minute)

	h.mu.Lock()
	_, haveOld := h.m["OLDCALL"]
	_, haveYoung := h.m["YOUNGCALL"]
	h.mu.Unlock()
	if haveOld {
		t.Fatal("sweep should have evicted OLDCALL")
	}
	if !haveYoung {
		t.Fatal("sweep should have retained YOUNGCALL (still within ttl)")
	}
}

// TestHeardDirectStartSweeperLifecycle verifies the periodic sweeper
// goroutine evicts expired entries on tick and exits cleanly when the
// context is cancelled. Uses a short interval and a small TTL so the
// test finishes promptly without resorting to artificial clocks.
func TestHeardDirectStartSweeperLifecycle(t *testing.T) {
	h := newHeardDirect()
	h.Record("EPHEMERAL")

	// Move the clock forward so EPHEMERAL is already past a 10ms TTL.
	h.mu.Lock()
	h.m["EPHEMERAL"] = time.Now().Add(-time.Second)
	h.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())
	h.startSweeper(ctx, 10*time.Millisecond, 5*time.Millisecond)

	// Within a few ticks, the sweeper should evict the stale entry.
	deadline := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		_, present := h.m["EPHEMERAL"]
		h.mu.Unlock()
		if !present {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	h.mu.Lock()
	_, stillThere := h.m["EPHEMERAL"]
	h.mu.Unlock()
	if stillThere {
		t.Fatal("sweeper goroutine should have evicted expired entry")
	}

	// Cancel and give the goroutine a moment to exit; nothing to assert
	// beyond the absence of leak warnings under -race.
	cancel()
	time.Sleep(20 * time.Millisecond)
}

// TestHeardDirectConcurrent ensures Record/HeardWithin are safe under
// concurrent access — the hot path is hit from every RF arrival and
// every IS→RF gating decision.
func TestHeardDirectConcurrent(t *testing.T) {
	h := newHeardDirect()
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(2)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				h.Record("W5ABC")
			}
		}(i)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 200; j++ {
				_ = h.HeardWithin("W5ABC", time.Minute)
			}
		}(i)
	}
	wg.Wait()
}
