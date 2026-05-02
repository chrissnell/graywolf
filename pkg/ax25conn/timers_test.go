package ax25conn

import (
	"sort"
	"sync"
	"testing"
	"time"
)

// fakeClock implements Clock with manual time advancement. Used across
// pkg/ax25conn tests to drive timer expiries without real-time waits.
type fakeClock struct {
	mu      sync.Mutex
	now     time.Time
	pending []*fakeTimer
	next    uint64 // monotonic id; later-armed timers fire after earlier ones at the same instant
}

type fakeTimer struct {
	id     uint64
	clk    *fakeClock
	due    time.Time
	f      func()
	live   bool
	cancel bool
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Unix(0, 0)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) AfterFunc(d time.Duration, f func()) Stopper {
	c.mu.Lock()
	c.next++
	t := &fakeTimer{id: c.next, clk: c, due: c.now.Add(d), f: f, live: true}
	c.pending = append(c.pending, t)
	c.mu.Unlock()
	return t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	due := make([]*fakeTimer, 0, len(c.pending))
	keep := c.pending[:0]
	for _, t := range c.pending {
		if !t.live || t.cancel {
			continue
		}
		if !t.due.After(c.now) {
			due = append(due, t)
			t.live = false
			continue
		}
		keep = append(keep, t)
	}
	c.pending = keep
	c.mu.Unlock()
	// Fire in deterministic order: earliest deadline first, ties broken by id.
	sort.Slice(due, func(i, j int) bool {
		if due[i].due.Equal(due[j].due) {
			return due[i].id < due[j].id
		}
		return due[i].due.Before(due[j].due)
	})
	for _, t := range due {
		t.f()
	}
}

func (t *fakeTimer) Stop() bool {
	t.clk.mu.Lock()
	defer t.clk.mu.Unlock()
	wasLive := t.live && !t.cancel
	t.cancel = true
	t.live = false
	return wasLive
}

func TestFakeClockBasic(t *testing.T) {
	clk := newFakeClock()
	fired := make(chan struct{}, 1)
	clk.AfterFunc(100*time.Millisecond, func() { fired <- struct{}{} })
	clk.advance(50 * time.Millisecond)
	select {
	case <-fired:
		t.Fatal("fired too soon")
	default:
	}
	clk.advance(60 * time.Millisecond)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("did not fire after deadline")
	}
}

func TestTimerResetExpiry(t *testing.T) {
	clk := newFakeClock()
	fired := make(chan struct{}, 1)
	tm := newTimer(clk, 100*time.Millisecond, func() { fired <- struct{}{} })
	tm.reset()
	if !tm.running() {
		t.Fatal("expected timer running after reset")
	}
	clk.advance(50 * time.Millisecond)
	select {
	case <-fired:
		t.Fatal("fired too soon")
	default:
	}
	clk.advance(60 * time.Millisecond)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("did not fire")
	}
	if tm.running() {
		t.Fatal("running flag must clear after fire")
	}
}

func TestTimerStopBeforeFire(t *testing.T) {
	clk := newFakeClock()
	fired := make(chan struct{}, 1)
	tm := newTimer(clk, 100*time.Millisecond, func() { fired <- struct{}{} })
	tm.reset()
	tm.stop()
	clk.advance(200 * time.Millisecond)
	select {
	case <-fired:
		t.Fatal("must not fire after stop")
	case <-time.After(20 * time.Millisecond):
	}
	if tm.running() {
		t.Fatal("running flag must clear on stop")
	}
}

func TestTimerResetRestartsDeadline(t *testing.T) {
	clk := newFakeClock()
	fired := make(chan struct{}, 4)
	tm := newTimer(clk, 100*time.Millisecond, func() { fired <- struct{}{} })
	tm.reset()
	clk.advance(80 * time.Millisecond)
	tm.reset() // pushes deadline another 100ms
	clk.advance(80 * time.Millisecond)
	select {
	case <-fired:
		t.Fatal("must not fire — reset extended deadline")
	default:
	}
	clk.advance(40 * time.Millisecond)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("did not fire after extended deadline")
	}
	if got := len(fired); got != 0 {
		t.Fatalf("expected exactly one fire, got %d remaining drains", got)
	}
}

func TestTimerResetToOverridesDuration(t *testing.T) {
	clk := newFakeClock()
	fired := make(chan struct{}, 1)
	tm := newTimer(clk, 100*time.Millisecond, func() { fired <- struct{}{} })
	tm.resetTo(20 * time.Millisecond)
	clk.advance(25 * time.Millisecond)
	select {
	case <-fired:
	case <-time.After(time.Second):
		t.Fatal("resetTo deadline did not fire")
	}
}
