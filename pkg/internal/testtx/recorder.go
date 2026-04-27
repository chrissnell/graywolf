// Package testtx provides a shared transmit-sink recorder for tests.
// Every caller of txgovernor.TxSink (kiss, agw, beacon, digipeater)
// previously carried a near-identical mutex+slice "fake sink" in its
// test file; this package is the one canonical implementation.
//
// Recorder satisfies txgovernor.TxSink and collects every Submit
// invocation. It supports an optional per-submit hook so tests that
// need to block on a specific number of frames, or signal a waiter
// channel, can do so without reimplementing the recorder.
package testtx

import (
	"context"
	"sync"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// Capture is one recorded Submit invocation.
type Capture struct {
	Channel uint32
	Frame   *ax25.Frame
	Source  txgovernor.SubmitSource
}

// Recorder is a txgovernor.TxSink that stores every Submit call for
// later inspection. Safe for concurrent use.
type Recorder struct {
	mu       sync.Mutex
	captures []Capture
	// hook, if non-nil, is invoked after each Submit with the freshly
	// appended capture. Called outside the recorder mutex so hooks
	// may block (e.g. send on a waiter channel) without deadlocking
	// accessor methods. Hooks must not re-enter Submit.
	hook func(Capture)
}

// Compile-time assertion that *Recorder implements the canonical
// txgovernor.TxSink interface.
var _ txgovernor.TxSink = (*Recorder)(nil)

// NewRecorder builds an empty Recorder.
func NewRecorder() *Recorder { return &Recorder{} }

// OnSubmit installs a hook invoked after each Submit call with the
// capture that was just appended. Passing nil clears the hook.
// Returns the receiver for chained construction:
//
//	sink := testtx.NewRecorder().OnSubmit(func(c testtx.Capture) {
//	    signal <- struct{}{}
//	})
func (r *Recorder) OnSubmit(fn func(Capture)) *Recorder {
	r.mu.Lock()
	r.hook = fn
	r.mu.Unlock()
	return r
}

// Submit implements txgovernor.TxSink. It records the invocation,
// invokes the installed hook (if any) outside the lock, and returns
// nil. Production sinks that want to simulate errors should build on
// this type rather than replacing it.
func (r *Recorder) Submit(_ context.Context, channel uint32, frame *ax25.Frame, source txgovernor.SubmitSource) error {
	c := Capture{Channel: channel, Frame: frame, Source: source}
	r.mu.Lock()
	r.captures = append(r.captures, c)
	hook := r.hook
	r.mu.Unlock()
	if hook != nil {
		hook(c)
	}
	return nil
}

// Captures returns a copy of every recorded Submit call in order.
func (r *Recorder) Captures() []Capture {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Capture, len(r.captures))
	copy(out, r.captures)
	return out
}

// Frames returns a copy of the captured frames in order. Convenience
// for tests that only care about the payload.
func (r *Recorder) Frames() []*ax25.Frame {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*ax25.Frame, len(r.captures))
	for i, c := range r.captures {
		out[i] = c.Frame
	}
	return out
}

// Len returns the number of captured Submit calls.
func (r *Recorder) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.captures)
}

// Last returns a copy of the most recent capture, or nil if none.
func (r *Recorder) Last() *Capture {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.captures) == 0 {
		return nil
	}
	c := r.captures[len(r.captures)-1]
	return &c
}

// Reset clears the captured history. Useful in table-driven tests
// that want to reuse a recorder across sub-tests.
func (r *Recorder) Reset() {
	r.mu.Lock()
	r.captures = nil
	r.mu.Unlock()
}
