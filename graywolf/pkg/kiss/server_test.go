package kiss

import (
	"context"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/internal/testsync"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// dialWhenReady opens a TCP connection to addr, retrying while the
// listener is still coming up. Fails the test if the connection can't
// be made within timeout.
func dialWhenReady(t *testing.T, addr string, timeout time.Duration) net.Conn {
	t.Helper()
	var conn net.Conn
	testsync.WaitFor(t, func() bool {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		conn = c
		return true
	}, timeout, "kiss server listener at "+addr)
	return conn
}

// fakeSink embeds the shared testtx.Recorder and adds a per-submit
// signal channel so tests can block until a frame has been handed
// to the sink without polling on Len().
type fakeSink struct {
	*testtx.Recorder
	ch chan struct{}
}

func newFakeSink() *fakeSink {
	s := &fakeSink{
		Recorder: testtx.NewRecorder(),
		ch:       make(chan struct{}, 16),
	}
	s.OnSubmit(func(testtx.Capture) { s.ch <- struct{}{} })
	return s
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

	conn := dialWhenReady(t, srv.cfg.ListenAddr, time.Second)
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
	frames := sink.Frames()
	if len(frames) != 1 {
		t.Fatalf("expected 1 frame, got %d", len(frames))
	}
	got := frames[0]
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

	testsync.WaitFor(t, func() bool { return srv.ActiveClients() == 1 },
		time.Second, "transport client to register")

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

// capturingIngress records every RxIngress invocation for assertions.
type capturingIngress struct {
	mu    sync.Mutex
	calls []ingressCall
}

type ingressCall struct {
	rf  *pb.ReceivedFrame
	src ingress.Source
}

func (c *capturingIngress) fn(rf *pb.ReceivedFrame, src ingress.Source) {
	c.mu.Lock()
	c.calls = append(c.calls, ingressCall{rf: rf, src: src})
	c.mu.Unlock()
}

func (c *capturingIngress) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

func (c *capturingIngress) snapshot() []ingressCall {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]ingressCall, len(c.calls))
	copy(out, c.calls)
	return out
}

// kissUIFrameBytes builds a UI AX.25 frame and returns its encoded bytes.
func kissUIFrameBytes(t *testing.T, info string) []byte {
	t.Helper()
	src, _ := ax25.ParseAddress("N0CALL-1")
	dst, _ := ax25.ParseAddress("APRS")
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
	if err != nil {
		t.Fatalf("build ui frame: %v", err)
	}
	b, err := f.Encode()
	if err != nil {
		t.Fatalf("encode ui frame: %v", err)
	}
	return b
}

// feedFrame connects a client and writes a single KISS data frame
// containing the given AX.25 payload, then closes the connection.
// Callers are expected to block on the downstream sink / ingress
// signal before making assertions; no post-write delay is inserted.
func feedFrame(t *testing.T, addr string, axBytes []byte) {
	t.Helper()
	conn := dialWhenReady(t, addr, time.Second)
	defer conn.Close()
	if _, err := conn.Write(Encode(0, axBytes)); err != nil {
		t.Fatalf("write kiss frame: %v", err)
	}
}

// TestServerModeDispatch asserts the D4 invariant (ModeModem is unchanged)
// and the D2 invariant (ModeTnc never hits the Sink) hold for the server
// dispatch path. A single table covers both modes.
func TestServerModeDispatch(t *testing.T) {
	cases := []struct {
		name       string
		mode       Mode
		wantSink   int
		wantIngest int
	}{
		{"modem mode submits to sink", ModeModem, 1, 0},
		{"tnc mode routes to RxIngress", ModeTnc, 0, 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sink := newFakeSink()
			cap := &capturingIngress{}
			ingestSignal := make(chan struct{}, 4)
			srv := NewServer(ServerConfig{
				InterfaceID: 42,
				Name:        "t",
				ListenAddr:  "127.0.0.1:0",
				Sink:        sink,
				ChannelMap:  map[uint8]uint32{0: 7},
				Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
				Mode:        tc.mode,
				RxIngress: func(rf *pb.ReceivedFrame, src ingress.Source) {
					cap.fn(rf, src)
					ingestSignal <- struct{}{}
				},
			})

			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			srv.cfg.ListenAddr = ln.Addr().String()
			_ = ln.Close()

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			serveDone := make(chan struct{})
			go func() { _ = srv.ListenAndServe(ctx); close(serveDone) }()

			feedFrame(t, srv.cfg.ListenAddr, kissUIFrameBytes(t, "hello"))

			// Wait on whichever branch we expect to fire.
			waitUntil := time.After(2 * time.Second)
			if tc.wantSink > 0 {
				select {
				case <-sink.ch:
				case <-waitUntil:
					t.Fatal("sink did not receive frame in modem mode")
				}
			}
			if tc.wantIngest > 0 {
				select {
				case <-ingestSignal:
				case <-waitUntil:
					t.Fatal("RxIngress not invoked in tnc mode")
				}
			}

			// No wait needed for the unselected branch: dispatchDataFrame's
			// switch takes exactly one arm per frame, so once the selected
			// arm's signal has fired the other arm cannot still be running.

			if got := sink.Len(); got != tc.wantSink {
				t.Fatalf("sink captures=%d, want %d", got, tc.wantSink)
			}
			if got := cap.count(); got != tc.wantIngest {
				t.Fatalf("ingest calls=%d, want %d", got, tc.wantIngest)
			}

			if tc.mode == ModeTnc && tc.wantIngest > 0 {
				got := cap.snapshot()[0]
				if got.src.Kind != ingress.KindKissTnc || got.src.ID != 42 {
					t.Fatalf("src=%+v, want KindKissTnc id=42", got.src)
				}
				if got.rf.Channel != 7 {
					t.Fatalf("rf.Channel=%d, want 7 (from ChannelMap)", got.rf.Channel)
				}
				if len(got.rf.Data) == 0 {
					t.Fatal("rf.Data is empty")
				}
			}

			cancel()
			select {
			case <-serveDone:
			case <-time.After(2 * time.Second):
				t.Fatal("serve did not return")
			}
		})
	}
}

// TestServerTncRateLimitDrops exercises the per-interface rate limiter:
// three frames arrive back-to-back against a burst of 1, so two must
// drop. The limiter is deterministic under a fake clock because no
// time passes between writes.
func TestServerTncRateLimitDrops(t *testing.T) {
	var ingestCount atomic.Int32
	ingestSignal := make(chan struct{}, 8)
	clk := newFakeClock()
	srv := NewServer(ServerConfig{
		InterfaceID:      7,
		Name:             "tnc",
		ChannelMap:       map[uint8]uint32{0: 1},
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		Mode:             ModeTnc,
		TncIngressRateHz: 1,
		TncIngressBurst:  1,
		Clock:            clk,
		RxIngress: func(_ *pb.ReceivedFrame, _ ingress.Source) {
			ingestCount.Add(1)
			ingestSignal <- struct{}{}
		},
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv.cfg.ListenAddr = ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	serveDone := make(chan struct{})
	go func() { _ = srv.ListenAndServe(ctx); close(serveDone) }()

	ax := kissUIFrameBytes(t, "burst")
	// Open a single persistent connection and send three frames
	// back-to-back on it. The server's handleFrame is serial per
	// connection, so the three frames traverse the limiter in order
	// with no clock advance.
	conn := dialWhenReady(t, srv.cfg.ListenAddr, time.Second)
	defer conn.Close()
	for range 3 {
		if _, err := conn.Write(Encode(0, ax)); err != nil {
			t.Fatalf("write frame: %v", err)
		}
	}

	// Wait for at least one ingest (the burst token) to arrive.
	select {
	case <-ingestSignal:
	case <-time.After(2 * time.Second):
		t.Fatal("expected first frame through limiter")
	}

	// Block on the observable signal that frames 2 and 3 have been
	// decided by the limiter — Dropped is incremented synchronously
	// inside dispatchDataFrame, so once it reads 2 the server has
	// finished with every frame we wrote.
	testsync.WaitFor(t, func() bool { return srv.Dropped() >= 2 },
		time.Second, "two frames to drop at the rate limiter")

	if got := ingestCount.Load(); got != 1 {
		t.Fatalf("ingest count=%d, want 1 (burst=1)", got)
	}
	if got := srv.Dropped(); got != 2 {
		t.Fatalf("Dropped=%d, want 2", got)
	}
	if got := srv.QueueOverflow(); got != 0 {
		t.Fatalf("QueueOverflow=%d, want 0", got)
	}

	cancel()
	select {
	case <-serveDone:
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not return")
	}
}
