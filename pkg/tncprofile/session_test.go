package tncprofile

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

type sessionTestPort struct{ io.Reader }

func (p sessionTestPort) Write(b []byte) (int, error) { return len(b), nil }
func (p sessionTestPort) Close() error                { return nil }

func TestKenwoodSessionDetectsPowerCycle(t *testing.T) {
	for _, reply := range []string{"cmd:", "\r\nKenwood Radio Modem\r\n", "\xc0\x00old frame\xc0\r\ncmd:", "F1ZNT-3>APDW13,F1ZJM-3,F1ZCJ-3*,WIDE3-1:!position\r\n", "\xc0\x00old frame\xc0PS 1\rF4BIP-9>TS2QY0,F1ZJM-3,F1ZCJ-3,WIDE2*:`position\r\n"} {
		p := newKenwoodSession(sessionTestPort{bytewiseReader{bytes.NewBufferString(reply)}}, 0)
		if _, err := io.ReadAll(p); !errors.Is(err, ErrKenwoodCommandMode) {
			t.Errorf("%q: %v", reply, err)
		}
	}
}
func TestKenwoodSessionPreservesBinaryPayloadAndStartup(t *testing.T) {
	startup := "banner\r\ncmd:"
	want := startup + "\xc0\x00Kenwood Radio Modem\r\ncmd:\xc0\xc0\x00F1ZNT-3>APDW13,F1ZCJ-3*,WIDE3-1:payload\xc0"
	p := newKenwoodSession(sessionTestPort{bytewiseReader{bytes.NewBufferString(want)}}, len(startup))
	got, err := io.ReadAll(p)
	if err != nil || string(got) != want {
		t.Fatalf("corrupted KISS stream: %q %v", got, err)
	}
}

func TestKenwoodSessionDetectsOnlyAlteredOwnKISSEcho(t *testing.T) {
	addresses := bytes.Repeat([]byte{0x82}, 14)
	addresses[6] = 0x60
	addresses[13] = 0x61
	frame := append([]byte{0xc0, 0}, addresses...)
	frame = append(frame, 3, 0xf0)
	frame = append(frame, []byte("!test position")...)
	frame = append(frame, 0xc0)
	altered := append([]byte(nil), frame[:16]...)
	altered = append(altered, frame[17:]...)
	for _, tc := range []struct {
		name  string
		rx    []byte
		sent  bool
		reset bool
	}{
		{"command echo", altered, true, true},
		{"valid own RF frame", frame, true, false},
		{"unrelated invalid frame", altered, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newKenwoodSession(sessionTestPort{bytewiseReader{bytes.NewReader(tc.rx)}}, 0)
			if tc.sent {
				if _, err := p.Write(frame); err != nil {
					t.Fatal(err)
				}
			}
			got, err := io.ReadAll(p)
			if tc.reset {
				if !errors.Is(err, ErrKenwoodCommandMode) {
					t.Fatalf("echo not detected: %v", err)
				}
			} else if err != nil || !bytes.Equal(got, tc.rx) {
				t.Fatalf("RF frame altered: %x %v", got, err)
			}
		})
	}
}

func TestKenwoodSessionPreservesAllPortsWithBinaryAX25(t *testing.T) {
	addresses := bytes.Repeat([]byte{0x82}, 14)
	addresses[6] = 0x60
	addresses[13] = 0x61
	for port := 0; port < 16; port++ {
		frame := append([]byte{0xc0, byte(port << 4)}, addresses...)
		frame = append(frame, 3, 0xf0)
		frame = append(frame, []byte("cmd:F4BIP-9>TS2QY0,WIDE2*:Kenwood Radio Modem")...)
		frame = append(frame, 0xc0)
		body := bytes.ReplaceAll(frame[1:len(frame)-1], []byte{0xdb}, []byte{0xdb, 0xdd})
		body = bytes.ReplaceAll(body, []byte{0xc0}, []byte{0xdb, 0xdc})
		frame = append(append([]byte{0xc0}, body...), 0xc0)
		p := newKenwoodSession(sessionTestPort{bytewiseReader{bytes.NewReader(frame)}}, 0)
		got, err := io.ReadAll(p)
		if err != nil || !bytes.Equal(got, frame) {
			t.Fatalf("port %d payload corrupted: %x %v", port, got, err)
		}
	}
}
