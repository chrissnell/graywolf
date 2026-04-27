package aprs

import (
	"strings"
	"testing"
)

func TestParsePHG(t *testing.T) {
	// APRS101 example: PHG7700 → 49W / 1280ft / 0dB / omni.
	phg, err := ParsePHG("7700")
	if err != nil {
		t.Fatalf("ParsePHG: %v", err)
	}
	if phg.Raw != "7700" {
		t.Errorf("Raw %q", phg.Raw)
	}
	if phg.PowerWatts != 49 {
		t.Errorf("PowerWatts %d want 49", phg.PowerWatts)
	}
	if phg.HeightFt != 1280 {
		t.Errorf("HeightFt %d want 1280", phg.HeightFt)
	}
	if phg.GainDB != 0 {
		t.Errorf("GainDB %d want 0", phg.GainDB)
	}
	if phg.Directivity != 0 {
		t.Errorf("Directivity %d want 0", phg.Directivity)
	}
}

func TestParsePHGTable(t *testing.T) {
	cases := []struct {
		in                           string
		wantWatts, wantFt, wantG, wantD int
	}{
		{"0000", 0, 10, 0, 0},       // minimum
		{"9999", 81, 5120, 9, 9},    // Ham::APRS::FAP out-of-range dir; still decodes
		{"1000", 1, 10, 0, 0},       // 1W
		{"3640", 9, 640, 4, 0},      // 9W / 640ft / 4dB omni
		{"7220", 49, 40, 2, 0},      // from test corpus (h=2 → 40ft)
		{"5132", 25, 20, 3, 2},      // 25W 20ft 3dB NE (90°)
	}
	for _, c := range cases {
		phg, err := ParsePHG(c.in)
		if err != nil {
			t.Errorf("%s: err %v", c.in, err)
			continue
		}
		if phg.PowerWatts != c.wantWatts {
			t.Errorf("%s: watts %d want %d", c.in, phg.PowerWatts, c.wantWatts)
		}
		if phg.HeightFt != c.wantFt {
			t.Errorf("%s: ft %d want %d", c.in, phg.HeightFt, c.wantFt)
		}
		if phg.GainDB != c.wantG {
			t.Errorf("%s: gain %d want %d", c.in, phg.GainDB, c.wantG)
		}
		if phg.Directivity != c.wantD {
			t.Errorf("%s: dir %d want %d", c.in, phg.Directivity, c.wantD)
		}
	}
}

func TestParsePHGErrors(t *testing.T) {
	bad := []string{"", "123", "12345", "ABCD", "77A0"}
	for _, s := range bad {
		if _, err := ParsePHG(s); err == nil {
			t.Errorf("ParsePHG(%q) expected error", s)
		}
	}
}

func TestEncodePHG(t *testing.T) {
	cases := []struct {
		watts, ft, g, d int
		want            string
	}{
		{49, 1280, 0, 0, "PHG7700"},    // exact
		{50, 1280, 0, 0, "PHG7700"},    // √50 ≈ 7.07 → 7
		{25, 20, 3, 2, "PHG5132"},      // exact
		{1, 10, 0, 0, "PHG1000"},       // 1W, 10 ft (encoded as h=0)
		{0, 10, 0, 0, "PHG0000"},       // zero power
		{10000, 5120, 9, 0, "PHG9990"}, // √10000=100 → clamp to 9 (81W)
		{100, 10, 0, 0, "PHG9000"},     // √100=10 → clamp to 9
	}
	for _, c := range cases {
		got, err := EncodePHG(c.watts, c.ft, c.g, c.d)
		if err != nil {
			t.Errorf("EncodePHG(%d,%d,%d,%d): %v", c.watts, c.ft, c.g, c.d, err)
			continue
		}
		if got != c.want {
			t.Errorf("EncodePHG(%d,%d,%d,%d) = %q want %q", c.watts, c.ft, c.g, c.d, got, c.want)
		}
	}

	// Directivity range enforcement.
	if _, err := EncodePHG(10, 20, 1, 9); err == nil {
		t.Error("expected error for directivity 9")
	}
	if _, err := EncodePHG(10, 20, 1, -1); err == nil {
		t.Error("expected error for directivity -1")
	}
}

func TestEncodePHGRoundTrip(t *testing.T) {
	// Values that encode exactly (no lossy quantisation).
	cases := []struct{ watts, ft, g, d int }{
		{49, 1280, 0, 0},
		{25, 20, 3, 2},
		{16, 160, 5, 4},
		{81, 5120, 9, 8},
	}
	for _, c := range cases {
		s, err := EncodePHG(c.watts, c.ft, c.g, c.d)
		if err != nil {
			t.Fatalf("encode: %v", err)
		}
		if !strings.HasPrefix(s, "PHG") || len(s) != 7 {
			t.Fatalf("bad format %q", s)
		}
		phg, err := ParsePHG(s[3:])
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if phg.PowerWatts != c.watts || phg.HeightFt != c.ft || phg.GainDB != c.g || phg.Directivity != c.d {
			t.Errorf("round-trip mismatch for %+v → %q → %+v", c, s, phg)
		}
	}
}

func TestParsePositionWithPHG(t *testing.T) {
	// APRS101 ch 7 example: PHG in place of CSE/SPD on a fixed-station report.
	info := []byte("!4903.50N/07201.75W#PHG5132Digipeater")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatalf("ParseInfo: %v", err)
	}
	if pkt.Position == nil || pkt.Position.PHG == nil {
		t.Fatalf("expected decoded PHG, got %+v", pkt.Position)
	}
	p := pkt.Position.PHG
	if p.PowerWatts != 25 || p.HeightFt != 20 || p.GainDB != 3 || p.Directivity != 2 {
		t.Errorf("PHG decode wrong: %+v", p)
	}
	if pkt.Comment != "Digipeater" {
		t.Errorf("comment %q", pkt.Comment)
	}
	// CSE/SPD should not be set for fixed-station PHG.
	if pkt.Position.HasCourse {
		t.Errorf("should not have course with PHG")
	}
}

func TestPHGString(t *testing.T) {
	var nilP *PHG
	if nilP.String() != "" {
		t.Errorf("nil PHG.String() should be empty")
	}
	p := &PHG{Raw: "7700"}
	if p.String() != "PHG7700" {
		t.Errorf("String()=%q", p.String())
	}
	// Reconstruct from decoded fields when Raw is missing.
	p2 := &PHG{PowerWatts: 49, HeightFt: 1280, GainDB: 0, Directivity: 0}
	if p2.String() != "PHG7700" {
		t.Errorf("reconstructed String()=%q", p2.String())
	}
}
