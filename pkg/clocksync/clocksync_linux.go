//go:build linux && !android

package clocksync

import "golang.org/x/sys/unix"

// maxSyncedError mirrors the kernel's NTP_PHASE_LIMIT: 16 seconds, in
// microseconds. The kernel grows the clock's maximum error by MAXFREQ
// (500 us/sec) between time-source updates and re-asserts STA_UNSYNC once
// maxerror crosses this limit (~8.9 h after the last update). systemd's
// timedated derives "System clock synchronized" from the same threshold.
const maxSyncedError = 16_000_000

// Check queries the kernel NTP discipline state via adjtimex(2). A zero
// Modes field makes this a read-only query that needs no privileges.
//
// We classify on the maxerror field, not the STA_UNSYNC status bit.
// STA_UNSYNC is an unreliable "is the clock disciplined?" signal: chrony
// leaves it set unless the `rtcsync` directive is configured, and
// systemd-timesyncd lets the kernel re-assert it between polls -- both
// produce false "unsynced" reports on hosts whose clocks are in fact
// NTP-synced (the Raspberry Pi false positive this fixes). `timedatectl`'s
// "System clock synchronized" line is computed by systemd-timedated's
// ntp_synced(), which calls adjtimex and returns `maxerror < 16 s`,
// explicitly ignoring STA_UNSYNC. Mirroring that threshold makes Graywolf
// agree with timedatectl across daemons and configurations.
func Check() Status {
	var tmx unix.Timex
	if _, err := unix.Adjtimex(&tmx); err != nil {
		return Unknown
	}
	// Maxerror is int32 on 32-bit arches (e.g. arm) and int64 on 64-bit;
	// widen to int64 so classify stays portable and unit-testable.
	return classify(int64(tmx.Maxerror))
}

// classify maps an adjtimex maxerror (microseconds) to a Status. A clock
// being disciplined by a time source keeps maxerror well under the limit;
// an undisciplined one sits at or above 16 s -- the kernel initializes
// maxerror to NTP_PHASE_LIMIT at boot and lets it grow unbounded with no
// daemon. Split out so the threshold logic is unit-testable without a live
// syscall.
func classify(maxerror int64) Status {
	if maxerror < maxSyncedError {
		return Synced
	}
	return Unsynced
}
