package aprs

import "testing"

func TestBase91RoundTrip(t *testing.T) {
	// 68_574_960 == 91^4 - 1, the maximum value representable in 4
	// base-91 digits. 91^4 itself overflows and is not a valid round-trip.
	cases := []int64{0, 1, 90, 91, 8280, 1_000_000, 68_574_960}
	for _, c := range cases {
		b := base91Encode(c, 4)
		got := base91DecodeN(b)
		if got != c {
			t.Errorf("round-trip %d: encoded %q, decoded %d", c, string(b), got)
		}
	}
}

func TestCompressedLatLonRoundTrip(t *testing.T) {
	lat, lon := 49.5, -72.75
	yx := EncodeCompressedLatLon(lat, lon)
	if len(yx) != 8 {
		t.Fatalf("len %d", len(yx))
	}
	y := base91Decode4(yx[0:4])
	x := base91Decode4(yx[4:8])
	decLat := 90.0 - float64(y)/380926.0
	decLon := -180.0 + float64(x)/190463.0
	if abs(decLat-lat) > 1e-3 {
		t.Errorf("lat %v", decLat)
	}
	if abs(decLon-lon) > 1e-3 {
		t.Errorf("lon %v", decLon)
	}
}
