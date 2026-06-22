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

// TestParseMicELonOffsetNormalises locks in the APRS101 ch 10 rule that
// the +100° offset is added BEFORE the 180..189 / 190..199 wrap-range
// normalisation. Raw degrees byte 'l' (108-28 = 80) with the offset bit
// set gives 180, which the spec normalises to 100° — a perfectly valid
// longitude, not an overflow. A prior revision applied the fixup first
// and then rejected the post-offset 180 as out of range, silently
// dropping every offset (>= 100°) Mic-E station (issue #219). This is
// the exact byte that regression hinged on, so assert it decodes.
func TestParseMicELonOffsetNormalises(t *testing.T) {
	srcAddr, _ := ax25.ParseAddress("N0CALL")
	destAddr, _ := ax25.ParseAddress("U3SUY8") // offset bit set on dest[4]='Y'
	// Raw degrees byte = 80 + 28 = 108 ('l'); offset +100 -> 180 -> 100°.
	info := []byte{'`', 'l', 'A', 'A', 'A', 'A', 'A', '-', '/'}
	f, err := ax25.NewUIFrame(srcAddr, destAddr, nil, info)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v (offset longitude must decode, not be rejected)", err)
	}
	if pkt.Position == nil {
		t.Fatal("Position is nil — offset Mic-E longitude failed to decode")
	}
	// 'l'/'A'/'A' -> 100 deg, 37 min, 37 hundredths, East (dest[5]='8').
	wantLon := 100.0 + (37.0+37.0/100.0)/60.0
	if abs(pkt.Position.Longitude-wantLon) > 0.001 {
		t.Errorf("lon = %.4f, want ~%.4f", pkt.Position.Longitude, wantLon)
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

// TestMicE_OffsetLongitude_Issue219 is a regression test built from
// real APRS-IS packets attached to GitHub issue #219. The reporter was
// connected to APRS-IS (no RF) and every Mic-E station in the western
// US silently failed to plot, while the same stations appeared on
// aprs.fi. Root cause: decodeMicELon applied the 180..189 / 190..199
// wrap-range normalisation BEFORE adding the destination's +100°
// longitude offset, instead of after as APRS101 ch 10 requires. With
// the wrong order, raw degree byte 'r' (0x72 -> 86) plus the +100
// offset lands at 186 with no fixup applied, tripping the final
// out-of-range guard and dropping the packet. These stations are all
// near 106..120°W, so they all require the offset and all regressed.
func TestMicE_OffsetLongitude_Issue219(t *testing.T) {
	cases := []struct {
		name    string
		dest    string
		body    []byte // info field after the leading '`' type byte, first 3 = longitude
		wantLon float64
	}{
		// W7PA-9>T2SUTU,...:`rDO"\>/]"F!}
		{"T2SUTU", "T2SUTU", []byte{'r', 'D', 'O'}, -106.6752},
		// W7PA-9>T2SVXT,...:`rB9"f;>/]"Ex}
		{"T2SVXT", "T2SVXT", []byte{'r', 'B', '9'}, -106.6382},
		// KG7HPT-9>TRUQUW,...:`r0+l ...
		{"TRUQUW", "TRUQUW", []byte{'r', '0', '+'}, -106.3358},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, _, off, ew, err := decodeMicEDest(tc.dest)
			if err != nil {
				t.Fatalf("decodeMicEDest(%q): %v", tc.dest, err)
			}
			lon, err := decodeMicELon(tc.body, off, ew)
			if err != nil {
				t.Fatalf("decodeMicELon: %v (offset longitude must not be rejected)", err)
			}
			if abs(lon-tc.wantLon) > 0.01 {
				t.Errorf("lon = %.4f, want ~%.4f", lon, tc.wantLon)
			}
			if lon >= 0 {
				t.Errorf("lon = %.4f, want a western (negative) longitude", lon)
			}
		})
	}
}

// TestMicE_EndToEnd_Issue219 parses a full Mic-E packet through the
// same path the APRS-IS ingress uses (frame -> aprs.Parse) and asserts
// a plotted position comes out, closing the issue #219 loop above the
// unit level.
func TestMicE_EndToEnd_Issue219(t *testing.T) {
	destAddr, err := ax25.ParseAddress("T2SUTU")
	if err != nil {
		t.Fatal(err)
	}
	srcAddr, err := ax25.ParseAddress("W7PA-9")
	if err != nil {
		t.Fatal(err)
	}
	// Real on-air bytes: `rDO"\\>/]"F!}  — backtick type byte, then
	// lon "rDO", speed/course `"\\` (two 0x5C bytes), symbol code '>',
	// table '/', Kenwood ']' manufacturer prefix, then comment. The Go
	// string literal below uses \\\\ for the two literal backslashes.
	info := []byte("`rDO\"\\\\>/]\"F!}")
	f, err := ax25.NewUIFrame(srcAddr, destAddr, nil, info)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if pkt.Type != PacketMicE {
		t.Fatalf("type = %v, want PacketMicE", pkt.Type)
	}
	if pkt.Position == nil {
		t.Fatal("Position is nil — Mic-E position failed to decode")
	}
	if abs(pkt.Position.Latitude-42.5908) > 0.01 {
		t.Errorf("lat = %.4f, want ~42.5908", pkt.Position.Latitude)
	}
	if abs(pkt.Position.Longitude-(-106.6752)) > 0.01 {
		t.Errorf("lon = %.4f, want ~-106.6752", pkt.Position.Longitude)
	}
}

// TestParseMicECommentSurfaced covers GH #377: a mobile beacon's
// free-form comment ("KK4CUK Matt's Cozy") was decoded into MicE.Status
// but never copied to pkt.Comment, so the stationcache and the mobile
// UI showed nothing even though aprs.fi displayed it. The trailing
// "|...|!DAO!|3" telemetry/DAO/remnant tail these Byonics/McTracker
// radios emit must be stripped cleanly — a greedy pipe sweep used to
// leave a stray "3" on the end. The companion packets carrying only
// telemetry (no human text) must surface an empty comment, not "3".
func TestParseMicECommentSurfaced(t *testing.T) {
	cases := []struct {
		name string
		dest string
		info string
		want string
	}{
		{"comment", "S7TP0U", "`qa4.RI'/'\"M)}KK4CUK Matt's Cozy|!>&@'j|!w<(!|3", "KK4CUK Matt's Cozy"},
		{"telemetry_only_a", "S7RW2Q", "`q4o-fG'/'\"O#}|!;&A'm|!wi@!|3", ""},
		{"telemetry_only_b", "S7TT4S", "`q[m.RF'/'\"Jp}|!?&D'j|!woI!|3", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src, _ := ax25.ParseAddress("N9825W")
			dst, _ := ax25.ParseAddress(tc.dest)
			f, err := ax25.NewUIFrame(src, dst, nil, []byte(tc.info))
			if err != nil {
				t.Fatal(err)
			}
			pkt, err := Parse(f)
			if err != nil {
				t.Fatal(err)
			}
			if pkt.MicE == nil {
				t.Fatal("expected Mic-E packet")
			}
			if pkt.Comment != tc.want {
				t.Errorf("pkt.Comment = %q, want %q", pkt.Comment, tc.want)
			}
			if pkt.MicE.Status != tc.want {
				t.Errorf("MicE.Status = %q, want %q", pkt.MicE.Status, tc.want)
			}
			// All three beacons carry a base-91 DAO ("!w..!") wedged
			// between the telemetry block and the trailing remnant. The
			// strip rework exists precisely so extractDAO still consumes
			// it — assert the precision was applied, not merely deleted.
			if pkt.Position.DAODatum != 'W' {
				t.Errorf("DAODatum = %q, want 'W' (DAO not applied)", pkt.Position.DAODatum)
			}
		})
	}
}
