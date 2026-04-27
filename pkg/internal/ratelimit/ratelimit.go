// Package ratelimit provides a sliding-window event counter. It is a
// usage tracker, not a token bucket: callers record events and ask
// how many events fall inside the configured window ending now.
// Useful for transmit governor rate caps where we want to say "no
// more than N packets/min on this channel" without ever blocking a
// submit call.
package ratelimit

import (
	"sync"
	"time"
)

// Window tracks events over a fixed duration. Not safe for concurrent
// use without the internal lock held; all exported methods take it.
type Window struct {
	mu     sync.Mutex
	window time.Duration
	now    func() time.Time
	events []time.Time
}

// New returns a Window of the given duration using time.Now as its
// clock. Panics if window <= 0.
func New(window time.Duration) *Window {
	return NewWithClock(window, time.Now)
}

// NewWithClock returns a Window with a caller-supplied clock. Panics
// if window <= 0 or now == nil.
func NewWithClock(window time.Duration, now func() time.Time) *Window {
	if window <= 0 {
		panic("ratelimit: non-positive window")
	}
	if now == nil {
		panic("ratelimit: nil clock")
	}
	return &Window{window: window, now: now}
}

// Record adds an event at the current time.
func (w *Window) Record() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = append(w.events, w.now())
}

// Count returns the number of events within the window ending now.
// As a side effect it drops events older than the window so a caller
// that Records frequently does not leak memory.
func (w *Window) Count() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.evictLocked()
	return len(w.events)
}

// Reset clears all recorded events.
func (w *Window) Reset() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.events = nil
}

// evictLocked drops events older than the window. The slice is
// compacted in place because events are appended monotonically.
func (w *Window) evictLocked() {
	if len(w.events) == 0 {
		return
	}
	cutoff := w.now().Add(-w.window)
	i := 0
	for i < len(w.events) && w.events[i].Before(cutoff) {
		i++
	}
	if i == 0 {
		return
	}
	// Shift remaining events to the front to keep the slice bounded.
	n := copy(w.events, w.events[i:])
	w.events = w.events[:n]
}
