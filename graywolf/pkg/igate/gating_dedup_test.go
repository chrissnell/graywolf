package igate

import (
	"testing"
	"time"
)

func TestDedupSuppressesWithinWindow(t *testing.T) {
	d := newDedupCache()
	base := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return base }
	if !d.shouldGate("k", false) {
		t.Fatal("first observation must be gated")
	}
	d.now = func() time.Time { return base.Add(10 * time.Second) }
	if d.shouldGate("k", false) {
		t.Fatal("second observation within 30s must be suppressed")
	}
}

func TestDedupAllowsAfterWindow(t *testing.T) {
	d := newDedupCache()
	base := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return base }
	d.shouldGate("k", false)
	d.now = func() time.Time { return base.Add(31 * time.Second) }
	if !d.shouldGate("k", false) {
		t.Fatal("after 30s+ the packet must be gated again")
	}
}

func TestDedupFixedBeaconExemption(t *testing.T) {
	d := newDedupCache()
	base := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return base }
	if !d.shouldGate("beacon", true) {
		t.Fatal("first beacon should gate")
	}
	// Within 30s is still suppressed even for beacons.
	d.now = func() time.Time { return base.Add(20 * time.Second) }
	if d.shouldGate("beacon", true) {
		t.Fatal("burst beacon within 30s still suppressed")
	}
	// At 61s, the exemption kicks in (>1min).
	d.now = func() time.Time { return base.Add(61 * time.Second) }
	if !d.shouldGate("beacon", true) {
		t.Fatal("fixed beacon >1min apart must not be suppressed")
	}
}

func TestDedupDifferentKeysIndependent(t *testing.T) {
	d := newDedupCache()
	base := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return base }
	if !d.shouldGate("a", false) || !d.shouldGate("b", false) {
		t.Fatal("distinct keys must gate independently")
	}
}
