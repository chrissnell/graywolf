package kiss

import (
	"sync"
	"sync/atomic"
	"time"
)

// Clock abstracts time for deterministic tests. Only Now is required;
// the rate limiter refills lazily based on wall-time deltas rather than
// goroutine-driven tickers, so no After/Timer surface is needed.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

// RateLimiter is a token-bucket limiter sized for per-interface KISS-TNC
// ingress caps. It refills lazily on each Allow call so no goroutines or
// tickers are required — a dormant interface costs nothing.
//
// A rate of zero OR a burst of zero disables limiting entirely: Allow
// always returns true and Dropped stays at zero. This matches the
// configstore convention where zero means "use defaults" higher up and
// an explicitly-unlimited path through the server for unit tests.
type RateLimiter struct {
	rate  float64 // tokens per second; 0 = unlimited
	burst float64 // bucket capacity; 0 = unlimited
	clock Clock
	mu    sync.Mutex
	// tokens and last are guarded by mu. tokens is a float so fractional
	// refill survives sub-second Allow spacing without rounding bias.
	tokens  float64
	last    time.Time
	dropped atomic.Uint64
}

// NewRateLimiter builds a RateLimiter with the given steady-state rate
// (tokens/sec) and burst (bucket capacity). If either is zero the
// limiter runs in unlimited mode. clock defaults to wall time if nil.
func NewRateLimiter(rateHz, burst uint32, clock Clock) *RateLimiter {
	if clock == nil {
		clock = realClock{}
	}
	r := &RateLimiter{
		rate:  float64(rateHz),
		burst: float64(burst),
		clock: clock,
	}
	if rateHz != 0 && burst != 0 {
		r.tokens = float64(burst)
		r.last = clock.Now()
	}
	return r
}

// Allow consumes one token. Returns true on success, false on drop.
// A dropped call increments the counter exposed by Dropped. Unlimited
// limiters (zero rate or zero burst) always return true and never
// increment the counter.
func (r *RateLimiter) Allow() bool {
	if r.rate == 0 || r.burst == 0 {
		return true
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	now := r.clock.Now()
	if elapsed := now.Sub(r.last); elapsed > 0 {
		r.tokens += elapsed.Seconds() * r.rate
		if r.tokens > r.burst {
			r.tokens = r.burst
		}
		r.last = now
	}
	if r.tokens < 1 {
		r.dropped.Add(1)
		return false
	}
	r.tokens--
	return true
}

// Dropped returns the total number of Allow calls that have been denied
// by this limiter since construction. Atomic; safe from any goroutine.
func (r *RateLimiter) Dropped() uint64 { return r.dropped.Load() }
