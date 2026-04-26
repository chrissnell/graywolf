//go:build !linux

package logbuffer

// Non-Linux platforms never reach the Pi/SD-card branches; these stubs
// keep the path picker compileable. macOS and Windows always use the
// disk-backed default.
func isRaspberryPi(_ string) bool  { return false }
func isSDCardDevice(_ string) bool { return false }
func backingDeviceForPath(string) (string, error) {
	return "", nil
}
