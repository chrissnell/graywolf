package igate

import (
	"time"

	"github.com/chrissnell/graywolf/pkg/internal/dedup"
)

// dedupWindow is the default suppression window for RF->IS gating.
const dedupWindow = 30 * time.Second

// beaconRepeatAllow is the lower-bound interval after which a fixed
// position beacon from the same source+payload is NOT suppressed, per
// the spec: fixed-station beacons >1min apart are not suppressed.
const beaconRepeatAllow = time.Minute

// dedupCache implements the RF->IS duplicate suppression rule on top
// of the shared dedup.Window. The iGate has a quirk the other dedup
// call sites do not: if both the previous and current observation of
// a key are fixed-station position beacons and they are spaced far
// enough apart, normal suppression is waived. That exemption lives
// here at the call site rather than in the shared Window so the
// shared type stays simple. The Window's per-entry value type holds
// the "was fixed beacon" bit we need.
type dedupCache struct {
	w   *dedup.Window[string, bool]
	now func() time.Time
}

func newDedupCache() *dedupCache {
	c := &dedupCache{now: time.Now}
	// Route the Window's clock through c.now so tests can override it
	// in one place and keep the Window's stored timestamps in sync
	// with the caller's age calculations.
	c.w = dedup.New[string, bool](dedup.Config{
		TTL: dedupWindow,
		Now: func() time.Time { return c.now() },
	})
	return c
}

// shouldGate reports whether a packet with the given dedup key should
// be forwarded. fixedBeacon indicates the packet is a stationary
// position report (no course/speed, no message/telemetry), which is
// exempted from the short window if the last send was more than
// beaconRepeatAllow ago.
func (d *dedupCache) shouldGate(key string, fixedBeacon bool) bool {
	prevFixed, prevWhen, seen := d.w.Peek(key)
	now := d.now()
	if !seen {
		d.w.Record(key, fixedBeacon)
		return true
	}
	age := now.Sub(prevWhen)
	// Fixed beacon exception: re-gate if the last send was longer ago
	// than the beacon allow interval, regardless of the 30s window.
	if fixedBeacon && prevFixed && age >= beaconRepeatAllow {
		d.w.Record(key, true)
		return true
	}
	if age < dedupWindow {
		return false
	}
	d.w.Record(key, fixedBeacon)
	return true
}
