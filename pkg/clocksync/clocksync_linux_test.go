//go:build linux && !android

package clocksync

import "testing"

func TestClassify(t *testing.T) {
	tests := []struct {
		name     string
		maxerror int64
		want     Status
	}{
		{"freshly disciplined clock", 0, Synced},
		{"small dispersion, well synced", 1_000, Synced},
		{"converged daemon, mid-poll", maxSyncedError - 1, Synced},
		// At exactly the 16 s limit the kernel re-asserts STA_UNSYNC, and
		// timedatectl reports unsynced too (`maxerror < 16 s`).
		{"at the 16s limit", maxSyncedError, Unsynced},
		{"boot default / no daemon", maxSyncedError + 500_000, Unsynced},
		{"long-undisciplined, grown unbounded", maxSyncedError * 50, Unsynced},
	}
	for _, tc := range tests {
		if got := classify(tc.maxerror); got != tc.want {
			t.Errorf("%s: classify(%d) = %d, want %d", tc.name, tc.maxerror, got, tc.want)
		}
	}
}
