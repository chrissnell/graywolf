package beacon

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/gps"
)

func newTestScheduler(t *testing.T) *Scheduler {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	s, err := New(Options{Sink: newMockSink(1), Logger: logger})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return s
}

// TestBuildInfo_QSY_PositionEmitsFreq confirms a position beacon with
// QSY configured (no callsign override) emits the AFRS freq-spec prefix
// in its comment.
func TestBuildInfo_QSY_PositionEmitsFreq(t *testing.T) {
	s := newTestScheduler(t)
	info, err := s.buildInfo(context.Background(), Config{
		Type:         TypePosition,
		Lat:          40.0,
		Lon:          -105.0,
		Format:       "compressed",
		Comment:      "Repeater",
		QSYFreqMHz:   146.520,
		QSYToneType:  "ctcss",
		QSYToneFreq:  "100.0",
		QSYHasOffset: true,
		QSYOffsetMHz: 0.6,
	})
	if err != nil {
		t.Fatalf("buildInfo: %v", err)
	}
	if !strings.Contains(info, "146.520MHz T100 +060") {
		t.Fatalf("expected QSY freq-spec in info, got %q", info)
	}
	pkt, err := aprs.ParseInfo([]byte(info))
	if err != nil {
		t.Fatalf("parse: %v (%q)", err, info)
	}
	if pkt.QSY == nil || pkt.QSY.FrequencyMHz != 146.520 {
		t.Fatalf("QSY not round-trippable from emitted info: %+v", pkt.QSY)
	}
	if pkt.Comment != "Repeater" {
		t.Errorf("comment = %q, want %q (QSY prefix should not leak into comment)", pkt.Comment, "Repeater")
	}
}

// TestBuildInfo_QSY_CallsignOverrideSuppressesFreq is the defense-in-depth
// guard: a position beacon with an overridden callsign must never emit
// QSY, even if QSYFreqMHz is set (e.g. a hand-edited DB row).
func TestBuildInfo_QSY_CallsignOverrideSuppressesFreq(t *testing.T) {
	s := newTestScheduler(t)
	info, err := s.buildInfo(context.Background(), Config{
		Type:               TypePosition,
		Lat:                40.0,
		Lon:                -105.0,
		Format:             "compressed",
		Comment:            "Repeater",
		QSYFreqMHz:         146.520,
		CallsignOverridden: true,
	})
	if err != nil {
		t.Fatalf("buildInfo: %v", err)
	}
	if strings.Contains(info, "MHz") {
		t.Fatalf("QSY must be suppressed for an overridden callsign, got %q", info)
	}
}

// TestBuildInfo_QSY_IGateSuppressesFreq confirms QSY only applies to
// TypePosition, not the combined TypeIGate branch.
func TestBuildInfo_QSY_IGateSuppressesFreq(t *testing.T) {
	s := newTestScheduler(t)
	info, err := s.buildInfo(context.Background(), Config{
		Type:       TypeIGate,
		Lat:        40.0,
		Lon:        -105.0,
		Format:     "compressed",
		Comment:    "status",
		QSYFreqMHz: 146.520,
	})
	if err != nil {
		t.Fatalf("buildInfo: %v", err)
	}
	if strings.Contains(info, "MHz") {
		t.Fatalf("QSY must not apply to igate beacons, got %q", info)
	}
}

// TestBuildInfo_QSY_TrackerSuppressesFreq confirms QSY does not apply to
// trackers (manual QSY entry is restricted to type=="position").
func TestBuildInfo_QSY_TrackerSuppressesFreq(t *testing.T) {
	s := newTestScheduler(t)
	cache := gps.NewMemCache()
	cache.Update(gps.Fix{Latitude: 40.0, Longitude: -105.0})
	s.cache = cache
	info, err := s.buildInfo(context.Background(), Config{
		Type:       TypeTracker,
		Format:     "compressed",
		Comment:    "status",
		QSYFreqMHz: 146.520,
	})
	if err != nil {
		t.Fatalf("buildInfo: %v", err)
	}
	if strings.Contains(info, "MHz") {
		t.Fatalf("QSY must not apply to tracker beacons, got %q", info)
	}
}

// TestBuildInfo_QSY_MicE confirms the Mic-E branch (no data-extension
// slot) still carries QSY via the comment prefix.
func TestBuildInfo_QSY_MicE(t *testing.T) {
	s := newTestScheduler(t)
	info, err := s.buildInfo(context.Background(), Config{
		Type:       TypePosition,
		Lat:        40.0,
		Lon:        -105.0,
		Format:     "mic_e",
		Comment:    "Repeater",
		QSYFreqMHz: 146.520,
	})
	if err != nil {
		t.Fatalf("buildInfo: %v", err)
	}
	if !strings.Contains(info, "146.520MHz") {
		t.Fatalf("expected QSY freq-spec in Mic-E info, got %q", info)
	}
}
