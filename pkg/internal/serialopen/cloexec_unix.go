//go:build linux || darwin || freebsd || openbsd

package serialopen

import (
	"reflect"

	"go.bug.st/serial"
	"golang.org/x/sys/unix"
)

// portFD returns the descriptor behind a go.bug.st/serial port. The
// library keeps it in the unexported `handle` field of its unixPort and
// offers no accessor, so it is read via reflection (read-only, no unsafe).
func portFD(p serial.Port) (int, error) {
	v := reflect.ValueOf(p)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return -1, errNoDescriptor(p)
	}
	f := v.FieldByName("handle")
	if !f.IsValid() || f.Kind() != reflect.Int {
		return -1, errNoDescriptor(p)
	}
	return int(f.Int()), nil
}

func closeOnExec(p serial.Port) error {
	fd, err := portFD(p)
	if err != nil {
		return err
	}
	unix.CloseOnExec(fd)
	return nil
}
