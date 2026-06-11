//go:build linux

package clocksync

import "golang.org/x/sys/unix"

// Check queries the kernel NTP discipline state via adjtimex(2). A zero
// Modes field makes this a read-only query that needs no privileges.
// The clock is treated as unsynced when the kernel returns TIME_ERROR
// or sets the STA_UNSYNC status bit -- the same signal `timedatectl`'s
// "System clock synchronized" line is derived from.
func Check() Status {
	var tmx unix.Timex
	state, err := unix.Adjtimex(&tmx)
	if err != nil {
		return Unknown
	}
	if state == unix.TIME_ERROR || tmx.Status&unix.STA_UNSYNC != 0 {
		return Unsynced
	}
	return Synced
}
