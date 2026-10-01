package aprs

import "testing"

// TestParseQSY exercises the AFRS frequency-spec examples from
// aprs.org/info/freqspec.txt.
func TestParseQSY(t *testing.T) {
	cases := []struct {
		name     string
		comment  string
		wantNil  bool
		wantFreq float64
		wantTT   string
		wantTF   string
		wantOff  bool
		wantOffM float64
		wantRest string
	}{
		{
			name:     "freq only, 10kHz form",
			comment:  "146.52 MHz Enroute Alabama",
			wantFreq: 146.52,
			wantRest: "Enroute Alabama",
		},
		{
			name:     "freq only, 1kHz form no space",
			comment:  "147.105MHz AARC Radio Club",
			wantFreq: 147.105,
			wantRest: "AARC Radio Club",
		},
		{
			name:     "freq + CTCSS tone, truncated code reverses to 107.2",
			comment:  "146.82 MHz T107 AARC Repeater",
			wantFreq: 146.82,
			wantTT:   "ctcss",
			wantTF:   "107.2",
			wantRest: "AARC Repeater",
		},
		{
			name:     "freq + CTCSS 'C' form + trailing range token (not decoded, left in rest)",
			comment:  "146.835MHz C107 R25m AARC",
			wantFreq: 146.835,
			wantTT:   "ctcss",
			wantTF:   "107.2",
			wantRest: "R25m AARC",
		},
		{
			name:     "freq + DCS code + range in km",
			comment:  "146.805MHz D256 R25k Repeater",
			wantFreq: 146.805,
			wantTT:   "dcs",
			wantTF:   "256",
			wantRest: "R25k Repeater",
		},
		{
			name:     "freq + tone + positive offset",
			comment:  "146.40 MHz T067 +100 Repeater",
			wantFreq: 146.40,
			wantTT:   "ctcss",
			wantTF:   "67.0",
			wantOff:  true,
			wantOffM: 1.00,
			wantRest: "Repeater",
		},
		{
			name:     "freq + tone + negative offset",
			comment:  "442.440MHz T107 -500 Repeater",
			wantFreq: 442.440,
			wantTT:   "ctcss",
			wantTF:   "107.2",
			wantOff:  true,
			wantOffM: -5.00,
			wantRest: "Repeater",
		},
		{
			name:     "lowercase tone letter (narrow modulation, distinction not decoded)",
			comment:  "145.50 MHz t077 Simplex",
			wantFreq: 145.50,
			wantTT:   "ctcss",
			wantTF:   "77.0",
			wantRest: "Simplex",
		},
		{
			name:    "no leading frequency -- no match",
			comment: "Just a comment, no freq here",
			wantNil: true,
		},
		{
			name:    "empty comment",
			comment: "",
			wantNil: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			q, rest := ParseQSY(tc.comment)
			if tc.wantNil {
				if q != nil {
					t.Fatalf("expected nil QSY, got %+v (rest=%q)", q, rest)
				}
				if rest != tc.comment {
					t.Errorf("rest = %q, want unchanged %q", rest, tc.comment)
				}
				return
			}
			if q == nil {
				t.Fatalf("expected non-nil QSY for %q", tc.comment)
			}
			if q.FrequencyMHz != tc.wantFreq {
				t.Errorf("FrequencyMHz = %v, want %v", q.FrequencyMHz, tc.wantFreq)
			}
			if q.ToneType != tc.wantTT {
				t.Errorf("ToneType = %q, want %q", q.ToneType, tc.wantTT)
			}
			if q.ToneFreq != tc.wantTF {
				t.Errorf("ToneFreq = %q, want %q", q.ToneFreq, tc.wantTF)
			}
			if q.HasOffset != tc.wantOff {
				t.Errorf("HasOffset = %v, want %v", q.HasOffset, tc.wantOff)
			}
			if tc.wantOff && q.OffsetMHz != tc.wantOffM {
				t.Errorf("OffsetMHz = %v, want %v", q.OffsetMHz, tc.wantOffM)
			}
			if rest != tc.wantRest {
				t.Errorf("rest = %q, want %q", rest, tc.wantRest)
			}
		})
	}
}

// TestEncodeQSY_RoundTrip confirms EncodeQSY output re-parses to an
// equivalent QSY (the ToneFreq decimal may be reconstructed from the
// truncated code, so the round-trip compares FrequencyMHz/ToneType/
// Offset rather than exact ToneFreq strings when the original wasn't
// already on a standard-tone boundary).
func TestEncodeQSY_RoundTrip(t *testing.T) {
	cases := []*QSY{
		{FrequencyMHz: 146.520},
		{FrequencyMHz: 146.520, ToneType: "ctcss", ToneFreq: "100.0"},
		{FrequencyMHz: 146.520, ToneType: "ctcss", ToneFreq: "107.2", HasOffset: true, OffsetMHz: 0.6},
		{FrequencyMHz: 442.440, ToneType: "dcs", ToneFreq: "023", HasOffset: true, OffsetMHz: -5.0},
		{FrequencyMHz: 146.520, HasOffset: true, OffsetMHz: 0},
	}
	for _, q := range cases {
		wire := EncodeQSY(q)
		if wire == "" {
			t.Fatalf("EncodeQSY(%+v) = \"\"", q)
		}
		got, rest := ParseQSY(wire)
		if got == nil {
			t.Fatalf("ParseQSY(%q) failed to parse own EncodeQSY output", wire)
		}
		if rest != "" {
			t.Errorf("ParseQSY(%q) rest = %q, want \"\"", wire, rest)
		}
		if got.FrequencyMHz != q.FrequencyMHz {
			t.Errorf("round-trip FrequencyMHz = %v, want %v", got.FrequencyMHz, q.FrequencyMHz)
		}
		if got.ToneType != q.ToneType {
			t.Errorf("round-trip ToneType = %q, want %q", got.ToneType, q.ToneType)
		}
		if got.HasOffset != q.HasOffset || got.OffsetMHz != q.OffsetMHz {
			t.Errorf("round-trip offset = (%v,%v), want (%v,%v)", got.HasOffset, got.OffsetMHz, q.HasOffset, q.OffsetMHz)
		}
	}
}

func TestEncodeQSY_Nil(t *testing.T) {
	if got := EncodeQSY(nil); got != "" {
		t.Errorf("EncodeQSY(nil) = %q, want \"\"", got)
	}
	if got := EncodeQSY(&QSY{}); got != "" {
		t.Errorf("EncodeQSY(zero value) = %q, want \"\"", got)
	}
}

// TestParseInfo_QSY_Position confirms the pipeline strips a leading
// freq-spec out of a real position packet's comment and populates
// pkt.QSY.
func TestParseInfo_QSY_Position(t *testing.T) {
	info := []byte("!4903.50N/07201.75W>146.520MHz T100 +060 Repeater")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.QSY == nil {
		t.Fatal("expected pkt.QSY to be populated")
	}
	if pkt.QSY.FrequencyMHz != 146.520 {
		t.Errorf("FrequencyMHz = %v", pkt.QSY.FrequencyMHz)
	}
	if pkt.QSY.ToneType != "ctcss" || pkt.QSY.ToneFreq != "100.0" {
		t.Errorf("tone = %q/%q", pkt.QSY.ToneType, pkt.QSY.ToneFreq)
	}
	if !pkt.QSY.HasOffset || pkt.QSY.OffsetMHz != 0.6 {
		t.Errorf("offset = %v/%v", pkt.QSY.HasOffset, pkt.QSY.OffsetMHz)
	}
	if pkt.Comment != "Repeater" {
		t.Errorf("Comment = %q, want %q (QSY prefix should be stripped)", pkt.Comment, "Repeater")
	}
}

// TestParseInfo_QSY_Object confirms QSY parsed from an object's embedded
// position comment is elevated to the top-level pkt.QSY (mirroring how
// Weather/DF are elevated from the nested inner packet).
func TestParseInfo_QSY_Object(t *testing.T) {
	info := []byte(";LEADER   *092345z4903.50N/07201.75W>146.520MHz T100 Repeater")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketObject || pkt.Object == nil {
		t.Fatalf("type %q", pkt.Type)
	}
	if pkt.QSY == nil {
		t.Fatal("expected pkt.QSY to be populated from the object's embedded comment")
	}
	if pkt.QSY.FrequencyMHz != 146.520 {
		t.Errorf("FrequencyMHz = %v", pkt.QSY.FrequencyMHz)
	}
	if pkt.Object.Comment != "Repeater" {
		t.Errorf("Object.Comment = %q, want %q (QSY prefix should be stripped)", pkt.Object.Comment, "Repeater")
	}
}
