package dedup

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSeenFirstCallReturnsFalse(t *testing.T) {
	w := New[string, struct{}](Config{TTL: 30 * time.Second})
	if _, hit := w.Seen("k", struct{}{}); hit {
		t.Fatalf("first observation should return false")
	}
	if _, hit := w.Seen("k", struct{}{}); !hit {
		t.Fatalf("second observation within TTL should return true")
	}
}

func TestSeenCarriesValue(t *testing.T) {
	w := New[string, int](Config{TTL: 30 * time.Second})
	if _, hit := w.Seen("a", 42); hit {
		t.Fatalf("first observation should not hit")
	}
	prev, hit := w.Seen("a", 99)
	if !hit {
		t.Fatalf("second observation should hit")
	}
	if prev != 42 {
		t.Fatalf("expected previous value 42, got %d", prev)
	}
}

func TestHasDoesNotRecord(t *testing.T) {
	w := New[string, struct{}](Config{TTL: 30 * time.Second})
	if w.Has("k") {
		t.Fatalf("empty window should not report Has")
	}
	// Has on a fresh key must not create an entry.
	if w.Len() != 0 {
		t.Fatalf("Has() must not create entries; len=%d", w.Len())
	}
	if _, hit := w.Seen("k", struct{}{}); hit {
		t.Fatalf("after Has() the key must still be unknown")
	}
}

func TestRecordAndHas(t *testing.T) {
	w := New[string, struct{}](Config{TTL: 30 * time.Second})
	w.Record("k", struct{}{})
	if !w.Has("k") {
		t.Fatalf("Has should be true after Record")
	}
}

func TestPeekReturnsPreviousValue(t *testing.T) {
	w := New[string, bool](Config{TTL: 30 * time.Second})
	w.Record("k", true)
	v, when, ok := w.Peek("k")
	if !ok {
		t.Fatalf("Peek should report tracked key")
	}
	if !v {
		t.Fatalf("Peek should return stored value true")
	}
	if when.IsZero() {
		t.Fatalf("Peek time should not be zero")
	}
}

func TestTTLExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{t: now}
	w := New[string, struct{}](Config{TTL: 30 * time.Second, Now: clock.Now})

	if _, hit := w.Seen("k", struct{}{}); hit {
		t.Fatalf("first observation should miss")
	}
	// Advance past the TTL.
	clock.Advance(31 * time.Second)
	if _, hit := w.Seen("k", struct{}{}); hit {
		t.Fatalf("observation after TTL should miss")
	}
	// Now within TTL again.
	clock.Advance(5 * time.Second)
	if _, hit := w.Seen("k", struct{}{}); !hit {
		t.Fatalf("observation within TTL should hit")
	}
}

func TestGCThresholdEvictsExpired(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{t: now}
	w := New[int, struct{}](Config{
		TTL:         10 * time.Second,
		GCThreshold: 3,
		Now:         clock.Now,
	})
	// Fill three entries at t=0.
	w.Record(1, struct{}{})
	w.Record(2, struct{}{})
	w.Record(3, struct{}{})
	if w.Len() != 3 {
		t.Fatalf("want len 3, got %d", w.Len())
	}
	// Advance past the TTL so every recorded entry is stale.
	clock.Advance(11 * time.Second)
	// Record a fresh entry: gcLocked sees len(3) >= threshold(3) and
	// evicts the stale entries before inserting entry 4, leaving
	// only entry 4.
	w.Record(4, struct{}{})
	if got := w.Len(); got != 1 {
		t.Fatalf("after GC want len 1, got %d", got)
	}
}

func TestGCDoesNotRunBelowThreshold(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{t: now}
	w := New[int, struct{}](Config{
		TTL:         10 * time.Second,
		GCThreshold: 10,
		Now:         clock.Now,
	})
	for i := 0; i < 5; i++ {
		w.Record(i, struct{}{})
	}
	clock.Advance(1 * time.Hour)
	// Below threshold, GC does not run, so Len returns the stale count.
	if w.Len() != 5 {
		t.Fatalf("below threshold GC should not run; got len %d", w.Len())
	}
}

func TestSetTTL(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	clock := &fakeClock{t: now}
	w := New[string, struct{}](Config{TTL: 30 * time.Second, Now: clock.Now})
	w.Record("k", struct{}{})
	clock.Advance(20 * time.Second)
	// Within original window.
	if !w.Has("k") {
		t.Fatalf("entry should still be within 30s TTL")
	}
	// Shrink the window. The existing entry is 20s old; with a 10s
	// TTL it should now be considered expired.
	w.SetTTL(10 * time.Second)
	if w.Has("k") {
		t.Fatalf("entry should be expired under shrunk TTL")
	}
	// Non-positive is ignored.
	w.SetTTL(-1)
	w.Record("k", struct{}{})
	if !w.Has("k") {
		t.Fatalf("fresh entry should be present under preserved 10s TTL")
	}
}

func TestReset(t *testing.T) {
	w := New[string, struct{}](Config{TTL: 30 * time.Second})
	w.Record("a", struct{}{})
	w.Record("b", struct{}{})
	if w.Len() != 2 {
		t.Fatalf("want 2, got %d", w.Len())
	}
	w.Reset()
	if w.Len() != 0 {
		t.Fatalf("Reset should clear; got %d", w.Len())
	}
}

func TestNonPositiveTTLPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("New with TTL=0 should panic")
		}
	}()
	_ = New[int, struct{}](Config{TTL: 0})
}

func TestConcurrentAccess(t *testing.T) {
	w := New[int, struct{}](Config{TTL: time.Second, GCThreshold: 32})
	var wg sync.WaitGroup
	var hits atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				// Overlap keys so both fresh inserts and duplicate hits
				// exercise the lock path.
				if _, hit := w.Seen(i%50, struct{}{}); hit {
					hits.Add(1)
				}
				_ = w.Has(i % 50)
				_, _, _ = w.Peek(i % 50)
			}
		}(g)
	}
	wg.Wait()
	if hits.Load() == 0 {
		t.Fatalf("expected some hits across concurrent workers")
	}
}

// fakeClock is a monotonic fake clock for deterministic time tests.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}
