package agw

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync/atomic"
	"testing"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// TestSendRawDecodeFallbackCountsStages drives s.dispatch directly with
// a SendRaw frame whose payload is not a valid AX.25 frame on either
// decode attempt. The initial decode fails (stage=initial), the
// skip-byte fallback also fails (stage=fallback), and the frame is
// dropped. OnDecodeError must fire once with each stage.
//
// Driving dispatch directly — rather than exercising the whole TCP
// listener — isolates this test from network flakiness and keeps the
// assertion about the counter wiring, not about accept loops.
func TestSendRawDecodeFallbackCountsStages(t *testing.T) {
	var initial, fallback atomic.Int64
	srv := NewServer(ServerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		OnDecodeError: func(stage string) {
			switch stage {
			case "initial":
				initial.Add(1)
			case "fallback":
				fallback.Add(1)
			default:
				t.Errorf("unexpected stage %q", stage)
			}
		},
	})

	// net.Pipe gives us a live conn whose RemoteAddr() is non-nil; the
	// bad payload test does not touch the wire but the log path still
	// reads cs.conn.RemoteAddr().
	cLocal, cRemote := net.Pipe()
	defer cLocal.Close()
	defer cRemote.Close()

	cs := &clientState{
		conn:      cLocal,
		callsigns: make(map[string]struct{}),
	}
	// Payload that cannot decode as AX.25 under either the direct or
	// skip-byte interpretation. A few bytes of zero is shorter than the
	// minimum ax25 frame, so Decode errors both times.
	bad := []byte{0x00, 0x00, 0x00, 0x00}
	if err := srv.dispatch(context.Background(), cs, &Header{DataKind: KindSendRaw}, bad); err != nil {
		t.Fatalf("dispatch returned error: %v", err)
	}
	if got := initial.Load(); got != 1 {
		t.Errorf("initial stage count = %d, want 1", got)
	}
	if got := fallback.Load(); got != 1 {
		t.Errorf("fallback stage count = %d, want 1", got)
	}
}

// TestSendRawDecodeFallbackSucceedsSkipByte verifies that when the
// first byte is a direwolf-style port prefix and the remaining bytes
// ARE a valid AX.25 frame, OnDecodeError fires once with
// stage=initial (the fallback was needed) but not with
// stage=fallback (the fallback succeeded, frame is forwarded).
func TestSendRawDecodeFallbackSucceedsSkipByte(t *testing.T) {
	var initial, fallback atomic.Int64
	sink := newFakeSink()
	srv := NewServer(ServerConfig{
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Sink:   sink,
		OnDecodeError: func(stage string) {
			switch stage {
			case "initial":
				initial.Add(1)
			case "fallback":
				fallback.Add(1)
			}
		},
	})

	cLocal, cRemote := net.Pipe()
	defer cLocal.Close()
	defer cRemote.Close()

	cs := &clientState{
		conn:      cLocal,
		callsigns: make(map[string]struct{}),
	}

	// Build a valid AX.25 UI frame, then prepend a spurious byte so the
	// initial decode fails but the skip-byte fallback succeeds.
	src, err := ax25.ParseAddress("KK6ABC")
	if err != nil {
		t.Fatal(err)
	}
	dst, err := ax25.ParseAddress("APRS")
	if err != nil {
		t.Fatal(err)
	}
	ui, err := ax25.NewUIFrame(src, dst, nil, []byte("hi"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := ui.Encode()
	if err != nil {
		t.Fatal(err)
	}
	prefixed := append([]byte{0xAA}, raw...)

	if err := srv.dispatch(context.Background(), cs, &Header{DataKind: KindSendRaw, CallFrom: "KK6ABC", CallTo: "APRS"}, prefixed); err != nil {
		t.Fatalf("dispatch returned error: %v", err)
	}
	if got := initial.Load(); got != 1 {
		t.Errorf("initial stage count = %d, want 1", got)
	}
	if got := fallback.Load(); got != 0 {
		t.Errorf("fallback stage count = %d, want 0 (skip-byte retry succeeded)", got)
	}
}
