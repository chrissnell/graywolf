//go:build darwin

package serialopen

import (
	"os"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// openPTY returns an open pty master and the path of its slave.
func openPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	m, err := os.OpenFile("/dev/ptmx", os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Skipf("no /dev/ptmx: %v", err)
	}
	fd := int(m.Fd())
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
		t.Fatalf("grantpt: %v", err)
	}
	if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
		t.Fatalf("unlockpt: %v", err)
	}
	var buf [128]byte
	if _, _, e := unix.Syscall(unix.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME),
		uintptr(unsafe.Pointer(&buf[0]))); e != 0 {
		t.Fatalf("ptsname: %v", e)
	}
	n := 0
	for n < len(buf) && buf[n] != 0 {
		n++
	}
	return m, string(buf[:n])
}
