package kiss

import (
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
)

// TestManager_OnBroadcastSuppressedFires verifies the self-loop guard
// in BroadcastFromChannel fires the OnBroadcastSuppressed observation
// hook for every skipped recipient, and does NOT fire it on normal
// (non-skipped) recipients. Uses the public API only — no hot-running
// servers; BroadcastFromChannel's observable side effect for the test
// is the hook, not the (empty) set of fan-out writes.
func TestManager_OnBroadcastSuppressedFires(t *testing.T) {
	var suppressCalls atomic.Int64
	var lastID atomic.Uint32
	m := NewManager(ManagerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnBroadcastSuppressed: func(recipientID uint32) {
			suppressCalls.Add(1)
			lastID.Store(recipientID)
		},
	})

	// Register two bare Server instances directly on the Manager's
	// `running` map — bypasses Start's goroutine lifecycle so the test
	// doesn't need a TCP listener. BroadcastFromChannel only reads
	// `id` and `srv` from each entry; the test never touches the
	// server's socket code path.
	m.running[10] = &managedServer{server: NewServer(ServerConfig{Broadcast: true})}
	m.running[20] = &managedServer{server: NewServer(ServerConfig{Broadcast: true})}

	// skip=true, skipID=10 → only iface 10 should be suppressed.
	m.BroadcastFromChannel(1, []byte{}, 10, true)

	if got := suppressCalls.Load(); got != 1 {
		t.Errorf("suppressCalls = %d, want 1", got)
	}
	if got := lastID.Load(); got != 10 {
		t.Errorf("suppressed recipient = %d, want 10", got)
	}

	// skip=false → no hook fire, regardless of IDs registered.
	suppressCalls.Store(0)
	m.BroadcastFromChannel(1, []byte{}, 10, false)
	if got := suppressCalls.Load(); got != 0 {
		t.Errorf("after skip=false: suppressCalls = %d, want 0", got)
	}
}
