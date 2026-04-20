// Package testsync supplies minimal synchronisation helpers for tests
// that must wait on real-world, non-logical-time events (TCP accept,
// goroutine dispatch, counter updates driven by background workers).
// For logical-time waits (rate limiter refill, scheduler ticks) inject
// a fake clock instead — see pkg/beacon/scheduler.go.
package testsync

import (
	"testing"
	"time"
)

// pollInterval is the wake-up cadence inside WaitFor. Small enough that
// a condition met quickly after a prior sleep is noticed within ~2ms;
// large enough that a long wait doesn't dominate CPU with busy-polling.
const pollInterval = 2 * time.Millisecond

// WaitFor polls cond until it returns true or timeout elapses. On
// timeout it fails the test with t.Fatalf, naming the wait site via
// what so the diagnostic pinpoints the missing precondition. Callers
// should pass a short description ("tcp listener bound", "client
// registered", "n captures recorded") rather than the literal code.
func WaitFor(t testing.TB, cond func() bool, timeout time.Duration, what string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("timeout after %v waiting for %s", timeout, what)
}
