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

func TestParseControlMod128_NotYet(t *testing.T) {
	if _, err := ParseControl([]byte{0, 0}, true); err == nil {
		t.Fatal("expected mod-128 stub error")
	}
}

func TestEncodeControlMod128_NotYet(t *testing.T) {
	if _, err := EncodeControl(Control{Kind: FrameI}, true); err == nil {
		t.Fatal("expected mod-128 stub error")
	}
}

func TestFrameKindString(t *testing.T) {
	if FrameI.String() != "I" || FrameInvalid.String() != "INVALID" {
		t.Fatalf("string drift: %v %v", FrameI, FrameInvalid)
	}
}
