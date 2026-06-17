//go:build linux && !android

package clocksync

import "golang.org/x/sys/unix"

// Check queries the kernel NTP discipline state via adjtimex(2). A zero
// Modes field makes this a read-only query that needs no privileges.
//
// The kernel initializes its NTP status word to STA_UNSYNC at boot and
// clears that bit only once a time source (NTP, chrony, systemd-timesyncd)
// has disciplined the clock -- the same signal `timedatectl`'s "System
// clock synchronized" line is derived from. STA_UNSYNC alone is therefore
// the precise "not disciplined" signal: it covers both the no-daemon case
// (bit still set from boot) and the daemon-not-yet-converged case, while a
// synced clock with a leap second pending (TIME_INS/TIME_DEL) keeps the
// bit clear and is correctly reported as Synced.
func Check() Status {
	var tmx unix.Timex
	if _, err := unix.Adjtimex(&tmx); err != nil {
		return Unknown
	}
	return classify(tmx.Status)
}

// classify maps an adjtimex status word to a Status. Split out so the bit
// logic is unit-testable without a live syscall.
func classify(status int32) Status {
	if status&unix.STA_UNSYNC != 0 {
		return Unsynced
	}
	return Synced
}
