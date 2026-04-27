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

// TestBridgeStopClosesDispatchers verifies that closePendingRequests marks
// the three dispatchers closed so a caller that races past the
// StateRunning fast-path check sees a closed channel from Register and
// rejects itself instead of leaking an entry into a dispatcher that will
// never be drained again.
func TestBridgeStopClosesDispatchers(t *testing.T) {
	b := New(Config{Logger: slog.Default()})

	// Register a pending entry in each dispatcher directly so we can
	// observe that Close drains them.
	_, enumCh := b.enumDispatcher.Register()
	_, toneCh := b.toneDispatcher.Register()
	_, scanCh := b.scanDispatcher.Register()

	b.closePendingRequests()

	// Every waiting channel should receive a zero value (closed channel).
	for name, ch := range map[string]<-chan any{
		"enum": adaptPbAudioDeviceList(enumCh),
		"tone": adaptPbTestToneResult(toneCh),
		"scan": adaptPbInputLevelScanResult(scanCh),
	} {
		select {
		case v, ok := <-ch:
			if ok && v != nil {
				t.Errorf("%s dispatcher delivered non-zero %+v after Close", name, v)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("%s dispatcher did not close channel", name)
		}
	}
}

// The three tiny adapters below widen the typed dispatcher reply channels
// to <-chan any so the preceding table-driven test can share one select.
func adaptPbAudioDeviceList(c <-chan *pb.AudioDeviceList) <-chan any {
	out := make(chan any, 1)
	go func() {
		v, ok := <-c
		if ok {
			out <- v
		}
		close(out)
	}()
	return out
}
func adaptPbTestToneResult(c <-chan *pb.TestToneResult) <-chan any {
	out := make(chan any, 1)
	go func() {
		v, ok := <-c
		if ok {
			out <- v
		}
		close(out)
	}()
	return out
}
func adaptPbInputLevelScanResult(c <-chan *pb.InputLevelScanResult) <-chan any {
	out := make(chan any, 1)
	go func() {
		v, ok := <-c
		if ok {
			out <- v
		}
		close(out)
	}()
	return out
}

// TestBridgeRegistrationAfterStopRejects verifies that once
// closePendingRequests has nil'd the dispatch maps, a caller that forces
// its way past the StateRunning fast-path sees errBridgeStopped at
// registration time instead of leaking a stale pending entry.
func TestBridgeRegistrationAfterStopRejects(t *testing.T) {
	b := New(Config{Logger: slog.Default()})
	b.setState(StateRunning)
	b.setSender(func(*pb.IpcMessage) error { return nil })

	// Drain the dispatch maps, as would happen in supervise's defer.
	b.closePendingRequests()

	if _, err := b.EnumerateAudioDevices(context.Background()); !errors.Is(err, errBridgeStopped) {
		t.Errorf("EnumerateAudioDevices err = %v, want errBridgeStopped", err)
	}
	if _, err := b.ScanInputLevels(context.Background()); !errors.Is(err, errBridgeStopped) {
		t.Errorf("ScanInputLevels err = %v, want errBridgeStopped", err)
	}
	if err := b.PlayTestTone(context.Background(), 0, "x", 48000, 1); !errors.Is(err, errBridgeStopped) {
		t.Errorf("PlayTestTone err = %v, want errBridgeStopped", err)
	}
}
