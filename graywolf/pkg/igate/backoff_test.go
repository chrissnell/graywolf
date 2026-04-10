package igate

import (
	"testing"
	"time"
)

func TestBackoffExponentialSchedule(t *testing.T) {
	b := newBackoff(1)
	// First value is 1s+jitter, then 2s, 4s, 8s, ...
	last := time.Duration(0)
	expected := []time.Duration{1 * time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second, 16 * time.Second}
	for i, base := range expected {
		d := b.Next()
		if d < base || d > base+base/4 {
			t.Fatalf("attempt %d: expected %s..%s, got %s", i, base, base+base/4, d)
		}
		if d <= last && i > 0 {
			t.Fatalf("attempt %d: backoff did not grow (%s <= %s)", i, d, last)
		}
		last = d
	}
}

func TestBackoffCapsAtFiveMinutes(t *testing.T) {
	b := newBackoff(2)
	var d time.Duration
	for i := 0; i < 20; i++ {
		d = b.Next()
	}
	if d < backoffMax || d > backoffMax+backoffMax/4 {
		t.Fatalf("expected cap ~%s, got %s", backoffMax, d)
	}
}

func TestBackoffResetReturnsToInitial(t *testing.T) {
	b := newBackoff(3)
	for i := 0; i < 5; i++ {
		b.Next()
	}
	b.Reset()
	d := b.Next()
	if d < time.Second || d > time.Second+time.Second/4 {
		t.Fatalf("after reset expected 1s..1.25s, got %s", d)
	}
}
