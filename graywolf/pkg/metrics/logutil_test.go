package metrics

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
	"time"
)

// TestRateLimitedLogger_SuppressesWithinInterval verifies that a second
// call for the same key inside the interval returns false and does not
// emit, and that a later call for the same key after the interval has
// elapsed emits again.
func TestRateLimitedLogger_SuppressesWithinInterval(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	r := NewRateLimitedLogger(time.Second)
	base := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { return base }

	if !r.Log(logger, slog.LevelWarn, "k", "first") {
		t.Fatalf("first call should have emitted")
	}
	if r.Log(logger, slog.LevelWarn, "k", "second") {
		t.Fatalf("second call within interval should have been suppressed")
	}

	// Advance past the window — the next call emits.
	r.now = func() time.Time { return base.Add(2 * time.Second) }
	if !r.Log(logger, slog.LevelWarn, "k", "third") {
		t.Fatalf("call after interval should have emitted")
	}

	out := buf.String()
	if !strings.Contains(out, "first") || !strings.Contains(out, "third") {
		t.Errorf("expected first and third in output, got %q", out)
	}
	if strings.Contains(out, "second") {
		t.Errorf("suppressed message leaked to output: %q", out)
	}
}

// TestRateLimitedLogger_KeysAreIndependent verifies that rate limiting
// is scoped per key, so a drop at one site does not mute a drop at
// another. This is the reason the helper keys by string instead of
// keeping a single timestamp.
func TestRateLimitedLogger_KeysAreIndependent(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))

	r := NewRateLimitedLogger(time.Minute)
	base := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { return base }

	if !r.Log(logger, slog.LevelWarn, "a", "alpha") {
		t.Fatal("key a first call should emit")
	}
	if !r.Log(logger, slog.LevelWarn, "b", "beta") {
		t.Fatal("key b first call should emit despite concurrent a")
	}
	if r.Log(logger, slog.LevelWarn, "a", "alpha again") {
		t.Fatal("key a repeat should be suppressed")
	}
}

// TestRateLimitedLogger_NilLoggerStillUpdatesWindow verifies that a
// caller passing a nil logger still advances the rate window, so a
// subsequent call with a real logger inside the same interval still
// sees suppression. This matters when the component's logger is
// optional.
func TestRateLimitedLogger_NilLoggerStillUpdatesWindow(t *testing.T) {
	r := NewRateLimitedLogger(time.Second)
	base := time.Unix(1_700_000_000, 0)
	r.now = func() time.Time { return base }

	if !r.Log(nil, slog.LevelWarn, "k", "msg") {
		t.Fatal("first call with nil logger should still return true")
	}
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	if r.Log(logger, slog.LevelWarn, "k", "msg") {
		t.Fatal("second call must observe the window set by the nil-logger call")
	}
	if buf.Len() != 0 {
		t.Errorf("expected no output, got %q", buf.String())
	}
}

// TestRateLimitedLogger_ZeroIntervalNeverSuppresses verifies that a
// non-positive interval disables rate limiting (useful in tests where
// every event must be observed).
func TestRateLimitedLogger_ZeroIntervalNeverSuppresses(t *testing.T) {
	r := NewRateLimitedLogger(0)
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	for i := 0; i < 5; i++ {
		if !r.Log(logger, slog.LevelInfo, "k", "msg") {
			t.Fatalf("call %d should have emitted with zero interval", i)
		}
	}
	if c := strings.Count(buf.String(), "msg="); c != 5 {
		t.Errorf("expected 5 emissions, got %d (%q)", c, buf.String())
	}
}
