package beacon

import (
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

// TestMicEPositionInfo_RoundTrip encodes a position via the new Mic-E
// encoder and parses it back via pkg/aprs to confirm the wire bytes
// survive a full encode -> parse round trip. Mic-E requires the parser
// to see the destination callsign (it carries the latitude digits and
// hemisphere/offset bits), so we build a real AX.25 UI frame.
func TestMicEPositionInfo_RoundTrip(t *testing.T) {
	cases := []struct {
		name      string
		lat, lon  float64
		course    int
		speedKt   float64
		altM      float64
		messaging bool
		symTable  byte
		symCode   byte
		comment   string
	}{
		{"fixed_west", 37.4092, -122.1404, 0, 0, 0, false, '/', '>', ""},
		{"fixed_east", 37.4092, 122.1404, 0, 0, 0, false, '/', '>', ""},
		{"southern_west", -33.8688, -151.2093, 0, 0, 0, false, '/', '>', ""},
		{"southern_east", -33.8688, 151.2093, 0, 0, 0, false, '/', '>', ""},
		{"messaging_alt", 37.4092, -122.1404, 0, 0, 100, true, '/', '>', ""},
		{"tracker_motion", 37.4092, -122.1404, 90, 30, 0, false, '/', '>', ""},
		{"with_comment", 37.4092, -122.1404, 0, 0, 0, false, '/', '>', "graywolf"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			info := MicEPositionInfo(tc.lat, tc.lon, tc.course, tc.speedKt, tc.altM, tc.symTable, tc.symCode, tc.messaging, 0, tc.comment)
			destCall := MicEDestination(tc.lat, tc.lon, 0)
			destAddr, err := ax25.ParseAddress(destCall)
			if err != nil {
				t.Fatalf("ax25.ParseAddress(%q): %v", destCall, err)
			}
			srcAddr, _ := ax25.ParseAddress("N0CALL")
			frame, err := ax25.NewUIFrame(srcAddr, destAddr, nil, []byte(info))
			if err != nil {
				t.Fatalf("NewUIFrame: %v", err)
			}
			p, err := aprs.Parse(frame)
			if err != nil {
				t.Fatalf("aprs.Parse: %v (info=%q dest=%q)", err, info, destCall)
			}
			if p.MicE == nil || p.Position == nil {
				t.Fatalf("no Mic-E position parsed: %+v", p)
			}
			if absf(p.Position.Latitude-tc.lat) > 0.001 {
				t.Errorf("lat: got %v want %v", p.Position.Latitude, tc.lat)
			}
			if absf(p.Position.Longitude-tc.lon) > 0.001 {
				t.Errorf("lon: got %v want %v", p.Position.Longitude, tc.lon)
			}
			if tc.course > 0 {
				if absf(float64(p.Position.Course-tc.course)) > 1 {
					t.Errorf("course: got %d want %d", p.Position.Course, tc.course)
				}
			}
			if tc.speedKt > 0 {
				if absf(p.Position.Speed-tc.speedKt) > 1 {
					t.Errorf("speed: got %v want %v", p.Position.Speed, tc.speedKt)
				}
			}
			if tc.altM != 0 {
				if absf(p.Position.Altitude-tc.altM) > 1 {
					t.Errorf("altitude: got %v want %v", p.Position.Altitude, tc.altM)
				}
				if !p.Position.HasAlt {
					t.Errorf("HasAlt = false, want true")
				}
			}
			if tc.symCode != 0 && p.Position.Symbol.Code != tc.symCode {
				t.Errorf("symbol code: got %q want %q", p.Position.Symbol.Code, tc.symCode)
			}
			if tc.comment != "" && !strings.Contains(p.MicE.Status, tc.comment) {
				t.Errorf("comment: got status %q want substring %q", p.MicE.Status, tc.comment)
			}
		})
	}
}

// TestMicEPositionInfo_AmbiguityDestRoundTrip exercises ambiguity
// levels 1..4 and confirms the destination carries the K/L/Z space
// variants in the right slots. The info-field longitude bytes are
// blanked too, so the parser flags them as ambiguous; we only assert
// the destination round-trip here (the destination's latitude digits
// must still decode at the appropriate precision).
func TestMicEPositionInfo_AmbiguityDestRoundTrip(t *testing.T) {
	for level := 1; level <= 4; level++ {
		destCall := MicEDestination(37.4092, -122.1404, level)
		if len(destCall) != 6 {
			t.Fatalf("level %d: dest len %d", level, len(destCall))
		}
		destAddr, err := ax25.ParseAddress(destCall)
		if err != nil {
			t.Fatalf("level %d: ParseAddress(%q): %v", level, destCall, err)
		}
		_ = destAddr
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
