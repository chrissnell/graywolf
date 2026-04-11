package ratelimit

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCountInEmptyWindow(t *testing.T) {
	w := New(time.Minute)
	if n := w.Count(); n != 0 {
		t.Fatalf("empty window count = %d, want 0", n)
	}
}

func TestRecordAndCount(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	w := NewWithClock(time.Minute, clock.Now)
	w.Record()
	w.Record()
	w.Record()
	if n := w.Count(); n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
}

func TestEventsFallOutOfWindow(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	w := NewWithClock(time.Minute, clock.Now)
	// Three events at t=0.
	for i := 0; i < 3; i++ {
		w.Record()
	}
	if n := w.Count(); n != 3 {
		t.Fatalf("count = %d, want 3", n)
	}
	// Advance 30 seconds: still within the minute.
	clock.Advance(30 * time.Second)
	if n := w.Count(); n != 3 {
		t.Fatalf("count after 30s = %d, want 3", n)
	}
	// Record two more at t=30s.
	w.Record()
	w.Record()
	if n := w.Count(); n != 5 {
		t.Fatalf("count after two more = %d, want 5", n)
	}
	// Advance past the first three (total 61s from t=0, t=30s for the
	// last two → 31s old).
	clock.Advance(31 * time.Second)
	if n := w.Count(); n != 2 {
		t.Fatalf("count after first batch expired = %d, want 2", n)
	}
	// Advance past the last two as well.
	clock.Advance(60 * time.Second)
	if n := w.Count(); n != 0 {
		t.Fatalf("count after all expired = %d, want 0", n)
	}
}

func TestCountShrinksBackingSlice(t *testing.T) {
	clock := &fakeClock{t: time.Unix(1_700_000_000, 0)}
	w := NewWithClock(time.Minute, clock.Now)
	for i := 0; i < 10; i++ {
		w.Record()
	}
	if n := w.Count(); n != 10 {
		t.Fatalf("count = %d, want 10", n)
	}
	clock.Advance(2 * time.Minute)
	if n := w.Count(); n != 0 {
		t.Fatalf("count after window = %d, want 0", n)
	}
	// Count() should have compacted the slice rather than leaving
	// 10 stale entries behind.
	w.mu.Lock()
	got := len(w.events)
	w.mu.Unlock()
	if got != 0 {
		t.Fatalf("backing slice len = %d, want 0", got)
	}
}

func TestReset(t *testing.T) {
	w := New(time.Minute)
	w.Record()
	w.Record()
	w.Reset()
	if n := w.Count(); n != 0 {
		t.Fatalf("count after Reset = %d, want 0", n)
	}
}

func TestNewPanicsOnBadWindow(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for non-positive window")
		}
	}()
	_ = New(0)
}

func TestNewWithClockPanicsOnNilClock(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic for nil clock")
		}
	}()
	_ = NewWithClock(time.Second, nil)
}

func TestConcurrentRecordAndCount(t *testing.T) {
	w := New(time.Second)
	var wg sync.WaitGroup
	var seen atomic.Int64
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				w.Record()
				seen.Add(int64(w.Count()))
			}
		}()
	}
	wg.Wait()
	if seen.Load() == 0 {
		t.Fatalf("expected non-zero sampled counts")
	}
}

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
