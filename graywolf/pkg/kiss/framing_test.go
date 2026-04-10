package kiss

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestEncodeDataFrame(t *testing.T) {
	got := Encode(0, []byte{0x01, 0x02, 0x03})
	want := []byte{FEND, 0x00, 0x01, 0x02, 0x03, FEND}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x want %x", got, want)
	}
}

func TestEncodeEscaping(t *testing.T) {
	got := Encode(1, []byte{FEND, FESC, 0x42})
	want := []byte{FEND, 0x10, FESC, TFEND, FESC, TFESC, 0x42, FEND}
	if !bytes.Equal(got, want) {
		t.Errorf("got %x want %x", got, want)
	}
}

func TestDecodeRoundTrip(t *testing.T) {
	payload := []byte{0x00, FEND, 0x01, FESC, 0x02, 0xFF}
	raw := Encode(3, payload)
	d := NewDecoder(bytes.NewReader(raw))
	f, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	if f.Port != 3 {
		t.Errorf("port=%d", f.Port)
	}
	if f.Command != CmdDataFrame {
		t.Errorf("cmd=%x", f.Command)
	}
	if !bytes.Equal(f.Data, payload) {
		t.Errorf("data mismatch: %x", f.Data)
	}
}

func TestDecodeSkipsLeadingFends(t *testing.T) {
	buf := []byte{FEND, FEND, FEND, 0x00, 'h', 'i', FEND}
	d := NewDecoder(bytes.NewReader(buf))
	f, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	if string(f.Data) != "hi" {
		t.Errorf("data=%q", f.Data)
	}
}

func TestDecodeMultipleFrames(t *testing.T) {
	var buf bytes.Buffer
	buf.Write(Encode(0, []byte("one")))
	buf.Write(Encode(0, []byte("two")))
	buf.Write(Encode(0, []byte("three")))
	d := NewDecoder(&buf)
	for _, want := range []string{"one", "two", "three"} {
		f, err := d.Next()
		if err != nil {
			t.Fatal(err)
		}
		if string(f.Data) != want {
			t.Errorf("got %q want %q", f.Data, want)
		}
	}
	if _, err := d.Next(); !errors.Is(err, io.EOF) {
		t.Errorf("expected EOF, got %v", err)
	}
}

func TestDecodeInvalidEscape(t *testing.T) {
	raw := []byte{FEND, 0x00, FESC, 0x99, FEND}
	d := NewDecoder(bytes.NewReader(raw))
	_, err := d.Next()
	if !errors.Is(err, ErrInvalidEscape) {
		t.Errorf("expected ErrInvalidEscape, got %v", err)
	}
}

func TestDecodeFrameTooLarge(t *testing.T) {
	d := &Decoder{MaxFrame: 4}
	d.br = nil
	big := make([]byte, 10)
	for i := range big {
		big[i] = 'a'
	}
	raw := Encode(0, big)
	d = NewDecoder(bytes.NewReader(raw))
	d.MaxFrame = 4
	if _, err := d.Next(); err == nil {
		t.Error("expected error for oversized frame")
	}
}

func TestCommandByte(t *testing.T) {
	raw := EncodeCommand(2, CmdTxDelay, []byte{50})
	d := NewDecoder(bytes.NewReader(raw))
	f, err := d.Next()
	if err != nil {
		t.Fatal(err)
	}
	if f.Port != 2 || f.Command != CmdTxDelay || f.Data[0] != 50 {
		t.Errorf("unexpected frame: %+v", f)
	}
}
