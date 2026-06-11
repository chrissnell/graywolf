// Package clocksync reports whether the host's system clock is being
// disciplined by a time source (NTP, chrony, systemd-timesyncd, ...).
//
// An undisciplined clock skews every time-relative calculation in
// Graywolf: packet ages go wrong (and can even read negative), and the
// web map's Time Range filter hides stations whose beacons fall outside
// the skewed window. See chrissnell/graywolf#234. The startup banner
// calls Check so operators get a heads-up before chasing phantom bugs.
package clocksync

// Status is the outcome of a clock-sync check.
type Status int

const (
	// Unknown means the check could not be performed -- the platform has
	// no query API or the syscall failed. Callers should stay silent
	// rather than warn on a value they couldn't actually measure.
	Unknown Status = iota
	// Synced means the kernel reports the clock is disciplined.
	Synced
	// Unsynced means the kernel reports no time source is disciplining
	// the clock.
	Unsynced
)
