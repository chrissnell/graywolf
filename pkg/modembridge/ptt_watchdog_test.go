package modembridge

import (
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// TestPttWatchdog_FiresAfterTimeout: key a channel and wait past the timeout;
// the watchdog must invoke the unkey closure exactly once.
func TestPttWatchdog_FiresAfterTimeout(t *testing.T) {
	var calls atomic.Int32
	w := newPttWatchdog(50*time.Millisecond, func(ch uint32) error {
		calls.Add(1)
		return nil
	}, discardLogger())

	w.onKey(1)
	time.Sleep(120 * time.Millisecond)

	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 auto-unkey call, got %d", got)
	}
}

// TestPttWatchdog_ResetsOnRepeatedKey: heartbeating every 30ms with a 50ms
// timeout means the watchdog should never fire during the heartbeat window,
// but should fire after the final heartbeat expires.
func TestPttWatchdog_ResetsOnRepeatedKey(t *testing.T) {
	var calls atomic.Int32
	w := newPttWatchdog(50*time.Millisecond, func(ch uint32) error {
		calls.Add(1)
		return nil
	}, discardLogger())

	w.onKey(1)
	time.Sleep(30 * time.Millisecond)
	w.onKey(1)
	time.Sleep(30 * time.Millisecond)
	w.onKey(1)
	time.Sleep(30 * time.Millisecond) // 90ms total, last heartbeat ~30ms ago — should NOT have fired yet

	if got := calls.Load(); got != 0 {
		t.Errorf("expected 0 auto-unkey calls before timeout expires, got %d", got)
	}

	// Wait past the timeout from the last heartbeat.
	time.Sleep(60 * time.Millisecond)

	if got := calls.Load(); got != 1 {
		t.Errorf("expected 1 auto-unkey call after timeout, got %d", got)
	}
}

// TestPttWatchdog_CancelsOnUnkey: key then immediately unkey; wait past the
// timeout. The unkey closure must NOT be called by the timer (it was cancelled).
func TestPttWatchdog_CancelsOnUnkey(t *testing.T) {
	var calls atomic.Int32
	w := newPttWatchdog(50*time.Millisecond, func(ch uint32) error {
		calls.Add(1)
		return nil
	}, discardLogger())

	w.onKey(1)
	w.onUnkey(1)
	time.Sleep(120 * time.Millisecond) // well past timeout

	if got := calls.Load(); got != 0 {
		t.Errorf("expected 0 auto-unkey calls after explicit unkey, got %d", got)
	}
}

// TestPttWatchdog_CancelAll: key two channels, call cancelAll, wait past the
// timeout — the unkey closure must NOT be invoked for either channel.
func TestPttWatchdog_CancelAll(t *testing.T) {
	var calls atomic.Int32
	w := newPttWatchdog(50*time.Millisecond, func(ch uint32) error {
		calls.Add(1)
		return nil
	}, discardLogger())

	w.onKey(1)
	w.onKey(2)
	w.cancelAll()
	time.Sleep(120 * time.Millisecond) // well past timeout

	if got := calls.Load(); got != 0 {
		t.Errorf("expected 0 auto-unkey calls after cancelAll, got %d", got)
	}
}

// TestPttWatchdog_PerChannelIsolation: two channels each get their own timer;
// both must fire independently.
func TestPttWatchdog_PerChannelIsolation(t *testing.T) {
	ch1calls := new(atomic.Int32)
	ch2calls := new(atomic.Int32)
	w := newPttWatchdog(50*time.Millisecond, func(ch uint32) error {
		if ch == 1 {
			ch1calls.Add(1)
		} else if ch == 2 {
			ch2calls.Add(1)
		}
		return nil
	}, discardLogger())

	w.onKey(1)
	w.onKey(2)
	time.Sleep(120 * time.Millisecond)

	if got := ch1calls.Load(); got != 1 {
		t.Errorf("channel 1: expected 1 auto-unkey call, got %d", got)
	}
	if got := ch2calls.Load(); got != 1 {
		t.Errorf("channel 2: expected 1 auto-unkey call, got %d", got)
	}
}
