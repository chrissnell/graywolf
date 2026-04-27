// Package backoff provides an exponential backoff schedule with
// optional additive jitter and a configurable cap. It is designed to
// drive reconnect loops (APRS-IS client) and restart supervisors
// (modembridge child process), which previously each carried their
// own nearly-identical implementation.
//
// A Backoff is not safe for concurrent use; each caller owns one.
package backoff

import (
	"math"
	"math/rand"
	"time"
)

// Config describes one Backoff schedule.
type Config struct {
	// Initial is the first delay returned by Next. Required; a
	// non-positive value panics in New.
	Initial time.Duration
	// Max caps the schedule. Once the growing delay reaches Max it
	// stays there. Zero means "unbounded" (capped at math.MaxInt64).
	Max time.Duration
	// Factor is the growth multiplier applied after each Next call.
	// Zero defaults to 2.0 (classic "double each time").
	Factor float64
	// JitterFrac is the upper bound of additive jitter as a fraction
	// of the current delay: the returned value is current +
	// Uniform[0, current*JitterFrac). Zero disables jitter entirely.
	// Must be in [0, 1); values outside that range panic.
	JitterFrac float64
	// Rand is the random source used for jitter. Ignored when
	// JitterFrac is 0. When nil and JitterFrac > 0, a time-seeded
	// source is created in New; tests that want deterministic jitter
	// should supply their own source.
	Rand *rand.Rand
}

// Backoff is the stateful schedule. The zero value is not usable;
// call New.
type Backoff struct {
	initial time.Duration
	max     time.Duration
	factor  float64
	jitter  float64
	rand    *rand.Rand

	// next is the un-jittered delay that the next Next() call will
	// return. It is updated after each call by multiplying by Factor
	// (capped at Max). Reset snaps it back to Initial.
	next time.Duration
}

// New builds a Backoff from cfg. Panics on an invalid configuration
// so callers discover the problem at startup rather than after a
// failure already occurred.
func New(cfg Config) *Backoff {
	if cfg.Initial <= 0 {
		panic("backoff: non-positive Initial")
	}
	if cfg.JitterFrac < 0 || cfg.JitterFrac >= 1 {
		panic("backoff: JitterFrac must be in [0, 1)")
	}
	factor := cfg.Factor
	if factor == 0 {
		factor = 2
	}
	if factor < 1 {
		panic("backoff: Factor must be >= 1 (or 0 for default 2)")
	}
	max := cfg.Max
	if max <= 0 {
		max = time.Duration(math.MaxInt64)
	}
	rnd := cfg.Rand
	if rnd == nil && cfg.JitterFrac > 0 {
		rnd = rand.New(rand.NewSource(time.Now().UnixNano()))
	}
	return &Backoff{
		initial: cfg.Initial,
		max:     max,
		factor:  factor,
		jitter:  cfg.JitterFrac,
		rand:    rnd,
		next:    cfg.Initial,
	}
}

// Next returns the current delay, then advances the schedule. The
// first call returns Initial (plus jitter); each subsequent call
// multiplies the un-jittered delay by Factor, capped at Max.
func (b *Backoff) Next() time.Duration {
	d := b.next
	// Advance for the next call, capped at Max.
	advance := time.Duration(float64(b.next) * b.factor)
	if advance <= 0 || advance > b.max {
		advance = b.max
	}
	b.next = advance
	// Jitter is applied on the value we are returning, using the
	// un-jittered delay as the base so the schedule itself stays
	// strictly exponential.
	if b.jitter > 0 && b.rand != nil {
		maxJitter := int64(float64(d) * b.jitter)
		if maxJitter > 0 {
			d += time.Duration(b.rand.Int63n(maxJitter))
		}
	}
	return d
}

// Reset clears the backoff so the next Next call returns Initial
// again. Use after a successful operation so the next failure starts
// over from the bottom of the schedule.
func (b *Backoff) Reset() {
	b.next = b.initial
}
