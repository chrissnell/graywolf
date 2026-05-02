package ax25conn

import "testing"

func TestParseControlMod8_I(t *testing.T) {
	c, err := ParseControl([]byte{0x12}, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != FrameI {
		t.Fatalf("kind=%v", c.Kind)
	}
	// 0x12 = 0001 0 010 = NR=0, P=1? no: NR=000<<5, P=0x10, NS=001<<1.
	// Actually 0x12 = 0b00010010: bit0=0(I), bits1-3=001(NS=1), bit4=1(P), bits5-7=000(NR=0).
	if c.NS != 1 || c.NR != 0 || !c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_RR(t *testing.T) {
	// 0x01 = NR=0, P=0, S=00, 01 → RR
	c, err := ParseControl([]byte{0x01}, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != FrameRR {
		t.Fatalf("kind=%v", c.Kind)
	}
	if c.NR != 0 || c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_RNR(t *testing.T) {
	// NR=2, P=1, RNR: NR<<5 | P<<4 | 0x05 = 0x40 | 0x10 | 0x05 = 0x55
	c, err := ParseControl([]byte{0x55}, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != FrameRNR || c.NR != 2 || !c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_REJ(t *testing.T) {
	// NR=3, P=0, REJ=0x09 → 0x60 | 0x09 = 0x69
	c, err := ParseControl([]byte{0x69}, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != FrameREJ || c.NR != 3 || c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_SREJ(t *testing.T) {
	// NR=4, P=0, SREJ=0x0D → 0x80 | 0x0D = 0x8D
	c, err := ParseControl([]byte{0x8D}, false)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != FrameSREJ || c.NR != 4 {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_SABM(t *testing.T) {
	c, _ := ParseControl([]byte{0x3F}, false) // 0x2F | P-bit
	if c.Kind != FrameSABM {
		t.Fatalf("kind=%v", c.Kind)
	}
	if !c.PF {
		t.Fatal("P bit must be set on SABM")
	}
}

func TestParseControlMod8_SABME(t *testing.T) {
	c, _ := ParseControl([]byte{0x7F}, false) // 0x6F | P-bit
	if c.Kind != FrameSABME || !c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_DISC(t *testing.T) {
	c, _ := ParseControl([]byte{0x53}, false) // 0x43 | P-bit
	if c.Kind != FrameDISC || !c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_DM(t *testing.T) {
	c, _ := ParseControl([]byte{0x1F}, false) // 0x0F | F-bit
	if c.Kind != FrameDM || !c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_UA(t *testing.T) {
	c, _ := ParseControl([]byte{0x73}, false) // 0x63 | F-bit
	if c.Kind != FrameUA || !c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_FRMR(t *testing.T) {
	c, _ := ParseControl([]byte{0x87}, false)
	if c.Kind != FrameFRMR || c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_UI(t *testing.T) {
	c, _ := ParseControl([]byte{0x03}, false)
	if c.Kind != FrameUI {
		t.Fatalf("%+v", c)
	}
	c2, _ := ParseControl([]byte{0x13}, false)
	if c2.Kind != FrameUI || !c2.PF {
		t.Fatalf("%+v", c2)
	}
}

func TestParseControlMod8_XID(t *testing.T) {
	c, _ := ParseControl([]byte{0xAF}, false)
	if c.Kind != FrameXID {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_TEST(t *testing.T) {
	c, _ := ParseControl([]byte{0xE3}, false)
	if c.Kind != FrameTEST {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod8_UnknownU(t *testing.T) {
	if _, err := ParseControl([]byte{0xFB}, false); err == nil {
		t.Fatal("expected error on unknown U-frame")
	}
}

func TestParseControlMod8_Short(t *testing.T) {
	if _, err := ParseControl(nil, false); err == nil {
		t.Fatal("expected error on empty control bytes")
	}
}

func TestEncodeControlMod8_I(t *testing.T) {
	got, err := EncodeControl(Control{Kind: FrameI, NS: 3, NR: 5, PF: true}, false)
	if err != nil {
		t.Fatal(err)
	}
	// NR=5<<5 | P=1<<4 | NS=3<<1 | 0 = 0xA0 | 0x10 | 0x06 = 0xB6
	if got[0] != 0xB6 {
		t.Fatalf("0x%02x, want 0xB6", got[0])
	}
}

func TestEncodeControlMod8_OverflowI(t *testing.T) {
	if _, err := EncodeControl(Control{Kind: FrameI, NS: 8, NR: 0}, false); err == nil {
		t.Fatal("expected NS overflow error")
	}
	if _, err := EncodeControl(Control{Kind: FrameI, NS: 0, NR: 8}, false); err == nil {
		t.Fatal("expected NR overflow error")
	}
}

func TestEncodeControlMod8_OverflowS(t *testing.T) {
	if _, err := EncodeControl(Control{Kind: FrameRR, NR: 8}, false); err == nil {
		t.Fatal("expected NR overflow error")
	}
}

func TestEncodeControlMod8_RoundTrip(t *testing.T) {
	cases := []Control{
		{Kind: FrameI, NS: 0, NR: 0},
		{Kind: FrameI, NS: 7, NR: 7, PF: true},
		{Kind: FrameRR, NR: 0},
		{Kind: FrameRR, NR: 4, PF: true},
		{Kind: FrameRNR, NR: 2},
		{Kind: FrameREJ, NR: 6, PF: true},
		{Kind: FrameSREJ, NR: 1},
		{Kind: FrameSABM, PF: true},
		{Kind: FrameSABM},
		{Kind: FrameSABME, PF: true},
		{Kind: FrameDISC, PF: true},
		{Kind: FrameDM, PF: true},
		{Kind: FrameDM},
		{Kind: FrameUA, PF: true},
		{Kind: FrameFRMR},
		{Kind: FrameUI, PF: true},
		{Kind: FrameUI},
		{Kind: FrameXID},
		{Kind: FrameTEST},
	}
	for _, want := range cases {
		raw, err := EncodeControl(want, false)
		if err != nil {
			t.Fatalf("encode %+v: %v", want, err)
		}
		got, err := ParseControl(raw, false)
		if err != nil {
			t.Fatalf("parse %+v: %v", want, err)
		}
		if got != want {
			t.Fatalf("round-trip mismatch: encoded 0x%02x, want %+v got %+v", raw[0], want, got)
		}
	}
}

func TestParseControlMod128_I(t *testing.T) {
	// NS=42 (0x2A), NR=85 (0x55), P=1.
	// byte0 = NS<<1 = 0x54; byte1 = NR<<1 | P = 0xAA | 0x01 = 0xAB.
	c, err := ParseControl([]byte{0x54, 0xAB}, true)
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != FrameI || c.NS != 42 || c.NR != 85 || !c.PF {
		t.Fatalf("%+v", c)
	}
}

func TestParseControlMod128_S(t *testing.T) {
	for _, tc := range []struct {
		name string
		b0   byte
		want FrameKind
	}{
		{"RR", 0x01, FrameRR},
		{"RNR", 0x05, FrameRNR},
		{"REJ", 0x09, FrameREJ},
		{"SREJ", 0x0D, FrameSREJ},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// NR=120, F=0.
			c, err := ParseControl([]byte{tc.b0, 0xF0}, true)
			if err != nil {
				t.Fatal(err)
			}
			if c.Kind != tc.want || c.NR != 120 || c.PF {
				t.Fatalf("%+v", c)
			}
		})
	}
}

func TestParseControlMod128_S_ReservedBits(t *testing.T) {
	// Reserved bits 4-7 of the low octet must be zero.
	if _, err := ParseControl([]byte{0x11, 0x00}, true); err == nil {
		t.Fatal("expected reserved-bits error")
	}
}

func TestParseControlMod128_U(t *testing.T) {
	// U-frames are 1 octet even under mod-128.
	c, err := ParseControl([]byte{0x3F}, true) // SABM with P=1
	if err != nil {
		t.Fatal(err)
	}
	if c.Kind != FrameSABM || !c.PF {
		t.Fatalf("%+v", c)
	}
	c2, err := ParseControl([]byte{0x7F}, true) // SABME with P=1
	if err != nil {
		t.Fatal(err)
	}
	if c2.Kind != FrameSABME || !c2.PF {
		t.Fatalf("%+v", c2)
	}
}

func TestParseControlMod128_Short(t *testing.T) {
	if _, err := ParseControl(nil, true); err == nil {
		t.Fatal("expected short-buffer error")
	}
	// I-frame low byte (0x00) followed by nothing — needs 2 bytes.
	if _, err := ParseControl([]byte{0x00}, true); err == nil {
		t.Fatal("expected short I/S error")
	}
}

func TestEncodeControlMod128_I(t *testing.T) {
	got, err := EncodeControl(Control{Kind: FrameI, NS: 42, NR: 85, PF: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != 0x54 || got[1] != 0xAB {
		t.Fatalf("got % x", got)
	}
}

func TestEncodeControlMod128_OverflowI(t *testing.T) {
	if _, err := EncodeControl(Control{Kind: FrameI, NS: 128, NR: 0}, true); err == nil {
		t.Fatal("expected NS overflow")
	}
	if _, err := EncodeControl(Control{Kind: FrameI, NS: 0, NR: 128}, true); err == nil {
		t.Fatal("expected NR overflow")
	}
}

func TestEncodeControlMod128_OverflowS(t *testing.T) {
	if _, err := EncodeControl(Control{Kind: FrameRR, NR: 128}, true); err == nil {
		t.Fatal("expected NR overflow")
	}
}

func TestEncodeControlMod128_U(t *testing.T) {
	// U-frames remain 1 octet.
	got, err := EncodeControl(Control{Kind: FrameSABME, PF: true}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != 0x7F {
		t.Fatalf("got % x", got)
	}
}

func TestEncodeControlMod128_RoundTrip(t *testing.T) {
	cases := []Control{
		{Kind: FrameI, NS: 0, NR: 0},
		{Kind: FrameI, NS: 127, NR: 127, PF: true},
		{Kind: FrameI, NS: 64, NR: 1},
		{Kind: FrameRR, NR: 0},
		{Kind: FrameRR, NR: 127, PF: true},
		{Kind: FrameRNR, NR: 50},
		{Kind: FrameREJ, NR: 99, PF: true},
		{Kind: FrameSREJ, NR: 23},
		{Kind: FrameSABM, PF: true},
		{Kind: FrameSABME, PF: true},
		{Kind: FrameDISC, PF: true},
		{Kind: FrameDM, PF: true},
		{Kind: FrameDM},
		{Kind: FrameUA, PF: true},
		{Kind: FrameFRMR},
		{Kind: FrameUI, PF: true},
		{Kind: FrameXID},
		{Kind: FrameTEST},
	}
	for _, want := range cases {
		raw, err := EncodeControl(want, true)
		if err != nil {
			t.Fatalf("encode %+v: %v", want, err)
		}
		got, err := ParseControl(raw, true)
		if err != nil {
			t.Fatalf("parse %+v: %v", want, err)
		}
		if got != want {
			t.Fatalf("round-trip mismatch: encoded % x, want %+v got %+v", raw, want, got)
		}
	}
}

func TestFrameKindString(t *testing.T) {
	if FrameI.String() != "I" || FrameInvalid.String() != "INVALID" {
		t.Fatalf("string drift: %v %v", FrameI, FrameInvalid)
	}
}
