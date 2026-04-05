package agw

import (
	"bytes"
	"testing"
)

func TestHeaderRoundTrip(t *testing.T) {
	h := &Header{
		Port:     0,
		DataKind: KindSendUnproto,
		PID:      0xF0,
		CallFrom: "N0CALL-1",
		CallTo:   "APRS",
		DataLen:  5,
		User:     0,
	}
	buf := EncodeHeader(h)
	if len(buf) != HeaderSize {
		t.Fatalf("len=%d", len(buf))
	}
	got, err := DecodeHeader(buf)
	if err != nil {
		t.Fatal(err)
	}
	if got.Port != 0 || got.DataKind != 'M' || got.PID != 0xF0 {
		t.Errorf("fields: %+v", got)
	}
	if got.CallFrom != "N0CALL-1" || got.CallTo != "APRS" {
		t.Errorf("calls: %+v", got)
	}
	if got.DataLen != 5 {
		t.Errorf("data_len=%d", got.DataLen)
	}
}

func TestReadWriteFrame(t *testing.T) {
	var buf bytes.Buffer
	payload := []byte("WIDE1-1 hello world")
	if err := WriteFrame(&buf, &Header{
		DataKind: KindSendUnproto,
		CallFrom: "W1AW",
		CallTo:   "APRS",
	}, payload); err != nil {
		t.Fatal(err)
	}
	h, data, err := ReadFrame(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if h.DataKind != KindSendUnproto {
		t.Errorf("kind=%c", h.DataKind)
	}
	if !bytes.Equal(data, payload) {
		t.Errorf("data mismatch: %q", data)
	}
}
