package cot

import "time"

// Clock abstracts time.Now for deterministic scheduler tests. Simpler
// than beacon.Clock (no After) since Scheduler.Run drives itself off a
// time.Ticker rather than sleeping until a computed wake time.
type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }
