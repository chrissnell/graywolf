package igate

import (
	"sync"
	"time"
)

// dedupWindow is the default suppression window for RF->IS gating.
const dedupWindow = 30 * time.Second

// beaconRepeatAllow is the lower-bound interval after which a fixed
// position beacon from the same source+payload is NOT suppressed. The
// spec ("fixed-station beacons >1min apart NOT suppressed") means the
// dedup window only applies to burst-style repeats within 30s.
const beaconRepeatAllow = time.Minute

// dedupEntry tracks last-seen time and whether the packet was a fixed
// position beacon (which gets the >1min exemption).
type dedupEntry struct {
	last       time.Time
	fixedBeacon bool
}

// dedupCache implements the RF->IS duplicate suppression rule. It is
// safe for concurrent use.
type dedupCache struct {
	mu      sync.Mutex
	entries map[string]dedupEntry
	window  time.Duration
	now     func() time.Time
}

func newDedupCache() *dedupCache {
	return &dedupCache{
		entries: make(map[string]dedupEntry),
		window:  dedupWindow,
		now:     time.Now,
	}
}

// shouldGate reports whether a packet with the given dedup key should be
// forwarded. fixedBeacon indicates the packet is a stationary position
// report (no course/speed, no message/telemetry), which is exempted from
// the short window if the last send was more than beaconRepeatAllow ago.
func (d *dedupCache) shouldGate(key string, fixedBeacon bool) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	now := d.now()
	d.gcLocked(now)
	prev, seen := d.entries[key]
	if !seen {
		d.entries[key] = dedupEntry{last: now, fixedBeacon: fixedBeacon}
		return true
	}
	age := now.Sub(prev.last)
	// Fixed beacon exception: re-gate if the last send was longer ago
	// than the beacon allow interval, regardless of the 30s window.
	if fixedBeacon && prev.fixedBeacon && age >= beaconRepeatAllow {
		d.entries[key] = dedupEntry{last: now, fixedBeacon: true}
		return true
	}
	if age < d.window {
		return false
	}
	d.entries[key] = dedupEntry{last: now, fixedBeacon: fixedBeacon}
	return true
}

func (d *dedupCache) gcLocked(now time.Time) {
	if len(d.entries) < 64 {
		return
	}
	cutoff := d.window
	if beaconRepeatAllow > cutoff {
		cutoff = beaconRepeatAllow
	}
	for k, e := range d.entries {
		if now.Sub(e.last) > cutoff*2 {
			delete(d.entries, k)
		}
	}
}
