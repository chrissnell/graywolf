package kiss

import (
	"errors"
	"io"
	"os"
	"sync/atomic"
	"testing"
	"time"
)

func TestParseBluetoothAddress(t *testing.T) {
	ok := map[string]struct {
		addr string
		ch   int
	}{
		"DC:0D:30:57:04:31":   {"DC:0D:30:57:04:31", 0},
		"dc-0d-30-57-04-31":   {"DC:0D:30:57:04:31", 0},
		"DC:0D:30:57:04:31#3": {"DC:0D:30:57:04:31", 3},
	}
	for in, want := range ok {
		addr, ch, good := ParseBluetoothAddress(in)
		if !good || addr != want.addr || ch != want.ch {
			t.Errorf("ParseBluetoothAddress(%q) = %q,%d,%v want %q,%d,true", in, addr, ch, good, want.addr, want.ch)
		}
	}
	for _, in := range []string{
		"", "/dev/cu.ICOMBTID-5200", "/dev/ttyUSB0", "COM3", "2341:0043",
		"DC:0D:30:57:04", "DC:0D:30:57:04:31#", "DC:0D:30:57:04:31#0", "DC:0D:30:57:04:31#99",
		"DC:0D:30:57:04:31#x",
	} {
		if _, _, good := ParseBluetoothAddress(in); good {
			t.Errorf("ParseBluetoothAddress(%q) accepted", in)
		}
	}
}

func newPipeConn(t *testing.T, write func([]byte) error, closeFn func()) (*btConn, *os.File) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	return &btConn{r: r, write: write, closeFn: closeFn}, w
}

func TestBtConn_ReadWriteAndEOF(t *testing.T) {
	var got []byte
	c, w := newPipeConn(t, func(p []byte) error { got = append(got, p...); return nil }, func() {})
	defer c.Close()

	go func() { w.Write([]byte{0xc0, 0x00, 0x41, 0xc0}) }()
	buf := make([]byte, 8)
	n, err := c.Read(buf)
	if err != nil || n != 4 || buf[0] != 0xc0 {
		t.Fatalf("Read = %d,%v %x", n, err, buf[:n])
	}
	if n, err := c.Write([]byte{1, 2, 3}); err != nil || n != 3 || len(got) != 3 {
		t.Fatalf("Write = %d,%v got=%v", n, err, got)
	}
	// The native layer ends the pipe when the radio drops: Read must EOF so
	// the supervisor reconnects.
	w.Close()
	if _, err := c.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("Read after link drop = %v, want EOF", err)
	}
}

func TestBtConn_WriteErrorPropagates(t *testing.T) {
	boom := errors.New("write timed out")
	c, w := newPipeConn(t, func([]byte) error { return boom }, func() {})
	defer w.Close()
	defer c.Close()
	if n, err := c.Write([]byte{1}); !errors.Is(err, boom) || n != 0 {
		t.Fatalf("Write = %d,%v", n, err)
	}
}

func TestBtConn_CloseOnceUnblocksRead(t *testing.T) {
	var closes atomic.Int32
	c, w := newPipeConn(t, func([]byte) error { return nil }, func() { closes.Add(1) })
	defer w.Close()

	done := make(chan error, 1)
	go func() { _, err := c.Read(make([]byte, 4)); done <- err }()
	time.Sleep(20 * time.Millisecond)
	c.Close()
	c.Close()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("Read returned nil error after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not unblock Read")
	}
	if n := closes.Load(); n != 1 {
		t.Fatalf("native close called %d times, want 1", n)
	}
}

func TestOpenClassicBluetooth_UnsupportedBuild(t *testing.T) {
	if ClassicBluetoothSupported() {
		t.Skip("native transport present")
	}
	if _, err := OpenClassicBluetooth("DC:0D:30:57:04:31", 0); err == nil {
		t.Fatal("want error on unsupported build")
	}
}
