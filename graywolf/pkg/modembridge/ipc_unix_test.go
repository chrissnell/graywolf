//go:build !windows

package modembridge

import (
	"io"
	"runtime"
	"strings"
	"testing"
	"time"
)

// TestReadDialAddrTimeoutNoLeak verifies that readDialAddr does not leak its
// reader goroutine when the readiness deadline fires. The test wires an
// io.Pipe as stdout; nothing is ever written, so the goroutine blocks in
// ReadByte until readDialAddr closes the reader on timeout.
func TestReadDialAddrTimeoutNoLeak(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	before := runtime.NumGoroutine()

	_, err := readDialAddr(pr, 20*time.Millisecond, "/tmp/fake.sock")
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}

	// Give the scheduler a moment; the goroutine must have exited because
	// readDialAddr drained the channel before returning.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("goroutine leak: before=%d after=%d", before, runtime.NumGoroutine())
}

// TestReadDialAddrSuccess covers the happy path: a '\n' byte on the pipe
// unblocks the read and returns the configured socket path.
func TestReadDialAddrSuccess(t *testing.T) {
	pr, pw := io.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = pw.Write([]byte("\n"))
	}()

	addr, err := readDialAddr(pr, time.Second, "/tmp/happy.sock")
	if err != nil {
		t.Fatalf("readDialAddr: %v", err)
	}
	if addr != "/tmp/happy.sock" {
		t.Fatalf("addr = %q, want /tmp/happy.sock", addr)
	}
	<-done
	_ = pr.Close()
	_ = pw.Close()
}
