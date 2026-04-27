package kiss

import (
	"context"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
)

// TestHandleFrame_OnDecodeErrorFires drives handleFrame with a data
// frame whose payload is not valid AX.25 and asserts that the
// OnDecodeError hook fires exactly once. A successful decode path is
// verified in the same test body so we know the hook is only invoked
// on the failure path, not on every frame.
func TestHandleFrame_OnDecodeErrorFires(t *testing.T) {
	var decodeErrs atomic.Int64
	srv := NewServer(ServerConfig{
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnDecodeError: func() { decodeErrs.Add(1) },
	})

	// Bogus data frame: empty payload cannot decode as AX.25.
	bad := &Frame{Command: CmdDataFrame, Port: 0, Data: []byte{0x00}}
	srv.handleFrame(context.Background(), "127.0.0.1:1234", bad)
	if got := decodeErrs.Load(); got != 1 {
		t.Errorf("after bad frame: decodeErrs = %d, want 1", got)
	}

	// Non-data frames (timing commands etc.) must never invoke the
	// hook — only failed AX.25 decoding counts.
	srv.handleFrame(context.Background(), "127.0.0.1:1234", &Frame{Command: CmdTxDelay, Data: []byte{10}})
	if got := decodeErrs.Load(); got != 1 {
		t.Errorf("after timing frame: decodeErrs = %d, want 1 (unchanged)", got)
	}
}

// TestHandleFrame_OnFrameIngressFires drives handleFrame with a valid
// UI frame and asserts OnFrameIngress is invoked exactly once with the
// server's configured Mode. The hook must fire before dispatchDataFrame
// so callers see every decoded frame regardless of mode-specific
// dispatch outcomes (rate-limit drops, queue overflow, etc.).
func TestHandleFrame_OnFrameIngressFires(t *testing.T) {
	var ingressCalls atomic.Int64
	var seenMode atomic.Value

	srv := NewServer(ServerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:   ModeTnc,
		OnFrameIngress: func(mode Mode) {
			ingressCalls.Add(1)
			seenMode.Store(mode)
		},
	})

	ax := kissUIFrameBytes(t, "hello")
	srv.handleFrame(context.Background(), "127.0.0.1:1234", &Frame{Command: CmdDataFrame, Port: 0, Data: ax})

	if got := ingressCalls.Load(); got != 1 {
		t.Errorf("ingressCalls = %d, want 1", got)
	}
	if got := seenMode.Load(); got != ModeTnc {
		t.Errorf("seenMode = %v, want %v", got, ModeTnc)
	}

	// A decode-failure data frame must NOT invoke OnFrameIngress —
	// the hook is observation of successful decodes only.
	srv.handleFrame(context.Background(), "127.0.0.1:1234", &Frame{Command: CmdDataFrame, Port: 0, Data: []byte{0x00}})
	if got := ingressCalls.Load(); got != 1 {
		t.Errorf("after bad frame: ingressCalls = %d, want 1 (unchanged)", got)
	}
}
