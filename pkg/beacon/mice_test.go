package beacon

import (
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

// TestMicEPositionInfo_MessageCode_IsOffDuty locks in the spec-correct
// message code per APRS101 ch 10 table 8: bits ABC = 111 (decimal 7)
// = M0 = Off Duty. An earlier draft set MicEMessageOffDuty = 0, which
// transmits as bits ABC = 000 = M7 = Emergency. The graywolf parser
// had a symmetrically inverted label table so internal round-trips
// silently agreed -- but every spec-correct decoder (FAP, aprs.fi,
// direwolf, YAAC) flagged graywolf beacons as Emergency. This test
// would have caught the bug.
func TestMicEPositionInfo_MessageCode_IsOffDuty(t *testing.T) {
	info := MicEPositionInfo(37.4092, -122.1404, 0, 0, 0, '/', '>', false, 0, "")
	destCall := MicEDestination(37.4092, -122.1404, 0)
	destAddr, err := ax25.ParseAddress(destCall)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", destCall, err)
	}
	srcAddr, _ := ax25.ParseAddress("N0CALL")
	frame, err := ax25.NewUIFrame(srcAddr, destAddr, nil, []byte(info))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	p, err := aprs.Parse(frame)
	if err != nil {
		t.Fatalf("aprs.Parse: %v", err)
	}
	if p.MicE == nil {
		t.Fatalf("no Mic-E parsed: %+v", p)
	}
	if p.MicE.MessageCode != 7 {
		t.Errorf("MessageCode = %d, want 7 (M0 = Off Duty per APRS101 ch 10)", p.MicE.MessageCode)
	}
	if p.MicE.MessageText != "Off Duty" {
		t.Errorf("MessageText = %q, want %q", p.MicE.MessageText, "Off Duty")
	}
}

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

// TestMicEPositionInfo_AmbiguityRoundTrip exercises ambiguity levels
// 1..4 end to end: build a Mic-E frame with the new encoder, parse it
// through aprs.Parse, and confirm the position decodes without error
// at the expected precision.
//
// Regression test for a 2026-05-29 NW5W-5 Suncrest digi beacon that
// aprs.fi rejected as "Invalid characters in mic-e information
// field" because the encoder was emitting ASCII space (0x20) at the
// blanked longitude positions. APRS101 ch 10 specifies ambiguity
// only via the destination's K/L/Z variants -- the info-field
// longitude bytes always carry numeric value bytes, and the receiver
// is responsible for discarding trailing minute digits based on the
// destination level. Emitting 0x20 there was a spec violation on our
// part; every value-byte-only parser (including FAP, the one aprs.fi
// uses) correctly rejected the frame.
func TestMicEPositionInfo_AmbiguityRoundTrip(t *testing.T) {
	// Suncrest-ish coords from the originally failing packet.
	const lat = 40.4756
	const lon = -111.8456
	srcAddr, _ := ax25.ParseAddress("N0CALL-5")
	// Tolerance is generous enough to swallow truncation rounding plus
	// the encoder's normal sub-hundredth quantization. Maps to
	// roughly "the visible precision at the given ambiguity level".
	tolByLevel := []float64{0.001, 0.005, 0.05, 0.5, 5.0}
	for level := 0; level <= 4; level++ {
		info := MicEPositionInfo(lat, lon, 0, 0, 0, '/', '>', false, level, "")
		// APRS101 ch 10 says the longitude info-field bytes are value
		// bytes only (no ambiguity space-blanking in the info field).
		// Anything in 0x1c..0x20 is a spec violation here.
		for i := 1; i <= 3; i++ {
			if info[i] == ' ' {
				t.Errorf("level %d: info byte %d is ASCII space (per APRS101 ch 10 these bytes are value-only)", level, i)
			}
		}
		destCall := MicEDestination(lat, lon, level)
		destAddr, err := ax25.ParseAddress(destCall)
		if err != nil {
			t.Fatalf("level %d: ParseAddress(%q): %v", level, destCall, err)
		}
		frame, err := ax25.NewUIFrame(srcAddr, destAddr, nil, []byte(info))
		if err != nil {
			t.Fatalf("level %d: NewUIFrame: %v", level, err)
		}
		p, err := aprs.Parse(frame)
		if err != nil {
			t.Fatalf("level %d: aprs.Parse: %v (dest=%q info=%q)", level, err, destCall, info)
		}
		if p.MicE == nil || p.Position == nil {
			t.Fatalf("level %d: no Mic-E position parsed: %+v", level, p)
		}
		if absf(p.Position.Latitude-lat) > tolByLevel[level] {
			t.Errorf("level %d: lat got %v want ~%v (tol %v)", level, p.Position.Latitude, lat, tolByLevel[level])
		}
		if absf(p.Position.Longitude-lon) > tolByLevel[level] {
			t.Errorf("level %d: lon got %v want ~%v (tol %v)", level, p.Position.Longitude, lon, tolByLevel[level])
		}
	}
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}
