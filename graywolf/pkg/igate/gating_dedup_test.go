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

// TestDedupFixedBeaconExtendedWindow covers the [30s, 60s) suppression
// window that applies only to pairs of fixed-station beacons. A
// previous version of this code had the extension inverted, so a
// 45s-old burst-repeated fixed beacon was gated instead of
// suppressed; this test pins the correct behavior.
func TestDedupFixedBeaconExtendedWindow(t *testing.T) {
	d := newDedupCache()
	base := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return base }
	if !d.shouldGate("beacon", true) {
		t.Fatal("first beacon should gate")
	}
	// 45s later, still within the fixed-beacon extended window.
	d.now = func() time.Time { return base.Add(45 * time.Second) }
	if d.shouldGate("beacon", true) {
		t.Fatal("fixed beacon at 45s must still be suppressed")
	}
	// At 60s exactly, the extension lifts and the packet gates.
	d.now = func() time.Time { return base.Add(60 * time.Second) }
	if !d.shouldGate("beacon", true) {
		t.Fatal("fixed beacon at 60s must gate (extension lifts at beaconRepeatAllow)")
	}
}

// TestDedupNonBeaconNotExtended verifies that the extended window
// applies only to fixed beacons, not to general traffic: a non-beacon
// repeat at 45s still gates (the burst window ends at 30s for
// anything not flagged as a stationary position report).
func TestDedupNonBeaconNotExtended(t *testing.T) {
	d := newDedupCache()
	base := time.Unix(1_700_000_000, 0)
	d.now = func() time.Time { return base }
	if !d.shouldGate("pkt", false) {
		t.Fatal("first packet should gate")
	}
	d.now = func() time.Time { return base.Add(45 * time.Second) }
	if !d.shouldGate("pkt", false) {
		t.Fatal("non-beacon at 45s must gate; extension is beacon-only")
	}
}
