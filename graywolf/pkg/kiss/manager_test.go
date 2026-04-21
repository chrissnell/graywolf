package kiss

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

// TestManager_Status reports every registered interface in
// StateListening with zero-valued supervisor telemetry (Phase 1 only
// supports server-listen). Phase 4 extends the walk to tcp-client
// supervisors.
func TestManager_Status(t *testing.T) {
	m := NewManager(ManagerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	// Empty manager → empty map, not nil.
	got := m.Status()
	if got == nil {
		t.Fatalf("Status() returned nil map")
	}
	if len(got) != 0 {
		t.Errorf("expected empty map, got %+v", got)
	}

	// Bypass Start's goroutine lifecycle the same way
	// TestManager_OnBroadcastSuppressedFires does below — the Status
	// walk reads only the `running` map keys.
	m.running[7] = &managedServer{server: NewServer(ServerConfig{})}
	m.running[42] = &managedServer{server: NewServer(ServerConfig{})}

	got = m.Status()
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(got), got)
	}
	for id, st := range got {
		if st.State != StateListening {
			t.Errorf("iface %d state=%q, want %q", id, st.State, StateListening)
		}
		if st.LastError != "" || st.RetryAtUnixMs != 0 || st.PeerAddr != "" ||
			st.ConnectedSince != 0 || st.ReconnectCount != 0 || st.BackoffSeconds != 0 {
			t.Errorf("iface %d: expected zero-valued Phase 4 placeholders, got %+v", id, st)
		}
	}
}

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

// TestManager_StartClient_ReportsSupervisorStatus verifies that
// StartClient dispatches a Client supervisor and that Manager.Status()
// returns the supervisor's live state (Connecting / Backoff /
// Connected), not the StateListening placeholder used for server-listen
// rows.
func TestManager_StartClient_ReportsSupervisorStatus(t *testing.T) {
	m := NewManager(ManagerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// DialFunc that always fails → client should end up in Backoff,
	// which is the easiest state to observe without a mock server.
	dialFunc := func(_ context.Context, _, _ string) (net.Conn, error) {
		return nil, errors.New("refused")
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	m.StartClient(ctx, 7, ClientConfig{
		Name:            "test-client",
		RemoteHost:      "127.0.0.1",
		RemotePort:      12345,
		ReconnectInitMs: 1000,
		ReconnectMaxMs:  1000,
		DialFunc:        dialFunc,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:            ModeModem,
		ChannelMap:      map[uint8]uint32{0: 3},
	})

	// Poll Status() until we observe the supervisor has advanced
	// past the initial Disconnected state into Connecting or
	// Backoff. Either is acceptable — the state machine transits
	// Disconnected → Connecting → Backoff when the dial fails.
	deadline := time.Now().Add(2 * time.Second)
	var st InterfaceStatus
	for time.Now().Before(deadline) {
		got := m.Status()
		if s, ok := got[7]; ok {
			st = s
			if s.State == StateBackoff || s.State == StateConnecting {
				break
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	if st.State != StateConnecting && st.State != StateBackoff {
		t.Fatalf("expected Connecting or Backoff, got %+v", st)
	}
	if st.State == StateListening {
		t.Errorf("tcp-client reported as server-listen placeholder")
	}

	// Stop the client — Manager.Stop must clean up the supervisor
	// and remove the entry from the status map.
	m.Stop(7)
	if _, ok := m.Status()[7]; ok {
		t.Errorf("Status still reports id=7 after Stop")
	}
}

// TestManager_Reconnect_RoutesToClient verifies that Reconnect(id)
// returns nil on a tcp-client row and a descriptive error on a
// server-listen row or a missing id.
func TestManager_Reconnect_RoutesToClient(t *testing.T) {
	m := NewManager(ManagerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// Missing id → error.
	if err := m.Reconnect(99); err == nil {
		t.Errorf("Reconnect(99) on empty manager: want err, got nil")
	}

	// Server-listen row → not-a-tcp-client error. We bypass Start by
	// installing a managedServer directly (consistent with
	// TestManager_Status's approach).
	m.running[1] = &managedServer{server: NewServer(ServerConfig{}), cancel: func() {}}
	if err := m.Reconnect(1); err == nil {
		t.Errorf("Reconnect on server row: want err, got nil")
	}

	// tcp-client row — start a real supervisor with a failing
	// dialFunc so Reconnect has something to wake.
	dialFunc := func(_ context.Context, _, _ string) (net.Conn, error) {
		return nil, errors.New("refused")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); m.Stop(2) }()
	m.StartClient(ctx, 2, ClientConfig{
		Name:            "reconnect-me",
		RemoteHost:      "127.0.0.1",
		RemotePort:      9999,
		ReconnectInitMs: 60000,
		ReconnectMaxMs:  60000,
		DialFunc:        dialFunc,
		Logger:          slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:            ModeModem,
		ChannelMap:      map[uint8]uint32{0: 1},
	})
	// Wait for supervisor to settle in Backoff.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if m.Status()[2].State == StateBackoff {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if err := m.Reconnect(2); err != nil {
		t.Errorf("Reconnect on tcp-client: want nil, got %v", err)
	}
}
