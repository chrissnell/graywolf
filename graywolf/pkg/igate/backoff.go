package igate

import (
	"math/rand"
	"time"
)

// Backoff parameters for APRS-IS reconnect: 1s, 2s, 4s, ..., capped at 5min.
const (
	backoffInitial = time.Second
	backoffMax     = 5 * time.Minute
)

// backoff is an exponential reconnect backoff with additive jitter. It
// is not safe for concurrent use; each connection manager owns one.
type backoff struct {
	current time.Duration
	rng     *rand.Rand
}

func newBackoff(seed int64) *backoff {
	return &backoff{
		current: 0,
		rng:     rand.New(rand.NewSource(seed)),
	}
}

// Reset clears the backoff to its initial state, called after a
// successful login handshake.
func (b *backoff) Reset() {
	b.current = 0
}

// Next returns the next delay and advances the schedule. The first
// call returns backoffInitial; subsequent calls double until the cap,
// then stay at the cap. Jitter of up to 25% is added on top.
func (b *backoff) Next() time.Duration {
	if b.current == 0 {
		b.current = backoffInitial
	} else {
		b.current *= 2
		if b.current > backoffMax {
			b.current = backoffMax
		}
	}
	jitter := time.Duration(b.rng.Int63n(int64(b.current / 4)))
	return b.current + jitter
}
