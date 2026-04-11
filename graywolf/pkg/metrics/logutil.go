package metrics

import (
	"log/slog"
	"sync"
	"time"
)

// RateLimitedLogger emits log messages at most once per interval per key.
// It exists so every "silent drop" site in graywolf can log the first
// occurrence of a drop-kind at warn level without flooding the operator's
// log when a subsystem enters sustained back-pressure.
//
// Thread-safe. The zero value is NOT usable — construct with
// NewRateLimitedLogger. One instance per drop site (not global), stored
// as a field on the owning component, so "rate limit" is scoped to the
// drop kind, not to the whole process.
type RateLimitedLogger struct {
	mu       sync.Mutex
	interval time.Duration
	last     map[string]time.Time
	// now is an injection seam for tests. Production code uses time.Now.
	now func() time.Time
}

// NewRateLimitedLogger returns a logger that allows at most one emission
// per (key, interval). A non-positive interval disables rate limiting —
// every call emits.
func NewRateLimitedLogger(interval time.Duration) *RateLimitedLogger {
	return &RateLimitedLogger{
		interval: interval,
		last:     make(map[string]time.Time),
		now:      time.Now,
	}
}

// Log emits msg via logger at the given level but only if the last
// emission for this key is older than interval ago. Returns true if the
// message was actually emitted. A nil logger is a no-op (but the rate
// window is still updated so a subsequent call with a real logger still
// observes suppression).
func (r *RateLimitedLogger) Log(logger *slog.Logger, level slog.Level, key, msg string, args ...any) bool {
	r.mu.Lock()
	now := r.now()
	if r.interval > 0 {
		if prev, ok := r.last[key]; ok && now.Sub(prev) < r.interval {
			r.mu.Unlock()
			return false
		}
	}
	r.last[key] = now
	r.mu.Unlock()

	if logger == nil {
		return true
	}
	logger.Log(nil, level, msg, args...)
	return true
}
