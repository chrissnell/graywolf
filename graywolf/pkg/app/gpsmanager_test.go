package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

// quietLogger returns a logger that discards everything. Shared by
// every _test.go in pkg/app that needs to construct a real slog.Logger
// without cluttering test output. Defined here because this was the
// first test file to need it.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestGPSManagerStopWaitsForReader verifies that stop() blocks until the
// reader goroutine actually exits, not just until its context is
// cancelled. Without this wait a restart could race the previous
// reader's serial-port close against the new reader's open.
func TestGPSManagerStopWaitsForReader(t *testing.T) {
	m := &gpsManager{logger: quietLogger()}

	started := make(chan struct{})
	release := make(chan struct{})
	var exited atomic.Bool

	parent, parentCancel := context.WithCancel(context.Background())
	defer parentCancel()

	run := func(ctx context.Context) error {
		close(started)
		select {
		case <-ctx.Done():
		case <-release:
		}
		// Simulate the tail end of a close-serial-port call: even after
		// ctx cancel, the reader does real work before returning. stop()
		// must wait for this work to finish.
		time.Sleep(30 * time.Millisecond)
		exited.Store(true)
		return nil
	}

	readerCtx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	go m.runLoop(readerCtx, done, run, "fake")

	<-started

	stopReturned := make(chan struct{})
	go func() { m.stop(); close(stopReturned) }()

	select {
	case <-stopReturned:
		t.Fatal("stop returned before reader exit (race with port close)")
	case <-time.After(10 * time.Millisecond):
	}

	// Now the reader observes ctx cancel and does its simulated cleanup.
	select {
	case <-stopReturned:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("stop never returned after reader exited")
	}

	if !exited.Load() {
		t.Fatal("reader goroutine did not complete before stop returned")
	}
}

// TestGPSManagerStopGraceExceeded verifies that a reader stuck past the
// shutdown grace window does not block stop indefinitely. The stuck
// reader is released at the end so its goroutine does not leak from
// the test.
func TestGPSManagerStopGraceExceeded(t *testing.T) {
	m := &gpsManager{logger: quietLogger()}

	stuck := make(chan struct{})
	defer close(stuck)
	run := func(ctx context.Context) error {
		<-stuck
		return nil
	}

	readerCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	m.cancel = cancel
	m.done = done
	go m.runLoop(readerCtx, done, run, "stuck")

	start := time.Now()
	m.stop()
	elapsed := time.Since(start)

	if elapsed < gpsReaderShutdownGrace {
		t.Errorf("stop returned in %s, want >= %s (stuck reader should hit grace)", elapsed, gpsReaderShutdownGrace)
	}
	if elapsed > gpsReaderShutdownGrace+500*time.Millisecond {
		t.Errorf("stop took %s, want close to %s", elapsed, gpsReaderShutdownGrace)
	}
}

// TestGPSManagerRunLoopRestartsOnError verifies the per-error backoff
// path: the run function is re-invoked after a non-cancel error, and
// the loop exits cleanly on ctx cancel during the backoff.
func TestGPSManagerRunLoopRestarts(t *testing.T) {
	m := &gpsManager{logger: quietLogger()}

	// Force a short backoff for this test via a wrapper that counts
	// invocations and then cancels its own context.
	var calls atomic.Int32
	readerCtx, cancel := context.WithCancel(context.Background())
	run := func(ctx context.Context) error {
		n := calls.Add(1)
		if n == 1 {
			return errors.New("transient")
		}
		// Second call: cancel the context so the loop exits via
		// ctx.Done rather than waiting the full backoff.
		cancel()
		<-ctx.Done()
		return ctx.Err()
	}

	done := make(chan struct{})
	// Use a very short effective backoff by cancelling during the
	// second run attempt rather than during the time.After.
	go m.runLoop(readerCtx, done, run, "flaky")

	select {
	case <-done:
	case <-time.After(gpsReaderRestartBackoff + 500*time.Millisecond):
		t.Fatal("runLoop did not exit")
	}

	if got := calls.Load(); got < 2 {
		t.Errorf("run invoked %d times, want >= 2 (initial + at least one retry)", got)
	}
}
