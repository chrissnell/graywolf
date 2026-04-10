package beacon

import (
	"math"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// TestCompressedPositionInfoRoundTrip builds a compressed position info
// field and decodes it back through the APRS parser to ensure we emit a
// byte sequence the parser recognises and that lat/lon/course/speed/alt
// survive the round trip within the format's resolution limits.
func TestCompressedPositionInfoRoundTrip(t *testing.T) {
	const (
		lat     = 45.12345
		lon     = -122.98765
		course  = 184    // degrees
		speedKt = 42.0   // knots
		altM    = 1234.5 // metres
	)

	info := CompressedPositionInfo(lat, lon, course, speedKt, altM, '/', '>', false, "Graywolf")

	// Shape check: prefix + 13-byte compressed block + "/A=NNNNNN" + comment.
	if info[0] != '!' {
		t.Fatalf("prefix: got %q want '!'", info[0])
	}
	if !strings.Contains(info, "/A=") {
		t.Fatalf("expected /A= altitude extension in %q", info)
	}
	if !strings.HasSuffix(info, "Graywolf") {
		t.Fatalf("expected trailing comment in %q", info)
	}

	pkt, err := aprs.ParseInfo([]byte(info))
	if err != nil {
		t.Fatalf("parse compressed info: %v (info=%q)", err, info)
	}
	if pkt.Position == nil {
		t.Fatalf("no position parsed from %q", info)
	}
	if !pkt.Position.Compressed {
		t.Fatalf("parser did not flag position as compressed: %q", info)
	}
	// Base-91 YYYY/XXXX resolution: <1m. Assert well under that.
	if math.Abs(pkt.Position.Latitude-lat) > 1e-4 {
		t.Errorf("lat round-trip: got %v want %v", pkt.Position.Latitude, lat)
	}
	if math.Abs(pkt.Position.Longitude-lon) > 1e-4 {
		t.Errorf("lon round-trip: got %v want %v", pkt.Position.Longitude, lon)
	}
	// Course quantises to the nearest 4°; speed is logarithmic.
	if !pkt.Position.HasCourse || math.Abs(float64(pkt.Position.Course-course)) > 4 {
		t.Errorf("course round-trip: got %v has=%v want ~%v", pkt.Position.Course, pkt.Position.HasCourse, course)
	}
	if math.Abs(pkt.Position.Speed-speedKt) > 2 {
		t.Errorf("speed round-trip: got %v want ~%v", pkt.Position.Speed, speedKt)
	}
	// Altitude came from /A= (feet), so round-trip is exact to the foot.
	wantAltM := math.Round(altM*3.28084) * 0.3048
	if !pkt.Position.HasAlt || math.Abs(pkt.Position.Altitude-wantAltM) > 0.5 {
		t.Errorf("altitude round-trip: got %v has=%v want %v", pkt.Position.Altitude, pkt.Position.HasAlt, wantAltM)
	}
	if pkt.Comment != "Graywolf" {
		t.Errorf("comment round-trip: got %q want %q", pkt.Comment, "Graywolf")
	}
	if pkt.Position.Symbol.Table != '/' || pkt.Position.Symbol.Code != '>' {
		t.Errorf("symbol round-trip: got %q/%q want '/'/'>'", pkt.Position.Symbol.Table, pkt.Position.Symbol.Code)
	}
}

// TestCompressedPositionInfoNoCSNoAlt verifies the no-course/no-speed
// path emits two spaces in the cs field and omits the /A= extension.
func TestCompressedPositionInfoNoCSNoAlt(t *testing.T) {
	info := CompressedPositionInfo(37.5, -122.0, 0, 0, 0, '/', '-', true, "")
	if info[0] != '=' {
		t.Fatalf("messaging prefix: got %q want '='", info[0])
	}
	if strings.Contains(info, "/A=") {
		t.Fatalf("expected no altitude extension: %q", info)
	}
	// Block length: '=' + 13 bytes.
	if len(info) != 14 {
		t.Fatalf("block length: got %d want 14 (%q)", len(info), info)
	}
	// cs field = info[11:13] when no comment.
	if info[11] != ' ' || info[12] != ' ' {
		t.Errorf("cs field: got %q%q want two spaces", info[11], info[12])
	}
	pkt, err := aprs.ParseInfo([]byte(info))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if pkt.Position == nil || !pkt.Position.Compressed {
		t.Fatalf("expected compressed position, got %+v", pkt.Position)
	}
	if pkt.Position.HasCourse || pkt.Position.HasAlt {
		t.Errorf("unexpected course/alt flags: %+v", pkt.Position)
	}
}
