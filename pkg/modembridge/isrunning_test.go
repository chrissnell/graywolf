package modembridge

import (
	"io"
	"log/slog"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// newTestBridge builds a Bridge with a silent logger and no configstore
// so IsRunning's state + liveness logic can be exercised directly.
func newTestBridge() *Bridge {
	return New(Config{
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		FrameBufferSize: 1,
	})
}

// TestIsRunningSocketDisconnected verifies IsRunning returns false when
// the supervisor has never left StateStopped (socket never connected).
func TestIsRunningSocketDisconnected(t *testing.T) {
	b := newTestBridge()
	if b.IsRunning() {
		t.Fatalf("IsRunning true with no connected session (state=%v)", b.State())
	}
}

// TestIsRunningConfiguring verifies IsRunning returns false while the
// supervisor is still configuring (connected but audio pipeline not up).
func TestIsRunningConfiguring(t *testing.T) {
	b := newTestBridge()
	b.setState(StateConfiguring)
	// Even with recent activity, StateConfiguring must not count as
	// running — the modem hasn't started emitting audio yet.
	b.lastActivityUnix.Store(time.Now().UnixNano())
	if b.IsRunning() {
		t.Fatalf("IsRunning true in StateConfiguring")
	}
}

// TestIsRunningConnectedButIdle verifies that a connected session which
// has gone silent for more than the heartbeat timeout returns false.
func TestIsRunningConnectedButIdle(t *testing.T) {
	b := newTestBridge()
	b.setState(StateRunning)
	// Set last activity to 45 s ago (well past the 30 s threshold).
	b.lastActivityUnix.Store(time.Now().Add(-45 * time.Second).UnixNano())
	if b.IsRunning() {
		t.Fatalf("IsRunning true after 45 s of silence (timeout=%v)", bridgeHeartbeatTimeout)
	}
}

// TestIsRunningConnectedWithRecentFrame verifies that a connected
// session with activity inside the heartbeat window returns true.
func TestIsRunningConnectedWithRecentFrame(t *testing.T) {
	b := newTestBridge()
	b.setState(StateRunning)
	b.lastActivityUnix.Store(time.Now().UnixNano())
	if !b.IsRunning() {
		t.Fatalf("IsRunning false with fresh activity + StateRunning")
	}
}

// TestIsRunningJustReconnected verifies that right after the
// supervisor transitions to StateRunning but before any IPC message
// has arrived, IsRunning is still false. The first inbound message
// (which will be ModemReady in production but any dispatched message
// in practice) flips it to true.
func TestIsRunningJustReconnected(t *testing.T) {
	b := newTestBridge()
	b.setState(StateRunning)
	// lastActivityUnix is zero (fresh Bridge) → IsRunning must be false.
	if b.IsRunning() {
		t.Fatalf("IsRunning true with zero activity timestamp")
	}
	// First dispatched message updates the timestamp.
	b.dispatchIPC(&pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{
		StatusUpdate: &pb.StatusUpdate{},
	}})
	if !b.IsRunning() {
		t.Fatalf("IsRunning false after first StatusUpdate")
	}
}

// TestIsRunningDispatchUpdatesActivityForAllMessageKinds verifies that
// each inbound IPC message kind counts as a heartbeat, not just frames.
func TestIsRunningDispatchUpdatesActivityForAllMessageKinds(t *testing.T) {
	b := newTestBridge()
	b.setState(StateRunning)

	cases := []struct {
		name string
		msg  *pb.IpcMessage
	}{
		{"ReceivedFrame", &pb.IpcMessage{Payload: &pb.IpcMessage_ReceivedFrame{
			ReceivedFrame: &pb.ReceivedFrame{Channel: 1, Data: []byte{0xAA}},
		}}},
		{"StatusUpdate", &pb.IpcMessage{Payload: &pb.IpcMessage_StatusUpdate{
			StatusUpdate: &pb.StatusUpdate{},
		}}},
		{"DcdChange", &pb.IpcMessage{Payload: &pb.IpcMessage_DcdChange{
			DcdChange: &pb.DcdChange{Channel: 1, Detected: false},
		}}},
		{"DeviceLevelUpdate", &pb.IpcMessage{Payload: &pb.IpcMessage_DeviceLevelUpdate{
			DeviceLevelUpdate: &pb.DeviceLevelUpdate{DeviceId: 1},
		}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Zero out, then dispatch, then check.
			b.lastActivityUnix.Store(0)
			if b.IsRunning() {
				t.Fatalf("IsRunning true with zero activity")
			}
			// Drain the frames channel if this is a ReceivedFrame so
			// dispatch's non-blocking send does not affect subsequent
			// cases.
			if _, ok := tc.msg.GetPayload().(*pb.IpcMessage_ReceivedFrame); ok {
				go func() { <-b.frames }()
			}
			b.dispatchIPC(tc.msg)
			if !b.IsRunning() {
				t.Fatalf("IsRunning false after %s", tc.name)
			}
		})
	}
}
