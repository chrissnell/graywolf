//go:build linux

package gps

import (
	"errors"
	"io"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// openPTY returns the master fd and the slave device path of a fresh
// pseudo-terminal pair. The slave stands in for a real /dev/ttyUSB0.
func openPTY(t *testing.T) (master int, slavePath string) {
	t.Helper()
	m, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("cannot open /dev/ptmx: %v", err)
	}
	t.Cleanup(func() { unix.Close(m) })
	if err := unix.IoctlSetPointerInt(m, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlockpt: %v", err)
	}
	n, err := unix.IoctlGetInt(m, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("ptsname: %v", err)
	}
	return m, "/dev/pts/" + strconv.Itoa(n)
}

// TestOpenNMEASerialNotExclusive is the GRA-118 regression: the GPS
// opener must leave the tty openable by the PTT driver. A plain open of
// the same device (what graywolf-modem's PTT path does) must succeed
// while GPS holds the port.
func TestOpenNMEASerialNotExclusive(t *testing.T) {
	_, slave := openPTY(t)

	port, err := openNMEASerial(slave, 4800)
	if err != nil {
		t.Fatalf("openNMEASerial: %v", err)
	}
	defer port.Close()

	// Simulate the PTT driver opening the same tty (ptt_unix.rs flags).
	fd, err := unix.Open(slave, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK|unix.O_CLOEXEC, 0)
	if err != nil {
		t.Fatalf("PTT-style open of shared tty failed (exclusive access held?): %v", err)
	}
	unix.Close(fd)
}

// TestOpenNMEASerialDeassertsRTS verifies GPS holds RTS/DTR low on open
// so it does not key a shared-line PTT (#305).
func TestOpenNMEASerialDeassertsRTS(t *testing.T) {
	_, slave := openPTY(t)

	port, err := openNMEASerial(slave, 4800)
	if err != nil {
		t.Fatalf("openNMEASerial: %v", err)
	}
	defer port.Close()

	fd, err := unix.Open(slave, unix.O_RDWR|unix.O_NOCTTY|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Fatalf("open shared tty: %v", err)
	}
	defer unix.Close(fd)
	bits, err := unix.IoctlGetInt(fd, unix.TIOCMGET)
	if err == unix.ENOTTY {
		// A pty fundamentally has no modem-control lines, so the #305
		// RTS/DTR deassert can only be verified on real hardware (and is
		// pinned by invariant #47). The opener's ENOTTY tolerance is what
		// lets the rest of these tests run on a pty at all.
		t.Skip("pty has no modem-control lines; RTS deassert not observable here")
	}
	if err != nil {
		t.Fatalf("TIOCMGET: %v", err)
	}
	if bits&unix.TIOCM_RTS != 0 {
		t.Errorf("RTS asserted after open; would key PTT")
	}
	if bits&unix.TIOCM_DTR != 0 {
		t.Errorf("DTR asserted after open")
	}
}

func TestOpenNMEASerialReadsData(t *testing.T) {
	master, slave := openPTY(t)

	port, err := openNMEASerial(slave, 9600)
	if err != nil {
		t.Fatalf("openNMEASerial: %v", err)
	}
	defer port.Close()
	_ = port.SetReadTimeout(500 * time.Millisecond)

	want := "$GPGGA,sentence\r\n"
	if _, err := unix.Write(master, []byte(want)); err != nil {
		t.Fatalf("write master: %v", err)
	}

	buf := make([]byte, len(want))
	got := 0
	deadline := time.Now().Add(2 * time.Second)
	for got < len(want) && time.Now().Before(deadline) {
		n, err := port.Read(buf[got:])
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		got += n
	}
	if string(buf[:got]) != want {
		t.Errorf("read %q, want %q", buf[:got], want)
	}
}

// TestOpenNMEASerialCloseUnblocksRead verifies Close() wakes a blocked
// Read promptly via the wake pipe rather than waiting out the timeout.
func TestOpenNMEASerialCloseUnblocksRead(t *testing.T) {
	_, slave := openPTY(t)

	port, err := openNMEASerial(slave, 9600)
	if err != nil {
		t.Fatalf("openNMEASerial: %v", err)
	}
	_ = port.SetReadTimeout(10 * time.Second)

	done := make(chan error, 1)
	go func() {
		_, err := port.Read(make([]byte, 64))
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	if err := port.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	select {
	case err := <-done:
		if !errors.Is(err, io.EOF) {
			t.Errorf("read after close returned %v, want io.EOF", err)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Read did not unblock after Close")
	}
}
