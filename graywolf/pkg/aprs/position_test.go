package aprs

import (
	"math"
	"testing"
)

func floatClose(a, b, eps float64) bool { return math.Abs(a-b) < eps }

func TestParsePositionUncompressed(t *testing.T) {
	// APRS spec example: "4903.50N/07201.75W-" (car symbol)
	info := []byte("!4903.50N/07201.75W-Test 001")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatalf("ParseInfo: %v", err)
	}
	if pkt.Type != PacketPosition {
		t.Fatalf("type %q want position", pkt.Type)
	}
	if pkt.Position == nil {
		t.Fatal("nil position")
	}
	if !floatClose(pkt.Position.Latitude, 49.0583, 1e-3) {
		t.Errorf("lat %v", pkt.Position.Latitude)
	}
	if !floatClose(pkt.Position.Longitude, -72.0292, 1e-3) {
		t.Errorf("lon %v", pkt.Position.Longitude)
	}
	if pkt.Position.Symbol.Table != '/' || pkt.Position.Symbol.Code != '-' {
		t.Errorf("symbol %q/%q", pkt.Position.Symbol.Table, pkt.Position.Symbol.Code)
	}
	if pkt.Comment != "Test 001" {
		t.Errorf("comment %q", pkt.Comment)
	}
}

func TestParsePositionAltitude(t *testing.T) {
	info := []byte("!4903.50N/07201.75W-/A=001000")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if !pkt.Position.HasAlt {
		t.Fatal("expected altitude")
	}
	// 1000 feet ≈ 304.8 m
	if !floatClose(pkt.Position.Altitude, 304.8, 0.5) {
		t.Errorf("alt %v", pkt.Position.Altitude)
	}
}

func TestParsePositionCourseSpeed(t *testing.T) {
	info := []byte("!4903.50N/07201.75W>088/036")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if !pkt.Position.HasCourse {
		t.Fatal("expected course")
	}
	if pkt.Position.Course != 88 || pkt.Position.Speed != 36 {
		t.Errorf("cse=%d spd=%v", pkt.Position.Course, pkt.Position.Speed)
	}
}

func TestParsePositionWithTimestamp(t *testing.T) {
	info := []byte("@092345z4903.50N/07201.75W-Test")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Position == nil || pkt.Position.Timestamp == nil {
		t.Fatal("expected timestamp")
	}
}

func TestParsePositionAmbiguity(t *testing.T) {
	info := []byte("!4903.  N/07201.  W-")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Position.Ambiguity == 0 {
		t.Errorf("expected non-zero ambiguity")
	}
}

func TestEncodePositionRoundTrip(t *testing.T) {
	p := Position{
		Latitude:  49.0583,
		Longitude: -72.0292,
		Symbol:    Symbol{Table: '/', Code: '-'},
	}
	b, err := EncodePosition(p, false)
	if err != nil {
		t.Fatal(err)
	}
	if b[0] != '!' {
		t.Errorf("prefix %q", b[0])
	}
	pkt, err := ParseInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	if !floatClose(pkt.Position.Latitude, p.Latitude, 1e-3) {
		t.Errorf("lat round-trip %v != %v", pkt.Position.Latitude, p.Latitude)
	}
	if !floatClose(pkt.Position.Longitude, p.Longitude, 1e-3) {
		t.Errorf("lon round-trip %v != %v", pkt.Position.Longitude, p.Longitude)
	}
}

func TestParsePositionCompressed(t *testing.T) {
	// Build a compressed position deterministically via the encoder
	// helpers to verify decode matches.
	lat, lon := 49.5, -72.75
	yx := EncodeCompressedLatLon(lat, lon)
	info := make([]byte, 0, 14)
	info = append(info, '!', '/')
	info = append(info, yx...)
	info = append(info, '>', ' ', ' ', 'A')
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatalf("ParseInfo compressed: %v", err)
	}
	if pkt.Position == nil || !pkt.Position.Compressed {
		t.Fatal("expected compressed position")
	}
	if !floatClose(pkt.Position.Latitude, lat, 1e-3) {
		t.Errorf("cmp lat %v", pkt.Position.Latitude)
	}
	if !floatClose(pkt.Position.Longitude, lon, 1e-3) {
		t.Errorf("cmp lon %v", pkt.Position.Longitude)
	}
}

func TestParseNeverPanics(t *testing.T) {
	// Fuzz-lite: feed a handful of malformed inputs to every dispatcher
	// entry. None should panic or return unexpected errors beyond
	// "malformed".
	inputs := [][]byte{
		{},
		{'!'},
		{'!', '4'},
		{'@'},
		{':', 'A'},
		{'T', '#'},
		{'_'},
		{';', 'X'},
		{')', '!'},
		{'<'},
		{'?'},
		{'}'},
		{'>'},
		{'\''},
		{'`'},
		[]byte("!00000.00X/00000.00X-"),
		[]byte("=xxxxxxxxxxxxxxxxxxx"),
	}
	for _, in := range inputs {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("panic on %q: %v", in, r)
				}
			}()
			_, _ = ParseInfo(in)
		}()
	}
}
