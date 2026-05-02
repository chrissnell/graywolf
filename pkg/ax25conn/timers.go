package ax25conn

import (
	"sync"
	"time"
)

// Clock abstracts time so tests can drive deadlines deterministically.
// AfterFunc schedules f to run once after d, returning a Stopper that
// cancels the pending callback. The fake-clock test harness iterates
// scheduled callbacks on advance().
type Clock interface {
	Now() time.Time
	AfterFunc(d time.Duration, f func()) Stopper
}

// Stopper cancels a pending AfterFunc callback. The Stop() return is
// the same as time.Timer.Stop(): true if the callback had not yet
// fired, false otherwise.
type Stopper interface {
	Stop() bool
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
func (realClock) AfterFunc(d time.Duration, f func()) Stopper {
	return time.AfterFunc(d, f)
}

// timer wraps a single duration with reset/stop semantics matching
// AX.25 v2.2 §4.3.5: reset stops a running timer and starts it from
// zero. The underlying Stopper is replaced on each reset.
type timer struct {
	mu   sync.Mutex
	clk  Clock
	d    time.Duration
	on   bool
	gen  uint64 // bumps on every reset/stop; stale callbacks compare-and-skip
	cancel Stopper
	fire func()
}

func newTimer(clk Clock, d time.Duration, fire func()) *timer {
	return &timer{clk: clk, d: d, fire: fire}
}

// reset stops any running timer and arms a new one for the configured
// duration.
func (t *timer) reset() { t.resetTo(t.d) }

// resetTo arms the timer for the given duration. Used by RTT-adaptive
// T1 (Task 1.6b) to override the configured value per-fire.
func (t *timer) resetTo(d time.Duration) {
	t.mu.Lock()
	t.gen++
	gen := t.gen
	if t.cancel != nil {
		t.cancel.Stop()
	}
	t.on = true
	t.mu.Unlock()

	s := t.clk.AfterFunc(d, func() {
		t.mu.Lock()
		if !t.on || t.gen != gen {
			t.mu.Unlock()
			return
		}
		t.on = false
		t.mu.Unlock()
		if t.fire != nil {
			t.fire()
		}
	})

	t.mu.Lock()
	if t.gen == gen {
		t.cancel = s
	} else {
		// reset/stop ran between Unlock and now; cancel the stale arming.
		s.Stop()
	}
	t.mu.Unlock()
}

// stop cancels the timer if running. Idempotent.
func (t *timer) stop() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !t.on {
		return
	}
	t.on = false
	t.gen++
	if t.cancel != nil {
		t.cancel.Stop()
	}
}

// running reports whether the timer is currently armed.
func (t *timer) running() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.on
}
