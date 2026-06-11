//go:build !linux

package clocksync

// Check has no adjtimex-style query to call on non-Linux hosts, so it
// reports Unknown and the caller stays silent.
func Check() Status { return Unknown }
