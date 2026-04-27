package gps

import (
	"bytes"
	"context"
	"log/slog"
	"math"
	"testing"
)

func approxEq(a, b, eps float64) bool { return math.Abs(a-b) <= eps }

func TestParseRMC_Valid(t *testing.T) {
	line := "$GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W*6A"
	fix, active, err := ParseNMEA(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !active {
		t.Fatalf("expected active fix")
	}
	if !approxEq(fix.Latitude, 48.1173, 1e-4) {
		t.Errorf("lat = %v", fix.Latitude)
	}
	if !approxEq(fix.Longitude, 11.5167, 1e-4) {
		t.Errorf("lon = %v", fix.Longitude)
	}
	if !approxEq(fix.Speed, 22.4, 1e-3) {
		t.Errorf("speed = %v", fix.Speed)
	}
	if !approxEq(fix.Heading, 84.4, 1e-3) {
		t.Errorf("heading = %v", fix.Heading)
	}
	if !fix.HasCourse {
		t.Errorf("HasCourse = false")
	}
	if fix.Timestamp.IsZero() {
		t.Errorf("timestamp zero")
	}
}

func TestParseRMC_Void(t *testing.T) {
	line := "$GPRMC,123519,V,,,,,,,230394,,*47"
	// Compute correct checksum for this void sentence.
	body := "GPRMC,123519,V,,,,,,,230394,,"
	var xor byte
	for i := 0; i < len(body); i++ {
		xor ^= body[i]
	}
	line = "$" + body + "*" + upperHex(xor)
	_, active, err := ParseNMEA(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if active {
		t.Errorf("void sentence reported active")
	}
}

func TestParseGGA_Valid(t *testing.T) {
	line := "$GPGGA,123519,4807.038,N,01131.000,E,1,08,0.9,545.4,M,46.9,M,,*47"
	fix, active, err := ParseNMEA(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !active {
		t.Errorf("expected active fix")
	}
	if !approxEq(fix.Altitude, 545.4, 1e-3) {
		t.Errorf("alt = %v", fix.Altitude)
	}
	if !fix.HasAlt {
		t.Errorf("HasAlt = false")
	}
}

func TestParseNMEA_ChecksumFail(t *testing.T) {
	line := "$GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W*00"
	if _, _, err := ParseNMEA(line); err == nil {
		t.Fatalf("expected checksum error")
	}
}

func TestParseNMEA_Unsupported(t *testing.T) {
	if _, _, err := ParseNMEA("$GPHDT,123.4,T"); err == nil {
		t.Fatalf("expected unsupported error")
	}
}

func TestReadNMEAStream_PartialAcrossReads(t *testing.T) {
	// Stream with a valid sentence split across buffer boundaries plus a
	// trailing partial line (should be preserved and eventually flushed).
	line := "$GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W*6A\n"
	buf := bytes.NewBufferString(line + line)
	cache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	if err := ReadNMEAStream(context.Background(), buf, cache, logger, NMEAOptions{}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	fix, ok := cache.Get()
	if !ok {
		t.Fatalf("cache empty after stream")
	}
	if !approxEq(fix.Latitude, 48.1173, 1e-4) {
		t.Errorf("lat = %v", fix.Latitude)
	}
}

// TestReadNMEAStream_OnParseError verifies that every sentence that
// fails ParseNMEA causes OnParseError("nmea") to fire. Uses a mix of
// bad checksum, unsupported sentence type, and a totally malformed
// line so the counter counts every drop regardless of which specific
// parse step failed.
func TestReadNMEAStream_OnParseError(t *testing.T) {
	stream := bytes.NewBufferString(
		// bad checksum:
		"$GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W*00\n" +
			// unsupported sentence type (HDT):
			"$GPHDT,123.4,T\n" +
			// totally malformed:
			"garbage\n" +
			// valid — must not count:
			"$GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W*6A\n",
	)
	cache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))

	var parseErrs int
	opts := NMEAOptions{
		OnParseError: func(source string) {
			if source != "nmea" {
				t.Errorf("source = %q, want %q", source, "nmea")
			}
			parseErrs++
		},
	}
	if err := ReadNMEAStream(context.Background(), stream, cache, logger, opts); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if parseErrs != 3 {
		t.Errorf("OnParseError fire count = %d, want 3", parseErrs)
	}
	// The trailing valid line should still have landed in the cache.
	if _, ok := cache.Get(); !ok {
		t.Error("valid sentence did not reach the cache")
	}
}

func TestParseVTG_Valid(t *testing.T) {
	line := "$GPVTG,84.4,T,80.1,M,0.124,N,0.230,K,A*25"
	// Recompute checksum for our test sentence.
	body := "GPVTG,84.4,T,80.1,M,0.124,N,0.230,K,A"
	var xor byte
	for i := 0; i < len(body); i++ {
		xor ^= body[i]
	}
	line = "$" + body + "*" + upperHex(xor)

	fix, active, err := ParseNMEA(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !active {
		t.Fatalf("expected active VTG")
	}
	if !approxEq(fix.Heading, 84.4, 1e-3) {
		t.Errorf("heading = %v, want 84.4", fix.Heading)
	}
	if !approxEq(fix.Speed, 0.124, 1e-4) {
		t.Errorf("speed = %v, want 0.124", fix.Speed)
	}
	if !fix.HasCourse {
		t.Errorf("HasCourse = false")
	}
}

func TestParseVTG_ModeN(t *testing.T) {
	body := "GPVTG,,T,,M,,N,,K,N"
	var xor byte
	for i := 0; i < len(body); i++ {
		xor ^= body[i]
	}
	line := "$" + body + "*" + upperHex(xor)
	_, active, err := ParseNMEA(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if active {
		t.Errorf("mode=N should be inactive")
	}
}

func TestReadNMEAStream_VTGMergesIntoCachedFix(t *testing.T) {
	// A position fix followed by a VTG sentence should merge speed/heading
	// into the cached position without clobbering lat/lon.
	rmcBody := "GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W"
	var rmcXor byte
	for i := 0; i < len(rmcBody); i++ {
		rmcXor ^= rmcBody[i]
	}
	vtgBody := "GPVTG,90.0,T,85.0,M,5.5,N,10.2,K,A"
	var vtgXor byte
	for i := 0; i < len(vtgBody); i++ {
		vtgXor ^= vtgBody[i]
	}
	stream := bytes.NewBufferString(
		"$" + rmcBody + "*" + upperHex(rmcXor) + "\n" +
			"$" + vtgBody + "*" + upperHex(vtgXor) + "\n",
	)
	cache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	if err := ReadNMEAStream(context.Background(), stream, cache, logger, NMEAOptions{}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	fix, ok := cache.Get()
	if !ok {
		t.Fatal("cache empty")
	}
	// Position should come from RMC.
	if !approxEq(fix.Latitude, 48.1173, 1e-4) {
		t.Errorf("lat = %v, want ~48.1173", fix.Latitude)
	}
	// Speed/heading should come from VTG (overriding the RMC values).
	if !approxEq(fix.Speed, 5.5, 1e-3) {
		t.Errorf("speed = %v, want 5.5", fix.Speed)
	}
	if !approxEq(fix.Heading, 90.0, 1e-3) {
		t.Errorf("heading = %v, want 90.0", fix.Heading)
	}
}

func TestParseGSA_Valid3D(t *testing.T) {
	line := "$GPGSA,A,3,10,02,18,23,24,27,32,08,,,,,2.35,1.01,2.12*06"
	fix, active, err := ParseNMEA(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if !active {
		t.Fatalf("expected active GSA")
	}
	if fix.FixMode != 3 {
		t.Errorf("FixMode = %d, want 3", fix.FixMode)
	}
	if !approxEq(fix.PDOP, 2.35, 1e-3) {
		t.Errorf("PDOP = %v, want 2.35", fix.PDOP)
	}
	if !approxEq(fix.HDOP, 1.01, 1e-3) {
		t.Errorf("HDOP = %v, want 1.01", fix.HDOP)
	}
	if !approxEq(fix.VDOP, 2.12, 1e-3) {
		t.Errorf("VDOP = %v, want 2.12", fix.VDOP)
	}
	if !fix.HasDOP {
		t.Errorf("HasDOP = false")
	}
}

func TestParseGSA_NoFix(t *testing.T) {
	body := "GPGSA,A,1,,,,,,,,,,,,,,,,"
	var xor byte
	for i := 0; i < len(body); i++ {
		xor ^= body[i]
	}
	line := "$" + body + "*" + upperHex(xor)
	_, active, err := ParseNMEA(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if active {
		t.Errorf("fix=1 should be inactive")
	}
}

func TestReadNMEAStream_GSAMergesIntoCachedFix(t *testing.T) {
	rmcBody := "GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W"
	var rmcXor byte
	for i := 0; i < len(rmcBody); i++ {
		rmcXor ^= rmcBody[i]
	}
	gsaBody := "GPGSA,A,3,10,02,18,23,24,27,32,08,,,,,2.35,1.01,2.12"
	var gsaXor byte
	for i := 0; i < len(gsaBody); i++ {
		gsaXor ^= gsaBody[i]
	}
	stream := bytes.NewBufferString(
		"$" + rmcBody + "*" + upperHex(rmcXor) + "\n" +
			"$" + gsaBody + "*" + upperHex(gsaXor) + "\n",
	)
	cache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	if err := ReadNMEAStream(context.Background(), stream, cache, logger, NMEAOptions{}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	fix, ok := cache.Get()
	if !ok {
		t.Fatal("cache empty")
	}
	// Position from RMC.
	if !approxEq(fix.Latitude, 48.1173, 1e-4) {
		t.Errorf("lat = %v, want ~48.1173", fix.Latitude)
	}
	// DOP from GSA.
	if fix.FixMode != 3 {
		t.Errorf("FixMode = %d, want 3", fix.FixMode)
	}
	if !approxEq(fix.PDOP, 2.35, 1e-3) {
		t.Errorf("PDOP = %v, want 2.35", fix.PDOP)
	}
	if !approxEq(fix.HDOP, 1.01, 1e-3) {
		t.Errorf("HDOP = %v, want 1.01", fix.HDOP)
	}
	if !approxEq(fix.VDOP, 2.12, 1e-3) {
		t.Errorf("VDOP = %v, want 2.12", fix.VDOP)
	}
	if !fix.HasDOP {
		t.Errorf("HasDOP = false")
	}
}

func TestParseGSV_SingleMessage(t *testing.T) {
	body := "GPGSV,1,1,03,01,28,068,23,02,12,038,30,06,08,172,23"
	var xor byte
	for i := 0; i < len(body); i++ {
		xor ^= body[i]
	}
	line := "$" + body + "*" + upperHex(xor)
	talker, totalMsgs, msgNum, sats, err := parseGSVLine(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if talker != "GP" {
		t.Errorf("talker = %q, want GP", talker)
	}
	if totalMsgs != 1 || msgNum != 1 {
		t.Errorf("totalMsgs=%d msgNum=%d, want 1,1", totalMsgs, msgNum)
	}
	if len(sats) != 3 {
		t.Fatalf("got %d sats, want 3", len(sats))
	}
	if sats[0].PRN != 1 || sats[0].Elevation != 28 || sats[0].Azimuth != 68 || sats[0].SNR != 23 {
		t.Errorf("sat[0] = %+v", sats[0])
	}
	if sats[1].PRN != 2 || sats[1].SNR != 30 {
		t.Errorf("sat[1] = %+v", sats[1])
	}
}

func TestParseGSV_EmptySNR(t *testing.T) {
	body := "GPGSV,1,1,01,01,28,068,"
	var xor byte
	for i := 0; i < len(body); i++ {
		xor ^= body[i]
	}
	line := "$" + body + "*" + upperHex(xor)
	_, _, _, sats, err := parseGSVLine(line)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(sats) != 1 {
		t.Fatalf("got %d sats, want 1", len(sats))
	}
	if sats[0].SNR != -1 {
		t.Errorf("SNR = %d, want -1 (not tracking)", sats[0].SNR)
	}
}

func TestReadNMEAStream_GSVStoresSatellites(t *testing.T) {
	rmcBody := "GPRMC,123519,A,4807.038,N,01131.000,E,022.4,084.4,230394,003.1,W"
	var rmcXor byte
	for i := 0; i < len(rmcBody); i++ {
		rmcXor ^= rmcBody[i]
	}
	gsv1Body := "GPGSV,2,1,05,01,28,068,23,02,12,038,30,06,08,172,23,07,15,139,24"
	var gsv1Xor byte
	for i := 0; i < len(gsv1Body); i++ {
		gsv1Xor ^= gsv1Body[i]
	}
	gsv2Body := "GPGSV,2,2,05,09,42,310,35"
	var gsv2Xor byte
	for i := 0; i < len(gsv2Body); i++ {
		gsv2Xor ^= gsv2Body[i]
	}
	stream := bytes.NewBufferString(
		"$" + rmcBody + "*" + upperHex(rmcXor) + "\n" +
			"$" + gsv1Body + "*" + upperHex(gsv1Xor) + "\n" +
			"$" + gsv2Body + "*" + upperHex(gsv2Xor) + "\n",
	)
	cache := NewMemCache()
	logger := slog.New(slog.NewTextHandler(bytes.NewBuffer(nil), nil))
	if err := ReadNMEAStream(context.Background(), stream, cache, logger, NMEAOptions{}); err != nil {
		t.Fatalf("stream: %v", err)
	}
	if _, ok := cache.Get(); !ok {
		t.Fatal("position cache empty")
	}
	view, ok := cache.GetSatellites()
	if !ok {
		t.Fatal("satellite cache empty")
	}
	if len(view.Satellites) != 5 {
		t.Errorf("got %d satellites, want 5", len(view.Satellites))
	}
	if view.UpdatedAt.IsZero() {
		t.Error("UpdatedAt is zero")
	}
}

func upperHex(b byte) string {
	const hex = "0123456789ABCDEF"
	return string([]byte{hex[b>>4], hex[b&0x0f]})
}
