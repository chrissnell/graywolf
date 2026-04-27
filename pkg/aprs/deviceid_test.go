package aprs

import (
	"math"
	"testing"
)

func TestLookupTocall(t *testing.T) {
	tests := []struct {
		dest  string
		model string
	}{
		{"APMI06", "WX3in1 Plus 2.0"},
		{"APMI04", "WX3in1 Mini"},
		{"APMI99", ""},          // matches APMI?? wildcard, no model on wildcard entry
		{"APDW15", "DireWolf"},  // APDW??
		{"APK004", "TH-D74"},   // exact
		{"APK099", "TH-D7"},    // APK0?? wildcard
		{"APTT42", "TinyTrak"}, // APTT* star-wildcard
		{"APY300", "FTM-300D"}, // exact Yaesu
		{"XXXYYY", ""},         // no match
		{"APRS", "Unknown"},    // the bare APRS tocall
	}

	for _, tt := range tests {
		info := LookupTocall(tt.dest)
		if tt.model == "" {
			if info != nil && info.Model != "" {
				t.Errorf("LookupTocall(%q) = %q, want no model", tt.dest, info.Model)
			}
			continue
		}
		if info == nil {
			t.Errorf("LookupTocall(%q) = nil, want model=%q", tt.dest, tt.model)
			continue
		}
		if info.Model != tt.model {
			t.Errorf("LookupTocall(%q).Model = %q, want %q", tt.dest, info.Model, tt.model)
		}
	}
}

func TestLookupTocallStripSSID(t *testing.T) {
	info := LookupTocall("APMI06-5")
	if info == nil || info.Model != "WX3in1 Plus 2.0" {
		t.Errorf("LookupTocall with SSID failed: got %v", info)
	}
}

func TestHaversineDistanceMi(t *testing.T) {
	// Roughly 69 miles per degree of latitude at equator
	dist := HaversineDistanceMi(0, 0, 1, 0)
	if math.Abs(dist-69.1) > 0.5 {
		t.Errorf("HaversineDistanceMi(0,0,1,0) = %f, want ~69.1", dist)
	}

	// Zero distance
	dist = HaversineDistanceMi(40.0, -111.0, 40.0, -111.0)
	if dist != 0 {
		t.Errorf("same point distance = %f, want 0", dist)
	}
}
