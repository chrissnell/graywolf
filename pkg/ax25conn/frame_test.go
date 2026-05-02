package ax25conn

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

func mustParse(t *testing.T, s string) ax25.Address {
	t.Helper()
	a, err := ax25.ParseAddress(s)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", s, err)
	}
	return a
}

func TestEncodeDecodeI_RoundTrip(t *testing.T) {
	src := mustParse(t, "KE7XYZ-1")
	dst := mustParse(t, "BBS-3")
	via := []ax25.Address{mustParse(t, "WIDE2-2")}
	f := &Frame{
		Source: src, Dest: dst, Path: via,
		Control:   Control{Kind: FrameI, NS: 2, NR: 1, PF: false},
		PID:       0xF0,
		Info:      []byte("hello"),
		IsCommand: true,
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if string(got.Info) != "hello" {
		t.Fatalf("info=%q want hello", got.Info)
	}
	if got.Control.NS != 2 || got.Control.NR != 1 || got.Control.Kind != FrameI {
		t.Fatalf("ctl=%+v", got.Control)
	}
	if got.PID != 0xF0 {
		t.Fatalf("PID=0x%02x", got.PID)
	}
	if !got.IsCommand {
		t.Fatal("expected command-frame polarity")
	}
	if len(got.Path) != 1 || got.Path[0].Call != "WIDE2" || got.Path[0].SSID != 2 {
		t.Fatalf("path=%+v", got.Path)
	}
}

func TestEncodeDecodeRR_NoInfo(t *testing.T) {
	src := mustParse(t, "BBS-3")
	dst := mustParse(t, "KE7XYZ-1")
	f := &Frame{
		Source: src, Dest: dst,
		Control:   Control{Kind: FrameRR, NR: 4, PF: true},
		IsCommand: false,
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Control.Kind != FrameRR || got.Control.NR != 4 || !got.Control.PF {
		t.Fatalf("ctl=%+v", got.Control)
	}
	if len(got.Info) != 0 {
		t.Fatalf("RR has Info: %x", got.Info)
	}
}

func TestEncodeDecodeSABM(t *testing.T) {
	src := mustParse(t, "KE7XYZ-1")
	dst := mustParse(t, "BBS-3")
	f := &Frame{
		Source: src, Dest: dst,
		Control:   Control{Kind: FrameSABM, PF: true},
		IsCommand: true,
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Control.Kind != FrameSABM || !got.Control.PF {
		t.Fatalf("ctl=%+v", got.Control)
	}
	if !got.IsCommand {
		t.Fatal("SABM must be command-polarity")
	}
}

func TestEncodeDecodeDM(t *testing.T) {
	src := mustParse(t, "BBS-3")
	dst := mustParse(t, "KE7XYZ-1")
	f := &Frame{
		Source: src, Dest: dst,
		Control:   Control{Kind: FrameDM, PF: true},
		IsCommand: false,
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	got, err := Decode(raw, false)
	if err != nil {
		t.Fatal(err)
	}
	if got.Control.Kind != FrameDM || !got.Control.PF {
		t.Fatalf("ctl=%+v", got.Control)
	}
}

func TestDecodeShortMissingControl(t *testing.T) {
	src := mustParse(t, "KE7XYZ-1")
	dst := mustParse(t, "BBS-3")
	addr, err := ax25.EncodeAddressBlock(src, dst, nil, true)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Decode(addr, false); err == nil {
		t.Fatal("expected missing-control error")
	}
}

func TestDecodeIMissingPID(t *testing.T) {
	src := mustParse(t, "KE7XYZ-1")
	dst := mustParse(t, "BBS-3")
	addr, _ := ax25.EncodeAddressBlock(src, dst, nil, true)
	ctl, _ := EncodeControl(Control{Kind: FrameI, NS: 0, NR: 0}, false)
	raw := append(addr, ctl...)
	if _, err := Decode(raw, false); err == nil {
		t.Fatal("expected missing-PID error")
	}
}
