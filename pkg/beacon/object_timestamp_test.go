package beacon

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// TestDHMZulu pins the DDHHMMz formatting, including zero-padding of
// single-digit day/hour/minute.
func TestDHMZulu(t *testing.T) {
	cases := []struct {
		t    time.Time
		want string
	}{
		{time.Date(2026, 6, 28, 14, 5, 0, 0, time.UTC), "281405z"},
		{time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC), "020304z"},
		{time.Date(2026, 12, 31, 23, 59, 0, 0, time.UTC), "312359z"},
	}
	for _, c := range cases {
		if got := DHMZulu(c.t); got != c.want {
			t.Errorf("DHMZulu(%s) = %q, want %q", c.t, got, c.want)
		}
	}
}

// TestBuildObjectInfoUsesClockTimestamp is the regression guard for
// issue #412: object beacons must carry a real DDHHMMz timestamp derived
// from the current (UTC) time, not the fixed "111111z" placeholder that
// APRS-IS / APRS.fi reject as out-of-order. It also confirms the clock's
// local time is converted to UTC before formatting.
func TestBuildObjectInfoUsesClockTimestamp(t *testing.T) {
	// 09:05 in UTC-5 is 14:05 UTC on the 28th -> "281405z".
	est := time.FixedZone("EST", -5*3600)
	clk := newFakeClock(time.Date(2026, 6, 28, 9, 5, 0, 0, est))
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	s, err := New(Options{Sink: newMockSink(1), Clock: clk, Logger: logger})
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	info, err := s.buildInfo(context.Background(), Config{
		Type:       TypeObject,
		ObjectName: "FIELD DAY",
		Lat:        43.353,
		Lon:        -87.975,
	})
	if err != nil {
		t.Fatalf("buildInfo: %v", err)
	}

	if strings.Contains(info, "111111z") {
		t.Errorf("object info still uses the fixed 111111z placeholder: %q", info)
	}
	if !strings.Contains(info, "281405z") {
		t.Errorf("object info missing expected UTC timestamp 281405z: %q", info)
	}
	// The 9-char name (with its internal space) must survive intact —
	// the other half of issue #412 was a non-bug, so guard against a
	// regression that would truncate it.
	if !strings.HasPrefix(info, ";FIELD DAY*281405z") {
		t.Errorf("unexpected object header: %q", info)
	}
	// And it must still be a parseable object report.
	pkt, err := aprs.ParseInfo([]byte(info))
	if err != nil {
		t.Fatalf("parse: %v (%q)", err, info)
	}
	if pkt.Object == nil {
		t.Fatalf("not decoded as an object: %q", info)
	}
}
