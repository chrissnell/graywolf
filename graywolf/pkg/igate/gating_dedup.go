package igate

import (
	"time"

	"github.com/chrissnell/graywolf/pkg/internal/dedup"
)

// dedupWindow is the default burst-suppression window for RF->IS
// gating: identical packets received within this window are dropped as
// immediate repeats.
const dedupWindow = 30 * time.Second

// beaconRepeatAllow is the extended suppression window that applies
// only when both the previous and current observation of a dedup key
// are fixed-station position beacons (no course/speed, no message,
// no telemetry). Two such beacons closer together than this interval
// are still suppressed; once they are at least this far apart the
// duplicate is gated up to APRS-IS. The intent is "stationary
// beacons >1 minute apart are not suppressed" per the APRS-IS
// convention.
const beaconRepeatAllow = time.Minute

// dedupCache implements the RF->IS duplicate suppression rule on top
// of the shared dedup.Window. The iGate has a quirk the other dedup
// call sites do not: the suppression window is extended from
// dedupWindow to beaconRepeatAllow when both the previous and
// current observation are fixed-station position reports. That
// extension lives here at the call site rather than in the shared
// Window so the shared type stays simple. The Window's per-entry
// value type carries the "was fixed beacon" bit we need, and the
// Window's TTL is set to beaconRepeatAllow so entries survive long
// enough for the extended check to observe them.
type dedupCache struct {
	w   *dedup.Window[string, bool]
	now func() time.Time
}

func newDedupCache() *dedupCache {
	c := &dedupCache{now: time.Now}
	// Route the Window's clock through c.now so tests can override it
	// in one place and keep the Window's stored timestamps in sync
	// with the caller's age calculations. TTL is beaconRepeatAllow
	// (the longer of the two windows) so a 45s-old entry is still
	// present for the extended fixed-beacon check rather than having
	// been evicted as expired.
	c.w = dedup.New[string, bool](dedup.Config{
		TTL: beaconRepeatAllow,
		Now: func() time.Time { return c.now() },
	})
	return c
}

// shouldGate reports whether a packet with the given dedup key should
// be forwarded to APRS-IS. fixedBeacon indicates the current packet
// is a stationary position report. The rule is:
//
//   - First observation of a key: always gate.
//   - Subsequent observation within dedupWindow (30s): suppress.
//   - Subsequent observation between dedupWindow and beaconRepeatAllow
//     (30s–1m), where both the previous and current observation are
//     fixed-station beacons: still suppress.
//   - Otherwise: gate and refresh the entry.
func (d *dedupCache) shouldGate(key string, fixedBeacon bool) bool {
	prevFixed, prevWhen, seen := d.w.Peek(key)
	now := d.now()
	if !seen {
		d.w.Record(key, fixedBeacon)
		return true
	}
	age := now.Sub(prevWhen)
	// Burst window: identical packets within 30s are always suppressed.
	if age < dedupWindow {
		return false
	}
	// Extended window: two fixed-station beacons closer together than
	// beaconRepeatAllow are still suppressed even though the burst
	// window has elapsed. Both the previous and current observation
	// must be fixed beacons for the extension to apply; a mixed
	// sequence (which can only happen if two different packets collide
	// on the same key) falls through to normal gating.
	if fixedBeacon && prevFixed && age < beaconRepeatAllow {
		return false
	}
	d.w.Record(key, fixedBeacon)
	return true
}
