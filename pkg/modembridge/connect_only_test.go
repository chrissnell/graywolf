package modembridge

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// TestSupervisor_ConnectOnly_DialsExistingSocket exercises the
// connect-only path: an external listener is set up, the supervisor
// is configured with ExistingSocket pointing at that path, and the
// supervisor's RunSession is invoked once with the dialed conn. The
// test asserts no fork+exec was attempted (BinaryPath is left as a
// nonsense path; a fork attempt would surface a "no such file"
// error and the session would never start).
func TestSupervisor_ConnectOnly_DialsExistingSocket(t *testing.T) {
	dir, err := os.MkdirTemp("", "modembridge-connect-only-")
	if err != nil {
		t.Fatalf("mkdir tmp: %v", err)
	}
	defer os.RemoveAll(dir)
	sockPath := filepath.Join(dir, "modem.sock")

	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	gotSession := make(chan struct{})
	var sessionCount int32
	cfg := supervisorConfig{
		BinaryPath:     "/nonexistent/should-not-be-execed",
		ExistingSocket: sockPath,
		RunSession: func(ctx context.Context, conn net.Conn) error {
			atomic.AddInt32(&sessionCount, 1)
			close(gotSession)
			<-ctx.Done()
			return nil
		},
	}
	sup := newSupervisor(cfg, slog.Default())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sup.Run(ctx)
	}()

	// Accept the inbound dial and drop it back; the supervisor's
	// RunSession then runs against the accepted conn from the
	// dial-side. We don't need to read/write anything to assert that
	// connect-only worked.
	acceptDone := make(chan error, 1)
	go func() {
		c, aerr := ln.Accept()
		if aerr != nil {
			acceptDone <- aerr
			return
		}
		// Hold open until ctx cancel ripples through and supervisor
		// closes the dial side; the accept side's read will then EOF.
		<-ctx.Done()
		_ = c.Close()
		acceptDone <- nil
	}()

	select {
	case <-gotSession:
	case <-time.After(3 * time.Second):
		t.Fatalf("RunSession was not invoked within 3s")
	}

	cancel()
	<-done
	if err := <-acceptDone; err != nil && !errors.Is(err, net.ErrClosed) {
		t.Logf("accept exited with: %v (acceptable)", err)
	}
	if got := atomic.LoadInt32(&sessionCount); got != 1 {
		t.Fatalf("session count = %d, want 1", got)
	}
}

// TestSupervisor_ConnectOnly_DialFailureRetries asserts that a
// missing socket triggers the standard backoff/retry loop rather
// than crashing the supervisor.
func TestSupervisor_ConnectOnly_DialFailureRetries(t *testing.T) {
	cfg := supervisorConfig{
		ExistingSocket:   "/nonexistent/path/to/sock",
		ReadinessTimeout: 100 * time.Millisecond,
		MaxBackoff:       50 * time.Millisecond,
		RunSession: func(ctx context.Context, conn net.Conn) error {
			t.Fatalf("RunSession must not be called when dial fails")
			return nil
		},
	}
	sup := newSupervisor(cfg, slog.Default())

	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		sup.Run(ctx)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("supervisor.Run did not exit on ctx cancel")
	}
}
