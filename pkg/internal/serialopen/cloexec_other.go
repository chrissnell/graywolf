//go:build !(linux || darwin || freebsd || openbsd)

package serialopen

import "go.bug.st/serial"

// Windows handles are not inherited by default.
func closeOnExec(serial.Port) error { return nil }
