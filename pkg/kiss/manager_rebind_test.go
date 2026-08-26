package kiss

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// errorRecorder is a slog.Handler that keeps every ERROR-level record's
// formatted attributes. Used to catch the "kiss server" bind failure the
// manager only logs — Start has no error return, so the log line is the
// sole signal that a restart left the interface dead.
type errorRecorder struct {
	mu   sync.Mutex
	msgs []string
}

func (e *errorRecorder) Enabled(context.Context, slog.Level) bool { return true }
func (e *errorRecorder) WithAttrs([]slog.Attr) slog.Handler       { return e }
func (e *errorRecorder) WithGroup(string) slog.Handler            { return e }

func (e *errorRecorder) Handle(_ context.Context, r slog.Record) error {
	if r.Level < slog.LevelError {
		return nil
	}
	var sb strings.Builder
	sb.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		sb.WriteString(" ")
		sb.WriteString(a.String())
		return true
	})
	e.mu.Lock()
	e.msgs = append(e.msgs, sb.String())
	e.mu.Unlock()
	return nil
}

func (e *errorRecorder) snapshot() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.msgs...)
}

// reservedAddr returns a loopback address that was bindable a moment ago,
// for tests that immediately rebind it through the manager.
func reservedAddr(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("probe for a free port: %v", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	if err := ln.Close(); err != nil {
		t.Fatalf("close probe listener: %v", err)
	}
	return "127.0.0.1:" + strconv.Itoa(port)
}

// waitConnectable polls until addr accepts a connection or the deadline
// passes.
func waitConnectable(addr string, deadline time.Time) bool {
	for time.Now().Before(deadline) {
		c, err := net.Dial("tcp", addr)
		if err == nil {
			_ = c.Close()
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return false
}

// TestManagerStopWaitsForListenerClose pins the contract that makes the
// manager's stop/start sequence safe: when stopManaged returns, the
// server-listen interface's port is already free.
//
// Server.ListenAndServe documents that the port is rebindable "when it
// returns", and TestKissServerPortFreeAfterCancel guards that from the
// server's side. The manager, however, used to only call cancel() for
// this interface kind and return — cancel merely signals the watcher
// goroutine that closes the listener, so the port was still bound. The
// bind below is deliberately a single attempt with no retry loop: a
// retry would paper over exactly the delay under test.
//
// The client and serial kinds never had this problem; their close()
// blocks on a done channel. This test asserts the server-listen kind
// now behaves the same way.
func TestManagerStopWaitsForListenerClose(t *testing.T) {
	rec := &errorRecorder{}
	mgr := NewManager(ManagerConfig{Logger: slog.New(rec)})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		mgr.StopAll()
	})

	addr := reservedAddr(t)
	mgr.Start(ctx, 1, ServerConfig{
		Name:       "rebind-test",
		ListenAddr: addr,
		ChannelMap: map[uint8]uint32{0: 1},
	})
	if !waitConnectable(addr, time.Now().Add(3*time.Second)) {
		t.Fatalf("server never bound %s; errors=%v", addr, rec.snapshot())
	}

	mgr.Stop(1)

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("port still bound immediately after Stop returned: %v\n"+
			"stopManaged must wait for ListenAndServe to return, not just cancel it", err)
	}
	_ = ln.Close()
}

// TestManagerRestartFreesPreviousListener covers the operator-visible
// path: saving a KISS TNC config calls notifyKissManager, which calls
// Manager.Start on an already-running row. Start stops the old server and
// then immediately binds, so any gap between "cancelled" and "listener
// actually closed" is a live race.
//
// When the new bind loses that race it fails with "address already in
// use". Start has no error return, so the failure surfaces only as a log
// line — meanwhile the row stays registered in m.running and Status()
// still reports it, so the interface looks healthy while nothing is
// listening. The operator sees a KISS TNC that silently stopped accepting
// connections after a config save.
//
// The replacement deliberately binds a DIFFERENT address. Restarting on
// the same address is the real-world case, but asserting on it means
// racing the very window under test — during development the pre-fix code
// only lost that race on roughly 8% of restarts, which makes for a test
// that neither passes nor fails honestly. Using a second address removes
// the interference and turns the real question ("did Start wait for the
// old listener before binding?") into a single deterministic assertion:
// once Start returns, the old address must be free.
func TestManagerRestartFreesPreviousListener(t *testing.T) {
	rec := &errorRecorder{}
	mgr := NewManager(ManagerConfig{Logger: slog.New(rec)})
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(func() {
		cancel()
		mgr.StopAll()
	})

	oldAddr := reservedAddr(t)
	newAddr := reservedAddr(t)
	base := ServerConfig{Name: "rebind-test", ChannelMap: map[uint8]uint32{0: 1}}

	oldCfg := base
	oldCfg.ListenAddr = oldAddr
	mgr.Start(ctx, 1, oldCfg)
	if !waitConnectable(oldAddr, time.Now().Add(3*time.Second)) {
		t.Fatalf("server never bound %s; errors=%v", oldAddr, rec.snapshot())
	}

	// Replace the running row under the same id — the config-save path.
	newCfg := base
	newCfg.ListenAddr = newAddr
	mgr.Start(ctx, 1, newCfg)

	// Single attempt, no retry loop: a retry would paper over exactly the
	// delay under test.
	ln, err := net.Listen("tcp", oldAddr)
	if err != nil {
		t.Fatalf("previous listener still holds %s after Start returned: %v\n"+
			"Start must wait for the replaced server's ListenAndServe to return "+
			"before binding, or a same-address restart races its own predecessor",
			oldAddr, err)
	}
	_ = ln.Close()

	// And the replacement is actually serving.
	if !waitConnectable(newAddr, time.Now().Add(3*time.Second)) {
		t.Fatalf("replacement never bound %s; errors=%v", newAddr, rec.snapshot())
	}
	for _, msg := range rec.snapshot() {
		if strings.Contains(msg, "address already in use") {
			t.Fatalf("restart raced a listener close: %s", msg)
		}
	}
}
