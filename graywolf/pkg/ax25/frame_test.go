package ax25

import (
	"bytes"
	"testing"
)

func TestParseAddress(t *testing.T) {
	cases := []struct {
		in      string
		want    Address
		wantErr bool
	}{
		{"N0CALL", Address{Call: "N0CALL"}, false},
		{"W5XYZ-9", Address{Call: "W5XYZ", SSID: 9}, false},
		{"WIDE2-1*", Address{Call: "WIDE2", SSID: 1, Repeated: true}, false},
		{"n0call", Address{Call: "N0CALL"}, false},
		{"", Address{}, true},
		{"TOOLONGCALL", Address{}, true},
		{"W5-16", Address{}, true},
		{"W5-abc", Address{}, true},
		{"W5!", Address{}, true},
	}
	for _, c := range cases {
		got, err := ParseAddress(c.in)
		if (err != nil) != c.wantErr {
			t.Errorf("ParseAddress(%q) err=%v wantErr=%v", c.in, err, c.wantErr)
			continue
		}
		if err == nil && got != c.want {
			t.Errorf("ParseAddress(%q) = %+v want %+v", c.in, got, c.want)
		}
	}
}

func TestAddressString(t *testing.T) {
	if got := (Address{Call: "N0CALL"}).String(); got != "N0CALL" {
		t.Errorf("got %q", got)
	}
	if got := (Address{Call: "W5XYZ", SSID: 9}).String(); got != "W5XYZ-9" {
		t.Errorf("got %q", got)
	}
	if got := (Address{Call: "WIDE1", SSID: 1, Repeated: true}).String(); got != "WIDE1-1*" {
		t.Errorf("got %q", got)
	}
}

func TestUIFrameRoundTrip(t *testing.T) {
	src, _ := ParseAddress("N0CALL-1")
	dst, _ := ParseAddress("APRS")
	p1, _ := ParseAddress("WIDE1-1")
	p2, _ := ParseAddress("WIDE2-2")
	f, err := NewUIFrame(src, dst, []Address{p1, p2}, []byte("!4903.50N/07201.75W-Test"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if f2.Source.Call != "N0CALL" || f2.Source.SSID != 1 {
		t.Errorf("source: %+v", f2.Source)
	}
	if f2.Dest.Call != "APRS" || f2.Dest.SSID != 0 {
		t.Errorf("dest: %+v", f2.Dest)
	}
	if len(f2.Path) != 2 {
		t.Fatalf("path len: %d", len(f2.Path))
	}
	if f2.Path[0].Call != "WIDE1" || f2.Path[0].SSID != 1 {
		t.Errorf("path[0]: %+v", f2.Path[0])
	}
	if f2.Path[1].Call != "WIDE2" || f2.Path[1].SSID != 2 {
		t.Errorf("path[1]: %+v", f2.Path[1])
	}
	if !f2.IsUI() {
		t.Error("IsUI false")
	}
	if f2.PID != PIDNoLayer3 {
		t.Errorf("pid: %x", f2.PID)
	}
	if !bytes.Equal(f2.Info, []byte("!4903.50N/07201.75W-Test")) {
		t.Errorf("info mismatch: %q", f2.Info)
	}
	if !f2.CommandResp {
		t.Error("expected CommandResp=true for v2.0 command frame")
	}
}

func TestDecodeNoPath(t *testing.T) {
	src, _ := ParseAddress("W1AW")
	dst, _ := ParseAddress("CQ")
	f, _ := NewUIFrame(src, dst, nil, []byte("hi"))
	raw, err := f.Encode()
	if err != nil {
		t.Fatal(err)
	}
	f2, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(f2.Path) != 0 {
		t.Errorf("expected empty path, got %d", len(f2.Path))
	}
	if string(f2.Info) != "hi" {
		t.Errorf("info: %q", f2.Info)
	}
}

func TestDecodeRepeatedFlag(t *testing.T) {
	src, _ := ParseAddress("N0CALL")
	dst, _ := ParseAddress("APRS")
	// Mark first digi as already repeated.
	p1 := Address{Call: "WIDE1", SSID: 1, Repeated: true}
	p2, _ := ParseAddress("WIDE2-2")
	f, _ := NewUIFrame(src, dst, []Address{p1, p2}, []byte("x"))
	raw, _ := f.Encode()
	f2, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !f2.Path[0].Repeated {
		t.Error("expected path[0].Repeated=true")
	}
	if f2.Path[1].Repeated {
		t.Error("expected path[1].Repeated=false")
	}
}

func TestDecodeShortFrame(t *testing.T) {
	if _, err := Decode([]byte{1, 2, 3}); err == nil {
		t.Error("expected error")
	}
}

func TestDecodeConnectedModeHeader(t *testing.T) {
	// Build a frame with an SABM control byte (0x2F) to exercise the
	// "header-only parse, IsUI=false" path.
	src, _ := ParseAddress("W1AW")
	dst, _ := ParseAddress("W2XX")
	f, _ := NewUIFrame(src, dst, nil, nil)
	raw, _ := f.Encode()
	// Overwrite control byte.
	raw[14] = 0x2F // SABM
	f2, err := Decode(raw)
	if err != nil {
		t.Fatal(err)
	}
	if f2.IsUI() {
		t.Error("expected IsUI=false for SABM")
	}
	if f2.Source.Call != "W1AW" || f2.Dest.Call != "W2XX" {
		t.Error("header not parsed on connected-mode frame")
	}
}

func TestDedupKey(t *testing.T) {
	src, _ := ParseAddress("N0CALL-1")
	dst, _ := ParseAddress("APRS")
	f1, _ := NewUIFrame(src, dst, nil, []byte("hello"))
	f2, _ := NewUIFrame(src, dst, []Address{{Call: "WIDE1", SSID: 1}}, []byte("hello"))
	if f1.DedupKey() != f2.DedupKey() {
		t.Error("dedup key should ignore path")
	}
	f3, _ := NewUIFrame(src, dst, nil, []byte("bye"))
	if f1.DedupKey() == f3.DedupKey() {
		t.Error("dedup key should depend on info")
	}
}

func TestPathDedupKey(t *testing.T) {
	src, _ := ParseAddress("N0CALL-1")
	dst, _ := ParseAddress("APRS")
	// Same source+dest+info but different paths -> distinct keys.
	f1, _ := NewUIFrame(src, dst, []Address{{Call: "WIDE1", SSID: 1}}, []byte("hello"))
	f2, _ := NewUIFrame(src, dst, []Address{{Call: "WIDE2", SSID: 2}}, []byte("hello"))
	if f1.PathDedupKey() == f2.PathDedupKey() {
		t.Error("path dedup key should distinguish different paths")
	}
	// Same path should produce the same key.
	f3, _ := NewUIFrame(src, dst, []Address{{Call: "WIDE1", SSID: 1}}, []byte("hello"))
	if f1.PathDedupKey() != f3.PathDedupKey() {
		t.Error("path dedup key should match for identical frames")
	}
	// H-bit changes do not affect the key: a fresh frame and one with
	// its first path slot marked repeated should collapse so a
	// digi-suppression cache sees them as the same observation.
	f4, _ := NewUIFrame(src, dst, []Address{{Call: "WIDE1", SSID: 1, Repeated: true}}, []byte("hello"))
	if f1.PathDedupKey() != f4.PathDedupKey() {
		t.Error("path dedup key should ignore the H-bit")
	}
	// Different info -> different key.
	f5, _ := NewUIFrame(src, dst, []Address{{Call: "WIDE1", SSID: 1}}, []byte("bye"))
	if f1.PathDedupKey() == f5.PathDedupKey() {
		t.Error("path dedup key should depend on info")
	}
}

func TestEncodePathLimit(t *testing.T) {
	src, _ := ParseAddress("N0CALL")
	dst, _ := ParseAddress("APRS")
	path := make([]Address, 9)
	for i := range path {
		path[i] = Address{Call: "W5XYZ", SSID: uint8(i)}
	}
	_, err := NewUIFrame(src, dst, path, []byte("x"))
	if err == nil {
		t.Error("expected error for >8 path entries")
	}
}
