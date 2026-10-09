//go:build darwin || linux

package serialopen

import (
	"os/exec"
	"strconv"
	"testing"

	"go.bug.st/serial"
	"golang.org/x/sys/unix"
)

func cloexecSet(t *testing.T, fd int) bool {
	t.Helper()
	flags, err := unix.FcntlInt(uintptr(fd), unix.F_GETFD, 0)
	if err != nil {
		t.Fatalf("F_GETFD: %v", err)
	}
	return flags&unix.FD_CLOEXEC != 0
}

// go.bug.st/serial leaves the fd inheritable; Open must fix that. The
// first half pins the upstream behaviour so a library fix is noticed.
func TestOpenSetsCloseOnExec(t *testing.T) {
	master, slave := openPTY(t)
	defer master.Close()

	mode := &serial.Mode{BaudRate: 9600}

	raw, err := serial.Open(slave, mode)
	if err != nil {
		t.Fatalf("serial.Open: %v", err)
	}
	rawFD, err := portFD(raw)
	if err != nil {
		t.Fatalf("portFD: %v (go.bug.st/serial internals changed; update serialopen)", err)
	}
	upstreamLeaks := !cloexecSet(t, rawFD)
	raw.Close()
	if !upstreamLeaks {
		t.Log("go.bug.st/serial now sets CLOEXEC itself; serialopen may be redundant")
	}

	p, err := Open(slave, mode)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer p.Close()
	fd, err := portFD(p)
	if err != nil {
		t.Fatalf("portFD: %v", err)
	}
	if !cloexecSet(t, fd) {
		t.Fatal("FD_CLOEXEC not set on serial port fd")
	}
}

// childHasFD reports whether a freshly spawned child still has fd open
// (a redirect from a closed descriptor makes the shell exit non-zero).
func childHasFD(fd int) bool {
	return exec.Command("/bin/sh", "-c", "true <&"+strconv.Itoa(fd)).Run() == nil
}

// End-to-end: a child spawned while the port is open must not inherit it.
func TestOpenedPortNotInheritedByChild(t *testing.T) {
	master, slave := openPTY(t)
	defer master.Close()
	mode := &serial.Mode{BaudRate: 9600}

	// Negative control: proves the child check can see a leaked tty. Skip
	// rather than fail if upstream starts setting CLOEXEC itself.
	raw, err := serial.Open(slave, mode)
	if err != nil {
		t.Fatalf("serial.Open: %v", err)
	}
	rawFD, _ := portFD(raw)
	leaks := childHasFD(rawFD)
	raw.Close()
	if !leaks {
		t.Skip("child does not inherit a raw go.bug.st port; check cannot discriminate")
	}

	p, err := Open(slave, mode)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer p.Close()
	fd, _ := portFD(p)
	if childHasFD(fd) {
		t.Fatalf("child inherited serial fd %d", fd)
	}
}
