package aprs

import (
	"errors"
	"testing"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

func TestMicEDestEncoding(t *testing.T) {
	// Round-trip via EncodeMicEDest → decodeMicEDest.
	cases := []struct {
		lat      float64
		msg      int
		offset   bool
		west     bool
		wantSign float64
	}{
		{35.5, 0, false, true, 1},
		{-35.5, 3, true, true, -1},
		{45.25, 7, false, false, 1},
	}
	for _, tc := range cases {
		dest := EncodeMicEDest(tc.lat, tc.msg, tc.offset, tc.west, 0)
		if len(dest) != 6 {
			t.Fatalf("dest len %d", len(dest))
		}
		lat, msg, nsSign, lonOff, ewSign, err := decodeMicEDest(dest)
		if err != nil {
			t.Fatalf("decode %q: %v", dest, err)
		}
		latWant := tc.lat
		if latWant < 0 {
			latWant = -latWant
		}
		if abs(lat-latWant) > 0.01 {
			t.Errorf("%q lat %v want %v", dest, lat, latWant)
		}
		if msg != tc.msg {
			t.Errorf("%q msg %d want %d", dest, msg, tc.msg)
		}
		if nsSign != tc.wantSign {
			t.Errorf("%q ns sign %v", dest, nsSign)
		}
		wantOff := 0
		if tc.offset {
			wantOff = 100
		}
		if lonOff != wantOff {
			t.Errorf("%q offset %d want %d", dest, lonOff, wantOff)
		}
		wantEw := 1.0
		if tc.west {
			wantEw = -1
		}
		if ewSign != wantEw {
			t.Errorf("%q ew %v want %v", dest, ewSign, wantEw)
		}
	}
}

func TestParseMicEFrame(t *testing.T) {
	// Build a synthetic Mic-E frame: lat 35.5 N, lon -72.5 W, msg "En Route".
	dest := EncodeMicEDest(35.5, 1, false, true, 0) // lat, msg=1, offset=0, west
	destAddr, err := ax25.ParseAddress(dest)
	if err != nil {
		t.Fatal(err)
	}
	srcAddr, _ := ax25.ParseAddress("W1AW")
	// Info field: encode longitude 72.5 → deg=72 (+28=100=='d'), min=30
	// (+28=58=':'), hund=0 (+28=28=0x1C). Speed=0, course=0. Symbol />.
	info := []byte{
		'`',
		byte(72 + 28), byte(30 + 28), byte(0 + 28),
		byte(0 + 28), byte(0 + 28), byte(0 + 28),
		'>', '/',
	}
	f, err := ax25.NewUIFrame(srcAddr, destAddr, nil, info)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketMicE || pkt.MicE == nil {
		t.Fatalf("type %q", pkt.Type)
	}
	if abs(pkt.MicE.Position.Latitude-35.5) > 0.01 {
		t.Errorf("lat %v", pkt.MicE.Position.Latitude)
	}
	if abs(pkt.MicE.Position.Longitude+72.5) > 0.01 {
		t.Errorf("lon %v", pkt.MicE.Position.Longitude)
	}
}

func TestParseMicEAltitude(t *testing.T) {
	// Build a Mic-E frame with a 4-byte altitude appendix "XXX}" after
	// the symbol table. Encoded value + 10000 = meters.
	// Pick a target altitude of 1234 m → raw = 11234 → base-91 digits:
	// 11234 = 1*91*91 + 32*91 + 41 → digits (1,32,41) → bytes 34, 65, 74.
	dest := EncodeMicEDest(35.5, 0, false, true, 0)
	destAddr, _ := ax25.ParseAddress(dest)
	srcAddr, _ := ax25.ParseAddress("W1AW")
	info := []byte{
		'`',
		byte(72 + 28), byte(30 + 28), byte(0 + 28),
		byte(0 + 28), byte(0 + 28), byte(0 + 28),
		'>', '/',
		34, 65, 74, '}',
	}
	f, _ := ax25.NewUIFrame(srcAddr, destAddr, nil, info)
	pkt, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.MicE == nil {
		t.Fatal("no mic-e")
	}
	if !pkt.MicE.Position.HasAlt {
		t.Fatalf("expected altitude, got %+v", pkt.MicE.Position)
	}
	if int(pkt.MicE.Position.Altitude) != 1234 {
		t.Errorf("altitude %v want 1234", pkt.MicE.Position.Altitude)
	}
	if !pkt.Position.HasAlt || int(pkt.Position.Altitude) != 1234 {
		t.Errorf("outer position altitude %+v", pkt.Position)
	}
}

// TestParseMicEAmbiguousLonRejected covers a DL9DAK packet seen in the
// wild whose longitude info-field begins with SPACE (0x20). dest "U3SUY8"
// sets the +100° longitude-offset bit (dest[4]='Y'), so combining the
// SPACE byte (raw lon=4) with the offset yields 104.96° — a
// spec-compliant decode that drops the German station onto Mongolia,
// ~8000 km from its actual position. APRS101 ch 10 reserves SPACE as the
// ambiguous-data marker for this field, so we refuse to plot it.
func TestParseMicEAmbiguousLonRejected(t *testing.T) {
	srcAddr, _ := ax25.ParseAddress("DL9DAK")
	destAddr, _ := ax25.ParseAddress("U3SUY8")
	info := []byte{'\'', 0x20, 'U', 'h', 'l', 0x20, 'B', '-', '/', '>'}
	f, err := ax25.NewUIFrame(srcAddr, destAddr, nil, info)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := Parse(f)
	if err == nil {
		t.Fatalf("expected error for ambiguous lon, got pkt %+v", pkt.MicE)
	}
	if !errors.Is(err, ErrMicELonAmbiguous) {
		t.Fatalf("wrong error: %v (want ErrMicELonAmbiguous)", err)
	}
}

// TestParseMicEDelInLonRejected covers a pattern reported in graywolf
// issue #76: PicoAPRS-class hardware (DL8XI, DL9DAK, others) emits
// 0x7f (DEL) in the Mic-E info-field longitude when GPS has not yet
// locked, while still asserting the destination's +100° offset bit.
// Raw lon byte 0 = 0x7f → d = 99; combined with offset 100 → 199°,
// which wraps to ~-161° and drops a German station off Alaska. The
// SPACE (0x20) check from the previous fix did not catch it.
func TestParseMicEDelInLonRejected(t *testing.T) {
	cases := []struct {
		name string
		src  string
		dest string
		info []byte
	}{
		{
			// 2026-05-05 DL9DAK>U3SUY8: '<7f>Uhl <1c>-/>
			name: "DL9DAK",
			src:  "DL9DAK",
			dest: "U3SUY8",
			info: []byte{'\'', 0x7f, 'U', 'h', 'l', 0x20, 0x1c, '-', '/', '>'},
		},
		{
			// 2026-05-05 DL8XI>US3XQ4: `<7f>(<7f>l<1f>L-/"3u}Ingo
			name: "DL8XI",
			src:  "DL8XI",
			dest: "US3XQ4",
			info: []byte{'`', 0x7f, '(', 0x7f, 'l', 0x1f, 'L', '-', '/', '"', '3', 'u', '}'},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srcAddr, err := ax25.ParseAddress(tc.src)
			if err != nil {
				t.Fatal(err)
			}
			destAddr, err := ax25.ParseAddress(tc.dest)
			if err != nil {
				t.Fatal(err)
			}
			f, err := ax25.NewUIFrame(srcAddr, destAddr, nil, tc.info)
			if err != nil {
				t.Fatal(err)
			}
			pkt, err := Parse(f)
			if err == nil {
				t.Fatalf("expected error, got pkt %+v", pkt.MicE)
			}
			if !errors.Is(err, ErrMicELonAmbiguous) {
				t.Fatalf("wrong error: %v (want ErrMicELonAmbiguous)", err)
			}
		})
	}
}

// TestParseMicELonOverflowRejected covers the post-offset range guard
// independently of the sentinel-byte check. A radio that encodes raw
// degrees 80..99 with the +100° offset bit set produces 180..199 — a
// value the spec does not allow. Reject rather than wrap to the
// antimeridian.
func TestParseMicELonOverflowRejected(t *testing.T) {
	srcAddr, _ := ax25.ParseAddress("N0CALL")
	destAddr, _ := ax25.ParseAddress("U3SUY8") // offset bit set on dest[4]='Y'
	// Raw degrees byte = 80 + 28 = 108 ('l'). With offset +100 = 180,
	// out of range. Use printable bytes so the sentinel check does not
	// fire first.
	info := []byte{'`', 'l', 'A', 'A', 'A', 'A', 'A', '-', '/'}
	f, err := ax25.NewUIFrame(srcAddr, destAddr, nil, info)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Parse(f); !errors.Is(err, ErrMicELonAmbiguous) {
		t.Fatalf("wrong error: %v", err)
	}
}

func abs(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

// TestEncodeMicEDest_Ambiguity confirms the K/L/Z space-variant
// replacement on the latitude digits per APRS101 ch 10 round-trips
// through the local decoder for all four ambiguity levels and for
// every hemisphere/offset combination.
func TestEncodeMicEDest_Ambiguity(t *testing.T) {
	// lat must be in -90..90; the longitude offset bit is independent
	// of the latitude value (it flags that the longitude needed +100,
	// not the latitude).
	cases := []struct {
		name    string
		lat     float64
		msg     int
		offset  bool
		west    bool
		wantNS  float64 // 1 N, -1 S
		wantOff int
		wantEW  float64 // 1 E, -1 W
	}{
		{"north_e_no_offset", 37.4092, 0, false, false, 1, 0, 1},
		{"north_e_offset_flag", 37.4092, 0, true, false, 1, 100, 1},
		{"south_w_no_offset", -37.4092, 0, false, true, -1, 0, -1},
		{"south_w_offset_flag", -37.4092, 0, true, true, -1, 100, -1},
		{"north_w_no_offset", 37.4092, 0, false, true, 1, 0, -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			for level := 0; level <= 4; level++ {
				got := EncodeMicEDest(tc.lat, tc.msg, tc.offset, tc.west, level)
				if len(got) != 6 {
					t.Fatalf("level %d: unexpected length: %q", level, got)
				}
				dlat, _, dns, doff, dew, err := decodeMicEDest(got)
				if err != nil {
					t.Fatalf("level %d: decodeMicEDest(%q): %v", level, got, err)
				}
				if dns != tc.wantNS {
					t.Errorf("level %d: ns sign %v want %v (dest=%q)", level, dns, tc.wantNS, got)
				}
				if doff != tc.wantOff {
					t.Errorf("level %d: lon offset %d want %d (dest=%q)", level, doff, tc.wantOff, got)
				}
				if dew != tc.wantEW {
					t.Errorf("level %d: ew sign %v want %v (dest=%q)", level, dew, tc.wantEW, got)
				}
				// Tolerance grows with ambiguity level. Level 1 = 1/100
				// minute (~0.000167 deg) but the encoder also rounds the
				// fractional minute into an integer hundredth, so 0.001
				// is a safe floor. Levels 2..4 broaden to ~0.01, ~0.1,
				// ~1.0 degrees.
				wantLat := tc.lat
				if wantLat < 0 {
					wantLat = -wantLat
				}
				tol := []float64{0.001, 0.01, 0.1, 1.0, 10.0}[level]
				if abs(dlat-wantLat) > tol {
					t.Errorf("level %d: lat %.4f want ~%.4f (tol %.3f) (dest=%q)", level, dlat, wantLat, tol, got)
				}
			}
		})
	}
}

// TestMicEMessageLabels_MatchAPRS101 locks in the spec-correct
// indexing of the message-code label table per APRS101 ch 10 table 8:
// the 3-bit code is the decimal value of the ABC message bits read
// from destination slots 0..2. Bits 111 (decimal 7) = M0 = Off Duty;
// bits 000 (decimal 0) = M7 = Emergency. The table was historically
// inverted, which silently agreed with a symmetrically wrong encoder
// constant in pkg/beacon/mice.go but disagreed with every external
// decoder. Reproducing the indexing here so a future regression
// (well-intentioned alphabetization, for example) breaks loudly.
func TestMicEMessageLabels_MatchAPRS101(t *testing.T) {
	want := map[int]string{
		0: "Emergency", // ABC = 000
		1: "Priority",  // ABC = 001
		2: "Special",   // ABC = 010
		3: "Committed", // ABC = 011
		4: "Returning", // ABC = 100
		5: "In Service",
		6: "En Route",
		7: "Off Duty", // ABC = 111
	}
	for code, label := range want {
		if got := miceMessageLabels[code]; got != label {
			t.Errorf("miceMessageLabels[%d] = %q, want %q (APRS101 ch 10 table 8)", code, got, label)
		}
	}
}
