package modembridge

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// TestBridgeStopCancelsPendingRequests verifies that callers blocked in
// EnumerateAudioDevices / ScanInputLevels / PlayTestTone are unblocked with
// errBridgeStopped when the supervisor closes their dispatch channels,
// instead of waiting out the 5s / 30s per-call timeout.
//
// This bypasses the real child spawn (which requires an installed
// graywolf-modem binary) by driving the Bridge directly: force RUNNING
// state, install a no-op sendFn, kick off the request, and then fire
// closePendingRequests to simulate the defer that runs at the end of
// supervise().
func TestBridgeStopCancelsPendingRequests(t *testing.T) {
	cases := []struct {
		name string
		call func(b *Bridge) error
	}{
		{
			name: "EnumerateAudioDevices",
			call: func(b *Bridge) error {
				_, err := b.EnumerateAudioDevices(context.Background())
				return err
			},
		},
		{
			name: "ScanInputLevels",
			call: func(b *Bridge) error {
				_, err := b.ScanInputLevels(context.Background())
				return err
			},
		},
		{
			name: "PlayTestTone",
			call: func(b *Bridge) error {
				return b.PlayTestTone(context.Background(), 0, "fake", 48000, 1)
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := New(Config{Logger: slog.Default()})

			// Force RUNNING state and install a no-op sender so the
			// request registers a pending channel and blocks on the
			// dispatch map.
			b.setState(StateRunning)
			b.setSender(func(*pb.IpcMessage) error { return nil })

			errCh := make(chan error, 1)
			go func() { errCh <- tc.call(b) }()

			// Give the call time to register its pending entry.
			time.Sleep(20 * time.Millisecond)

			// Simulate the supervise() shutdown defer.
			b.closePendingRequests()

			select {
			case err := <-errCh:
				if !errors.Is(err, errBridgeStopped) {
					t.Fatalf("%s: err = %v, want errBridgeStopped", tc.name, err)
				}
			case <-time.After(100 * time.Millisecond):
				t.Fatalf("%s: caller did not return within 100ms after closePendingRequests", tc.name)
			}
		})
	}
}

// TestBridgeStopClearsPendingMaps verifies that closePendingRequests empties
// the three dispatch maps so follow-up requests do not see stale entries.
func TestBridgeStopClearsPendingMaps(t *testing.T) {
	b := New(Config{Logger: slog.Default()})

	// Install one pending entry in each map directly.
	b.enumPending[1] = make(chan *pb.AudioDeviceList, 1)
	b.tonePending[2] = make(chan *pb.TestToneResult, 1)
	b.scanPending[3] = make(chan *pb.InputLevelScanResult, 1)

	b.closePendingRequests()

	if n := len(b.enumPending); n != 0 {
		t.Errorf("enumPending len = %d, want 0", n)
	}
	if n := len(b.tonePending); n != 0 {
		t.Errorf("tonePending len = %d, want 0", n)
	}
	if n := len(b.scanPending); n != 0 {
		t.Errorf("scanPending len = %d, want 0", n)
	}
}
