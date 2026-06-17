//go:build linux && !android

package clocksync

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name   string
		status int32
		want   Status
	}{
		{"clean status", 0, Synced},
		{"disciplined PLL, no unsync bit", unix.STA_PLL, Synced},
		{"boot default / no daemon", unix.STA_UNSYNC, Unsynced},
		{"daemon running but not converged", unix.STA_PLL | unix.STA_UNSYNC, Unsynced},
		// A synced clock with a leap second queued sets STA_INS, not
		// STA_UNSYNC -- it must not be reported as unsynced.
		{"leap insert pending, still synced", unix.STA_INS, Synced},
	}
	for _, tc := range tests {
		if got := classify(tc.status); got != tc.want {
			t.Errorf("%s: classify(%#x) = %d, want %d", tc.name, tc.status, got, tc.want)
		}
	}
}
