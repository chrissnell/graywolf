// Package serialopen opens serial ports with close-on-exec set.
//
// go.bug.st/serial opens the device with unix.Open(O_RDWR|O_NOCTTY|
// O_NDELAY) and never sets O_CLOEXEC, so any process graywolf spawns
// afterwards (graywolf-modem, restarted at any time by its supervisor)
// inherits the open serial fd. The child then keeps the tty open after
// graywolf closes its own copy: a bluetooth cu.* port is never torn down,
// a "reconnect" reopens the same dead link, and disabling the interface
// does not release the device.
package serialopen

import (
	"fmt"

	"go.bug.st/serial"
)

// Open is serial.Open followed by FD_CLOEXEC on the port's descriptor.
//
// There is a window of a few instructions between open(2) and fcntl(2)
// in which a concurrent fork could still inherit the fd; holding
// syscall.ForkLock across Open would close it but would also block every
// subprocess spawn if a driver's open() hangs (macOS bluetooth nodes can).
//
// A failure to set the flag never fails the open: serial would stop
// working entirely if a go.bug.st upgrade changed its internals. The
// package test (TestOpenSetsCloseOnExec) is what guards that.
func Open(device string, mode *serial.Mode) (serial.Port, error) {
	p, err := serial.Open(device, mode)
	if err != nil {
		return nil, err
	}
	_ = closeOnExec(p)
	return p, nil
}

// errNoDescriptor is returned when the port's fd cannot be located.
func errNoDescriptor(p serial.Port) error {
	return fmt.Errorf("serialopen: cannot locate file descriptor of %T", p)
}
