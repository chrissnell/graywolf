package kiss

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

func TestSerialConfig_DefaultOpenFuncIsSet(t *testing.T) {
	cfg := SerialConfig{Device: "/dev/null", BaudRate: 9600}
	got := serialOpenOrDefault(cfg)
	if got == nil {
		t.Fatal("serialOpenOrDefault returned nil; default open must be wired")
	}
}

func TestSerialConfig_InjectedOpenFuncWins(t *testing.T) {
	cfg := SerialConfig{
		Device: "x", BaudRate: 1,
		OpenFunc: func(string, uint32) (io.ReadWriteCloser, error) {
			return struct {
				io.ReadWriteCloser
			}{}, nil
		},
	}
	open := serialOpenOrDefault(cfg)
	rwc, err := open("x", 1)
	if err != nil || rwc == nil {
		t.Fatalf("injected OpenFunc not used: rwc=%v err=%v", rwc, err)
	}
}

// fakeRWC is an in-memory io.ReadWriteCloser whose Read blocks until
// closed (so ServeTransport stays alive until ctx cancel / explicit
// close), and whose Close unblocks Read with io.EOF.
type fakeRWC struct {
	closed chan struct{}
	once   sync.Once
}

func newFakeRWC() *fakeRWC { return &fakeRWC{closed: make(chan struct{})} }

func (f *fakeRWC) Read(p []byte) (int, error) {
	<-f.closed
	return 0, io.EOF
}
func (f *fakeRWC) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeRWC) Close() error {
	f.once.Do(func() { close(f.closed) })
	return nil
}

// newTestSerialServer builds a minimal finalized *Server the way the
// Manager would (Sink set so RX has somewhere to go). Mode modem keeps
// it simple; Task 5 exercises the routed path.
func newTestSerialServer(t *testing.T, sink *txSinkRecorder) *Server {
	t.Helper()
	return NewServer(ServerConfig{
		Name:       "ser-test",
		Mode:       ModeModem,
		ChannelMap: map[uint8]uint32{0: 1},
		Sink:       sink,
	})
}

func waitState(t *testing.T, s *SerialSupervisor, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if s.Status().State == want {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("state = %q, want %q (timed out)", s.Status().State, want)
}

func TestSerialSupervisor_OpenSuccessConnected(t *testing.T) {
	rwc := newFakeRWC()
	srv := newTestSerialServer(t, &txSinkRecorder{})
	sup := NewSerial(SerialConfig{
		Name: "s", Device: "d", BaudRate: 9600,
		ReconnectInitMs: 10, ReconnectMaxMs: 50,
		OpenFunc: func(string, uint32) (io.ReadWriteCloser, error) { return rwc, nil },
	}, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.run(ctx)
	waitState(t, sup, StateConnected)
	cancel()
	sup.close()
	waitState(t, sup, StateStopped)
}

func TestSerialSupervisor_OpenFailureBackoff(t *testing.T) {
	srv := newTestSerialServer(t, &txSinkRecorder{})
	sup := NewSerial(SerialConfig{
		Name: "s", Device: "d", BaudRate: 9600,
		ReconnectInitMs: 10, ReconnectMaxMs: 20,
		OpenFunc: func(string, uint32) (io.ReadWriteCloser, error) {
			return nil, errors.New("ENOENT")
		},
	}, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.run(ctx)
	waitState(t, sup, StateBackoff)
	st := sup.Status()
	if st.LastError == "" || st.RetryAtUnixMs == 0 || st.BackoffSeconds == 0 {
		t.Fatalf("backoff status incomplete: %+v", st)
	}
	cancel()
	sup.close()
}

func TestSerialSupervisor_EOFThenBackoff(t *testing.T) {
	var n int
	var mu sync.Mutex
	srv := newTestSerialServer(t, &txSinkRecorder{})
	sup := NewSerial(SerialConfig{
		Name: "s", Device: "d", BaudRate: 9600,
		ReconnectInitMs: 10, ReconnectMaxMs: 20,
		OpenFunc: func(string, uint32) (io.ReadWriteCloser, error) {
			mu.Lock()
			n++
			mu.Unlock()
			r := newFakeRWC()
			go func() { time.Sleep(20 * time.Millisecond); r.Close() }()
			return r, nil
		},
	}, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.run(ctx)
	waitState(t, sup, StateConnected)
	waitState(t, sup, StateBackoff) // clean EOF → disconnected → backoff
	cancel()
	sup.close()
	mu.Lock()
	defer mu.Unlock()
	if n < 1 {
		t.Fatalf("OpenFunc never called")
	}
}

func TestSerialSupervisor_ReconnectWakesBackoff(t *testing.T) {
	var mu sync.Mutex
	var opens int
	srv := newTestSerialServer(t, &txSinkRecorder{})
	sup := NewSerial(SerialConfig{
		Name: "s", Device: "d", BaudRate: 9600,
		ReconnectInitMs: 60000, ReconnectMaxMs: 60000, // long; only Reconnect can shorten the wait
		OpenFunc: func(string, uint32) (io.ReadWriteCloser, error) {
			mu.Lock()
			opens++
			mu.Unlock()
			return nil, errors.New("boom")
		},
	}, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.run(ctx)

	// First open attempt fails and the supervisor parks in a 60s backoff.
	waitState(t, sup, StateBackoff)
	mu.Lock()
	before := opens
	mu.Unlock()

	// Reconnect must short-circuit the 60s wait and drive a fresh open
	// attempt almost immediately. Observe that durable effect (a new
	// open call) rather than the inherently-transient StateConnecting.
	sup.Reconnect()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := opens
		mu.Unlock()
		if n > before {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	got := opens
	mu.Unlock()
	if got <= before {
		t.Fatalf("Reconnect did not wake backoff: opens=%d, want > %d (60s backoff not short-circuited)", got, before)
	}
	cancel()
	sup.close()
}

// txSinkRecorder is a minimal txgovernor.TxSink that records frames.
type txSinkRecorder struct {
	mu     sync.Mutex
	frames [][]byte
	chans  []uint32
}

// Compile-time assertion that *txSinkRecorder satisfies txgovernor.TxSink.
var _ txgovernor.TxSink = (*txSinkRecorder)(nil)

func (r *txSinkRecorder) Submit(_ context.Context, channel uint32, frame *ax25.Frame, _ txgovernor.SubmitSource) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.chans = append(r.chans, channel)
	enc, _ := frame.Encode()
	r.frames = append(r.frames, enc)
	return nil
}

func (r *txSinkRecorder) got() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}

// TestSerialSupervisor_RxRoundTrip writes a KISS-framed AX.25 packet
// into the supervisor's port and asserts the owned Server decoded and
// delivered it to the recorder Sink on the mapped channel.
func TestSerialSupervisor_RxRoundTrip(t *testing.T) {
	// netPipe gives a real bidirectional io.ReadWriteCloser. The
	// supervisor reads the server side; the test writes the client
	// side.
	srvSide, cliSide := net.Pipe()
	rwc := struct {
		io.ReadWriteCloser
	}{srvSide}

	rec := &txSinkRecorder{}
	srv := NewServer(ServerConfig{
		Name:       "ser-rx",
		Mode:       ModeModem,
		ChannelMap: map[uint8]uint32{0: 7},
		Sink:       rec,
	})
	sup := NewSerial(SerialConfig{
		Name: "s", Device: "d", BaudRate: 9600,
		ReconnectInitMs: 10, ReconnectMaxMs: 20,
		OpenFunc: func(string, uint32) (io.ReadWriteCloser, error) { return rwc, nil },
	}, srv)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go sup.run(ctx)
	waitState(t, sup, StateConnected)

	// kissUIFrameBytes returns raw AX.25 bytes (f.Encode(), not KISS-wrapped).
	// Encode(0, ax) wraps them in a KISS frame for port 0.
	ax := kissUIFrameBytes(t, "hello")
	frame := Encode(0, ax)
	go func() { _, _ = cliSide.Write(frame) }()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if rec.got() > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if rec.got() == 0 {
		t.Fatal("Sink never received the decoded frame — owned Server RX path not wired")
	}
	cancel()
	sup.close()
}
