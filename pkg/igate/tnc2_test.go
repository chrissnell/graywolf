package igate

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

func buildRawFrame(t *testing.T, src, dest string, path []string, info string) []byte {
	t.Helper()
	srcA, err := ax25.ParseAddress(src)
	if err != nil {
		t.Fatal(err)
	}
	destA, err := ax25.ParseAddress(dest)
	if err != nil {
		t.Fatal(err)
	}
	paths := make([]ax25.Address, 0, len(path))
	for _, p := range path {
		a, err := ax25.ParseAddress(p)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, a)
	}
	f, err := ax25.NewUIFrame(srcA, destA, paths, []byte(info))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEncodeTNC2AppendsQAR(t *testing.T) {
	raw := buildRawFrame(t, "KE7XYZ-1", "APRS", []string{"WIDE1-1", "WIDE2-1"}, "!3725.00N/12158.00W>Hello")
	pkt := &aprs.DecodedAPRSPacket{
		Source: "KE7XYZ-1",
		Dest:   "APRS",
		Path:   []string{"WIDE1-1", "WIDE2-1"},
		Raw:    raw,
	}
	line, err := encodeTNC2(pkt, "W5XYZ-10")
	if err != nil {
		t.Fatal(err)
	}
	want := "KE7XYZ-1>APRS,WIDE1-1,WIDE2-1,qAR,W5XYZ-10:!3725.00N/12158.00W>Hello"
	if line != want {
		t.Fatalf("encodeTNC2 mismatch:\n got: %s\nwant: %s", line, want)
	}
}

func TestEncodeTNC2RejectsMissingInfo(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{Source: "KE7XYZ", Dest: "APRS"}
	if _, err := encodeTNC2(pkt, "W5XYZ-10"); err == nil {
		t.Fatal("expected error for packet with no Raw info")
	}
}

func TestParseTNC2StripsQConstructs(t *testing.T) {
	line := "W5ABC-7>APDR16,WIDE1-1*,WIDE2-2,qAR,T2TEXAS:!3725.00N/12158.00W>Test"
	f, err := parseTNC2(line)
	if err != nil {
		t.Fatal(err)
	}
	if f.Source.Call != "W5ABC" || f.Source.SSID != 7 {
		t.Fatalf("source wrong: %+v", f.Source)
	}
	if f.Dest.Call != "APDR16" {
		t.Fatalf("dest wrong: %+v", f.Dest)
	}
	if len(f.Path) != 2 {
		t.Fatalf("expected 2 path entries, got %d: %+v", len(f.Path), f.Path)
	}
	if f.Path[0].Repeated {
		t.Fatal("IS->RF must clear the H bit")
	}
	if string(f.Info) != "!3725.00N/12158.00W>Test" {
		t.Fatalf("info wrong: %q", f.Info)
	}
}

func TestParseTNC2RejectsBadLine(t *testing.T) {
	for _, bad := range []string{
		"",
		"no-colon-here",
		">MISSING-SRC:info",
		"SRC>:info",
		"SRC>DEST:",
	} {
		if _, err := parseTNC2(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestEncodeTNC2LineFraming(t *testing.T) {
	for _, tc := range []struct {
		name, in, want string
	}{
		{"plain", ">status", ">status"},
		{"trailing CR", ">status\r", ">status"},
		{"trailing LF", ">status\n", ">status"},
		{"trailing CRLF", ">status\r\n", ">status"},
		{"repeated terminators", ">status\r\n\r", ">status"},
		{"embedded CR", ">status\rbody", ">status"},
		{"embedded LF", ">status\nbody", ">status"},
		{"binary control preserved", ">status\x1cbody\r", ">status\x1cbody"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildRawFrame(t, "DL9ABC-9", "APRS", nil, tc.in)
			pkt := &aprs.DecodedAPRSPacket{
				Source: "DL9ABC-9",
				Dest: "APRS",
				Raw: raw,
			}
			{
				got, err := encodeTNC2(pkt, "W5XYZ-10")
				if err != nil {
					t.Fatal(err)
				}
				want := "DL9ABC-9>APRS,qAR,W5XYZ-10:" + tc.want
				if got != want {
					t.Errorf("got %q, want %q", got, want)
				}
			}
			f, err := ax25.Decode(raw)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(f.Info); got != tc.in {
				t.Fatalf("RF AX.25 info changed: got %q, want %q", got, tc.in)
			}
		})
	}
}


func TestEncodeTNC2RejectsEmptySanitizedInfo(t *testing.T) {
 pkt := &aprs.DecodedAPRSPacket{Source: "K1ABC", Dest: "APRS", Raw: buildRawFrame(t, "K1ABC", "APRS", nil, "\r>status")}
 if _, err := encodeTNC2(pkt, "W5XYZ-10"); err == nil { t.Fatal("expected empty sanitized information to be rejected") }
}
