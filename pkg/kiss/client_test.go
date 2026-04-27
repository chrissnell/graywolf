package kiss

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/ax25"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"go.uber.org/goleak"
)

// silentLogger keeps test output clean while still exercising the
// slog call sites (nil would be fine too, but a real handler
// catches accidental nil-deref regressions in the logging code).
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// mockServer drives one side of a net.Pipe. The test harness provides
// a dialFunc to the Client that returns the client-side pipe endpoint;
// the test holds the server-side and reads/writes KISS bytes to it.
type mockServer struct {
	serverConn net.Conn
	clientConn net.Conn
}

// Close releases both ends.
func (ms *mockServer) Close() {
	_ = ms.serverConn.Close()
	_ = ms.clientConn.Close()
}

// makePipeServer returns a mock ready to back one Client.Dial call.
// The Client's DialFunc pulls clientConn; the test drives serverConn.
func makePipeServer() *mockServer {
	s, c := net.Pipe()
	return &mockServer{serverConn: s, clientConn: c}
}

// ubFrame returns the AX.25 bytes of a valid UI frame from src to dst
// with the given info. Used by multiple tests below to avoid
// hand-constructing AX.25 at each call site.
func uiFrame(t *testing.T, src, dst, info string) []byte {
	t.Helper()
	srcA, err := ax25.ParseAddress(src)
	if err != nil {
		t.Fatalf("parse src: %v", err)
	}
	dstA, err := ax25.ParseAddress(dst)
	if err != nil {
		t.Fatalf("parse dst: %v", err)
	}
	f, err := ax25.NewUIFrame(srcA, dstA, nil, []byte(info))
	if err != nil {
		t.Fatalf("build frame: %v", err)
	}
	b, err := f.Encode()
	if err != nil {
		t.Fatalf("encode frame: %v", err)
	}
	return b
}

// sink collects submitted frames so modem-mode tests can assert.
type sink struct {
	mu     sync.Mutex
	frames []*ax25.Frame
}

func (s *sink) Submit(_ context.Context, ch uint32, ax *ax25.Frame, _ txgovernor.SubmitSource) error {
	s.mu.Lock()
	s.frames = append(s.frames, ax)
	s.mu.Unlock()
	_ = ch
	return nil
}

// TestClient_DialAndReadFrame verifies the basic happy path: the
// supervisor dials, reads a single KISS data frame, decodes and
// dispatches it to the sink (modem mode), and exits cleanly on ctx
// cancel.
func TestClient_DialAndReadFrame(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := makePipeServer()
	defer srv.Close()

	var dialCount atomic.Int32
	dialFunc := func(ctx context.Context, _, _ string) (net.Conn, error) {
		if n := dialCount.Add(1); n > 1 {
			return nil, errors.New("dial once only in this test")
		}
		return srv.clientConn, nil
	}

	sk := &sink{}
	cli := newClient(ClientConfig{
		InterfaceID:     42,
		Name:            "test",
		RemoteHost:      "127.0.0.1",
		RemotePort:      9999,
		ReconnectInitMs: 100,
		ReconnectMaxMs:  1000,
		DialFunc:        dialFunc,
		Sink:            sk,
		Logger:          silentLogger(),
		Mode:            ModeModem,
		ChannelMap:      map[uint8]uint32{0: 7},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go cli.run(ctx)

	// Wait for the supervisor to transition to StateConnected.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cli.Status().State == StateConnected {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := cli.Status().State; got != StateConnected {
		t.Fatalf("client did not connect: state=%q", got)
	}

	// Push one KISS data frame from the server side.
	kissBytes := Encode(0, uiFrame(t, "N0CALL", "APRS", "hello world"))
	if _, err := srv.serverConn.Write(kissBytes); err != nil {
		t.Fatalf("server write: %v", err)
	}

	// Wait for the sink to record it.
	deadline = time.Now().Add(2 * time.Second)
	var got []*ax25.Frame
	for time.Now().Before(deadline) {
		sk.mu.Lock()
		got = append(got[:0], sk.frames...)
		sk.mu.Unlock()
		if len(got) >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 frame in sink, got %d", len(got))
	}
	if !bytes.Equal(got[0].Info, []byte("hello world")) {
		t.Errorf("frame info = %q, want %q", got[0].Info, "hello world")
	}

	cancel()
	cli.close()
	if s := cli.Status().State; s != StateStopped && s != StateBackoff {
		// Backoff is acceptable if cancel raced the post-EOF
		// transition; stopped is the clean case.
		t.Errorf("state after cancel = %q, want stopped or backoff", s)
	}
}

// TestClient_ReconnectsAfterEOF verifies that when the server closes
// the conn mid-stream, the supervisor transitions to Backoff and then
// dials again. The test flips the DialFunc to return a fresh pipe on
// the second attempt.
func TestClient_ReconnectsAfterEOF(t *testing.T) {
	defer goleak.VerifyNone(t)

	var dialCount atomic.Int32
	var mu sync.Mutex
	var currentSrv *mockServer

	getCurrent := func() *mockServer {
		mu.Lock()
		defer mu.Unlock()
		return currentSrv
	}

	dialFunc := func(_ context.Context, _, _ string) (net.Conn, error) {
		n := dialCount.Add(1)
		if n > 3 {
			return nil, errors.New("enough")
		}
		srv := makePipeServer()
		mu.Lock()
		if currentSrv != nil {
			_ = currentSrv.serverConn.Close()
		}
		currentSrv = srv
		mu.Unlock()
		return srv.clientConn, nil
	}

	cli := newClient(ClientConfig{
		InterfaceID:     5,
		Name:            "reconnect-test",
		RemoteHost:      "127.0.0.1",
		RemotePort:      9999,
		ReconnectInitMs: 50,
		ReconnectMaxMs:  200,
		DialFunc:        dialFunc,
		Sink:            &sink{},
		Logger:          silentLogger(),
		Mode:            ModeModem,
		ChannelMap:      map[uint8]uint32{0: 1},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); cli.close() }()
	go cli.run(ctx)

	// Wait for first connect.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && cli.Status().State != StateConnected {
		time.Sleep(5 * time.Millisecond)
	}
	if cli.Status().State != StateConnected {
		t.Fatalf("first connect did not happen")
	}

	// Kill the server side — supervisor should observe EOF and
	// transition to Backoff, then reconnect. Reconnect count reaches
	// 2 after the second successful dial.
	initialReconnectCount := cli.Status().ReconnectCount
	srv := getCurrent()
	_ = srv.serverConn.Close()

	// Wait until the reconnect count increments AND we're connected
	// again — this is the atomic signal that the supervisor noticed
	// the EOF, ran backoff, and dialed a second time. We don't
	// assert on observing Backoff intermediate state because
	// short backoff (50ms) can race the poll loop.
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		st := cli.Status()
		if st.State == StateConnected && st.ReconnectCount > initialReconnectCount {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	final := cli.Status()
	if final.State != StateConnected {
		t.Errorf("did not reconnect: state=%q", final.State)
	}
	if final.ReconnectCount <= initialReconnectCount {
		t.Errorf("ReconnectCount=%d, want >%d", final.ReconnectCount, initialReconnectCount)
	}
}

// TestClient_ReconnectNowShortCircuitsBackoff verifies that
// Reconnect() wakes the supervisor out of its backoff sleep so the
// next dial happens immediately.
func TestClient_ReconnectNowShortCircuitsBackoff(t *testing.T) {
	defer goleak.VerifyNone(t)

	var dialCount atomic.Int32
	dialFunc := func(_ context.Context, _, _ string) (net.Conn, error) {
		n := dialCount.Add(1)
		if n <= 1 {
			return nil, errors.New("boom")
		}
		// Second attempt: succeed with a fresh pipe.
		srv := makePipeServer()
		return srv.clientConn, nil
	}

	cli := newClient(ClientConfig{
		InterfaceID:     99,
		Name:            "reconnect-now",
		RemoteHost:      "127.0.0.1",
		RemotePort:      9999,
		// Very long backoff so the test proves Reconnect() overrides it.
		ReconnectInitMs: 10000,
		ReconnectMaxMs:  10000,
		DialFunc:        dialFunc,
		Sink:            &sink{},
		Logger:          silentLogger(),
		Mode:            ModeModem,
		ChannelMap:      map[uint8]uint32{0: 1},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); cli.close() }()
	go cli.run(ctx)

	// Wait for state to enter Backoff (after the first dial fails).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && cli.Status().State != StateBackoff {
		time.Sleep(5 * time.Millisecond)
	}
	if cli.Status().State != StateBackoff {
		t.Fatalf("never entered Backoff: state=%q", cli.Status().State)
	}

	// Fire Reconnect() and expect a quick transition to Connected
	// (well before the 10s backoff would elapse).
	start := time.Now()
	cli.Reconnect()
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && cli.Status().State != StateConnected {
		time.Sleep(5 * time.Millisecond)
	}
	if cli.Status().State != StateConnected {
		t.Fatalf("Reconnect did not short-circuit backoff: state=%q", cli.Status().State)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("Reconnect took %v, expected near-immediate", elapsed)
	}
}

// TestClient_TxFromGovernor verifies that when AllowTxFromGovernor is
// true and Mode=tnc, frames pushed through the instance queue reach
// the remote peer via KISS framing.
func TestClient_TxFromGovernor(t *testing.T) {
	defer goleak.VerifyNone(t)

	srv := makePipeServer()
	defer srv.Close()

	dialFunc := func(_ context.Context, _, _ string) (net.Conn, error) {
		return srv.clientConn, nil
	}

	// TNC mode with a minimal RxIngress that drops frames (we're
	// testing TX; RxIngress just has to be non-nil for the mode to
	// work).
	rxIngress := func(_ *pb.ReceivedFrame, _ ingress.Source) {}

	cli := newClient(ClientConfig{
		InterfaceID:         12,
		Name:                "tx-test",
		RemoteHost:          "127.0.0.1",
		RemotePort:          9999,
		ReconnectInitMs:     100,
		ReconnectMaxMs:      500,
		DialFunc:            dialFunc,
		Logger:              silentLogger(),
		Mode:                ModeTnc,
		AllowTxFromGovernor: true,
		ChannelMap:          map[uint8]uint32{0: 11},
		RxIngress:           rxIngress,
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); cli.close() }()
	go cli.run(ctx)

	// Wait for connect + queue install.
	deadline := time.Now().Add(2 * time.Second)
	var q *instanceTxQueue
	for time.Now().Before(deadline) {
		q = cli.instanceQueue()
		if q != nil && cli.Status().State == StateConnected {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if q == nil {
		t.Fatalf("instance queue never installed")
	}

	frame := uiFrame(t, "N0CALL", "APRS", "tx from governor")
	if err := q.Enqueue(frame, 1); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// Read the KISS bytes from the server side and decode.
	_ = srv.serverConn.SetReadDeadline(time.Now().Add(2 * time.Second))
	d := NewDecoder(srv.serverConn)
	f, err := d.Next()
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if f.Command != CmdDataFrame {
		t.Errorf("cmd=%d, want %d", f.Command, CmdDataFrame)
	}
	// Decode AX.25 and confirm the info field.
	ax, err := ax25.Decode(f.Data)
	if err != nil {
		t.Fatalf("ax25 decode: %v", err)
	}
	if !bytes.Equal(ax.Info, []byte("tx from governor")) {
		t.Errorf("frame info = %q, want %q", ax.Info, "tx from governor")
	}
}

// TestClient_StopsOnCancel verifies that the supervisor exits cleanly
// when the parent context is cancelled, even when it's mid-backoff.
func TestClient_StopsOnCancel(t *testing.T) {
	defer goleak.VerifyNone(t)

	// DialFunc that always fails → supervisor stays in Backoff loop.
	dialFunc := func(_ context.Context, _, _ string) (net.Conn, error) {
		return nil, errors.New("nope")
	}
	cli := newClient(ClientConfig{
		InterfaceID:     7,
		Name:            "cancel-test",
		RemoteHost:      "127.0.0.1",
		RemotePort:      9999,
		ReconnectInitMs: 500,
		ReconnectMaxMs:  500,
		DialFunc:        dialFunc,
		Sink:            &sink{},
		Logger:          silentLogger(),
		Mode:            ModeModem,
		ChannelMap:      map[uint8]uint32{0: 1},
	})

	ctx, cancel := context.WithCancel(context.Background())
	go cli.run(ctx)

	// Wait for backoff state (first dial fails after resolving nope).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && cli.Status().State != StateBackoff {
		time.Sleep(5 * time.Millisecond)
	}

	// Cancel + close. cli.close() blocks on done — goleak would catch
	// a leaked supervisor goroutine.
	cancel()
	cli.close()
}

// TestClient_DialErrorSetsLastError verifies that a failed dial
// populates LastError and RetryAtUnixMs on the Status() snapshot
// consumed by the UI.
func TestClient_DialErrorSetsLastError(t *testing.T) {
	defer goleak.VerifyNone(t)

	dialFunc := func(_ context.Context, _, _ string) (net.Conn, error) {
		return nil, errors.New("connection refused")
	}
	cli := newClient(ClientConfig{
		InterfaceID:     1,
		Name:            "err-test",
		RemoteHost:      "127.0.0.1",
		RemotePort:      9999,
		ReconnectInitMs: 5000,
		ReconnectMaxMs:  5000,
		DialFunc:        dialFunc,
		Sink:            &sink{},
		Logger:          silentLogger(),
		Mode:            ModeModem,
		ChannelMap:      map[uint8]uint32{0: 1},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer func() { cancel(); cli.close() }()
	go cli.run(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && cli.Status().State != StateBackoff {
		time.Sleep(5 * time.Millisecond)
	}
	st := cli.Status()
	if st.State != StateBackoff {
		t.Fatalf("state = %q, want %q", st.State, StateBackoff)
	}
	if st.LastError == "" {
		t.Errorf("expected non-empty LastError on dial failure")
	}
	if st.RetryAtUnixMs == 0 {
		t.Errorf("expected RetryAtUnixMs > 0 in backoff")
	}
	if st.BackoffSeconds == 0 {
		t.Errorf("expected BackoffSeconds > 0 in backoff")
	}
}

// small sanity guard against accidental removal of the shared error
// constant in tx_queue.go during refactors.
var _ = ErrBackendDown
var _ = io.EOF
