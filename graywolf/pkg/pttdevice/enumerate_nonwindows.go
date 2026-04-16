//go:build !windows

package pttdevice

// enumerateSerialWindows is a no-op stub on non-windows platforms. The real
// implementation in enumerate_windows.go imports go.bug.st/serial/enumerator,
// which requires CGO on linux/darwin; isolating it to windows keeps cross-
// platform builds CGO-free.
func enumerateSerialWindows() []AvailableDevice {
	return nil
}
