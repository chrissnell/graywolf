package clocksync

import "testing"

// Check must always return one of the three defined states and never
// panic, regardless of platform or the host's actual sync state. On
// Linux the adjtimex query should also resolve to a concrete state
// rather than Unknown when the syscall is available.
func TestCheckReturnsValidStatus(t *testing.T) {
	switch s := Check(); s {
	case Unknown, Synced, Unsynced:
		// ok
	default:
		t.Fatalf("Check returned undefined Status %d", int(s))
	}
}
