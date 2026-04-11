package kiss

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

type fakeSink struct {
	mu     sync.Mutex
	frames []*ax25.Frame
	ch     chan struct{}
}

func newFakeSink() *fakeSink { return &fakeSink{ch: make(chan struct{}, 16)} }

func (s *fakeSink) Submit(_ context.Context, _ uint32, f *ax25.Frame, _ txgovernor.SubmitSource) error {
	s.mu.Lock()
	s.frames = append(s.frames, f)
	s.mu.Unlock()
	s.ch <- struct{}{}
	return nil
}

func TestServerRoundTrip(t *testing.T) {
	sink := newFakeSink()
	srv := NewServer(ServerConfig{
		Name:       "test",
		ListenAddr: "127.0.0.1:0",
		Sink:       sink,
		ChannelMap: map[uint8]uint32{0: 1},
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
	})

	// Bind an ephemeral port ourselves so we know the address.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.ListenAddr = ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	serveDone := make(chan error, 1)
	go func() { serveDone <- srv.ListenAndServe(ctx) }()

	// Wait for listener to come up.
	var conn net.Conn
	for i := 0; i < 50; i++ {
		c, err := net.Dial("tcp", srv.cfg.ListenAddr)
		if err == nil {
			conn = c
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if conn == nil {
		t.Fatal("could not connect to kiss server")
	}
	defer conn.Close()

	// Build and send a KISS data frame containing an AX.25 UI frame.
	src, _ := ax25.ParseAddress("N0CALL-1")
	dst, _ := ax25.ParseAddress("APRS")
	f, _ := ax25.NewUIFrame(src, dst, nil, []byte("hello"))
	axBytes, _ := f.Encode()
	kissBytes := Encode(0, axBytes)
	if _, err := conn.Write(kissBytes); err != nil {
		t.Fatal(err)
	}

	select {
	case <-sink.ch:
	case <-time.After(2 * time.Second):
		t.Fatal("sink did not receive frame")
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if len(sink.frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(sink.frames))
	}
	got := sink.frames[0]
	if got.Source.Call != "N0CALL" || got.Source.SSID != 1 {
		t.Errorf("source: %+v", got.Source)
	}
	if string(got.Info) != "hello" {
		t.Errorf("info: %q", got.Info)
	}

	// Active client count.
	if n := srv.ActiveClients(); n != 1 {
		t.Errorf("active=%d", n)
	}

	cancel()
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return")
	}
}

func TestServerBroadcast(t *testing.T) {
	srv := NewServer(ServerConfig{
		Name:       "bcast",
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ChannelMap: map[uint8]uint32{0: 1},
	})

	// Plug a pipe directly as a "transport" so we can verify broadcast
	// writes without a real TCP socket.
	clientR, serverW := io.Pipe()
	serverR, clientW := io.Pipe()
	rwc := struct {
		io.Reader
		io.Writer
		io.Closer
	}{serverR, serverW, ioCloserFn(func() error { _ = clientR.Close(); _ = clientW.Close(); return nil })}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() { _ = srv.ServeTransport(ctx, rwc) }()

	// Wait until the client is registered.
	for i := 0; i < 50 && srv.ActiveClients() == 0; i++ {
		time.Sleep(10 * time.Millisecond)
	}
	if srv.ActiveClients() != 1 {
		t.Fatal("transport client not registered")
	}

	// Start the reader before broadcasting — the pipe is unbuffered,
	// so Broadcast would block waiting for a reader otherwise.
	buf := make([]byte, 32)
	done := make(chan []byte, 1)
	go func() {
		n, _ := clientR.Read(buf)
		done <- buf[:n]
	}()

	// Broadcast a canned AX.25 payload.
	srv.Broadcast(0, []byte{0x01, 0x02, 0x03})
	select {
	case b := <-done:
		if len(b) < 5 || b[0] != FEND {
			t.Errorf("unexpected broadcast payload: %x", b)
		}
	case <-time.After(time.Second):
		t.Fatal("no broadcast received")
	}
}

type ioCloserFn func() error

func (f ioCloserFn) Close() error { return f() }
