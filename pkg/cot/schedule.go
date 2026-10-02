// Package cot implements graywolf's Cursor-on-Target (CoT) scheduler: a
// named/iconed/commented APRS object dropped from the live map that
// transmits immediately, then retransmits a finite number of times on
// a decaying schedule.
//
// Unlike pkg/beacon's in-memory min-heap run loop, the Scheduler here
// is stateless: Run polls the configstore for targets whose NextSendAt
// has arrived, sends them, and persists the next NextSendAt (or nil
// once exhausted). There is no in-memory pending set to keep in sync
// with creates/deletes -- the database is the single source of truth,
// which is deliberately simple given the small number of CoT targets a
// station is expected to have in flight at once.
package cot

import "time"

// ComputeOffsets returns the cumulative time-from-first-send offset for
// each of numTransmits total sends. The confirmed schedule: TX1 fires
// immediately (offset 0); TX2 fires after secondDelay; every later
// TX's offset is the previous TX's offset multiplied by decay. For
// numTransmits=4, secondDelay=300s, decay=2 this returns
// [0, 300s, 600s, 1200s].
//
// numTransmits<=0 returns nil. numTransmits==1 returns [0] (an
// immediate send only, no retransmits).
func ComputeOffsets(numTransmits int, secondDelay time.Duration, decay float64) []time.Duration {
	if numTransmits <= 0 {
		return nil
	}
	offsets := make([]time.Duration, numTransmits)
	// offsets[0] is the zero value already (TX1 is immediate).
	if numTransmits > 1 {
		offsets[1] = secondDelay
	}
	for n := 2; n < numTransmits; n++ {
		offsets[n] = time.Duration(float64(offsets[n-1]) * decay)
	}
	return offsets
}
