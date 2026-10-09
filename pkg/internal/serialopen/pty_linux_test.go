//go:build linux

package serialopen

import (
	"fmt"
	"os"
	"testing"

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
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		t.Fatalf("unlockpt: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		t.Fatalf("ptsname: %v", err)
	}
	return m, fmt.Sprintf("/dev/pts/%d", n)
}
