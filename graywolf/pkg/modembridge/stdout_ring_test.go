package modembridge

import (
	"fmt"
	"io"
	"log/slog"
	"testing"
	"time"
)

// newTestSupervisor returns a bare supervisor suitable for driving
// scanModemStdout directly. The Run loop is never started.
func newTestSupervisor() *supervisor {
	return newSupervisor(supervisorConfig{}, slog.Default())
}

// TestScanModemStdoutRingBuffer writes more lines than stdoutRingMax to
// a pipe feeding scanModemStdout, then closes the writer so the scanner
// sees EOF. The ring must contain only the last stdoutRingMax lines and
// the reader goroutine must actually exit (done closes).
func TestScanModemStdoutRingBuffer(t *testing.T) {
	s := newTestSupervisor()

	pr, pw := io.Pipe()
	done := make(chan struct{})
	go s.scanModemStdout(pr, done)

	const total = 20
	for i := 0; i < total; i++ {
		if _, err := fmt.Fprintf(pw, "line %d\n", i); err != nil {
			t.Fatalf("write line %d: %v", i, err)
		}
	}
	_ = pw.Close()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scanner goroutine did not exit after pipe close")
	}

	ring := s.LastStdout()
	if len(ring) != stdoutRingMax {
		t.Fatalf("ring len = %d, want %d", len(ring), stdoutRingMax)
	}
	// The oldest lines must have been evicted; the buffer contains the
	// tail of the stream.
	wantFirst := fmt.Sprintf("line %d", total-stdoutRingMax)
	if ring[0] != wantFirst {
		t.Errorf("ring[0] = %q, want %q", ring[0], wantFirst)
	}
	wantLast := fmt.Sprintf("line %d", total-1)
	if ring[len(ring)-1] != wantLast {
		t.Errorf("ring[last] = %q, want %q", ring[len(ring)-1], wantLast)
	}
}

// TestScanModemStdoutEmpty verifies an immediate EOF results in an
// empty ring and a cleanly exited goroutine.
func TestScanModemStdoutEmpty(t *testing.T) {
	s := newTestSupervisor()

	pr, pw := io.Pipe()
	_ = pw.Close()

	done := make(chan struct{})
	go s.scanModemStdout(pr, done)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scanner goroutine did not exit")
	}

	if got := len(s.LastStdout()); got != 0 {
		t.Fatalf("ring len = %d, want 0", got)
	}
}
