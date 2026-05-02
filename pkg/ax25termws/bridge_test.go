package ax25termws

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/ax25conn"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

type nopSink struct{}

func (nopSink) Submit(_ context.Context, _ uint32, _ *ax25.Frame, _ txgovernor.SubmitSource) error {
	return nil
}

func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func captureLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func newTestBridge(t *testing.T, ctx context.Context, out chan Envelope, lg *slog.Logger) (*Bridge, *ax25conn.Manager) {
	t.Helper()
	if lg == nil {
		lg = quietLogger()
	}
	mgr := ax25conn.NewManager(ax25conn.ManagerConfig{TxSink: nopSink{}, Logger: lg})
	t.Cleanup(mgr.Close)
	b := New(BridgeConfig{
		Manager:  mgr,
		Logger:   lg,
		Operator: "op1",
		Ctx:      ctx,
		Out:      out,
	})
	return b, mgr
}

func validConnect() *ConnectArgs {
	return &ConnectArgs{
		ChannelID: 1,
		LocalCall: "ke7xyz",
		LocalSSID: 1,
		DestCall:  "BBS",
		DestSSID:  3,
	}
}

func TestBridge_HandleConnectOpensSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 16)
	b, mgr := newTestBridge(t, ctx, out, nil)

	if err := b.Handle(ctx, Envelope{Kind: KindConnect, Connect: validConnect()}); err != nil {
		t.Fatalf("Handle Connect: %v", err)
	}
	if mgr.Count() != 1 {
		t.Fatalf("expected 1 session, got %d", mgr.Count())
	}
	if b.SessionID() == 0 {
		t.Fatal("session id should be non-zero")
	}
}

func TestBridge_HandleConnectTwiceRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 16)
	b, _ := newTestBridge(t, ctx, out, nil)

	if err := b.Handle(ctx, Envelope{Kind: KindConnect, Connect: validConnect()}); err != nil {
		t.Fatalf("first connect: %v", err)
	}
	err := b.Handle(ctx, Envelope{Kind: KindConnect, Connect: validConnect()})
	if err == nil || !strings.Contains(err.Error(), "already open") {
		t.Fatalf("expected already-open error, got %v", err)
	}
}

func TestBridge_HandleConnectMissingArgs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 16)
	b, _ := newTestBridge(t, ctx, out, nil)
	if err := b.Handle(ctx, Envelope{Kind: KindConnect}); err == nil {
		t.Fatal("expected missing-args error")
	}
}

func TestBridge_HandleConnectBadCallsign(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 16)
	b, _ := newTestBridge(t, ctx, out, nil)
	bad := validConnect()
	bad.DestCall = "TOOLONG7"
	err := b.Handle(ctx, Envelope{Kind: KindConnect, Connect: bad})
	if err == nil {
		t.Fatal("expected dest-address error")
	}
}

func TestBridge_HandleDataBeforeConnectRejected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 16)
	b, _ := newTestBridge(t, ctx, out, nil)
	err := b.Handle(ctx, Envelope{Kind: KindData, Data: []byte("hi")})
	if err == nil || !strings.Contains(err.Error(), "not connected") {
		t.Fatalf("expected not-connected, got %v", err)
	}
}

func TestBridge_HandleUnknownKind(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 16)
	b, _ := newTestBridge(t, ctx, out, nil)
	err := b.Handle(ctx, Envelope{Kind: "bogus"})
	if err == nil {
		t.Fatal("expected unknown-kind error")
	}
}

func TestBridge_HandleDisconnectIsNoopWhenNotConnected(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 16)
	b, _ := newTestBridge(t, ctx, out, nil)
	if err := b.Handle(ctx, Envelope{Kind: KindDisconnect}); err != nil {
		t.Fatalf("disconnect-before-connect should be tolerated: %v", err)
	}
	if err := b.Handle(ctx, Envelope{Kind: KindAbort}); err != nil {
		t.Fatalf("abort-before-connect should be tolerated: %v", err)
	}
}

func TestBridge_HandleConnectChannelAPRSOnly(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 16)
	lookup := aprsOnlyLookup{}
	mgr := ax25conn.NewManager(ax25conn.ManagerConfig{
		TxSink:       nopSink{},
		Logger:       quietLogger(),
		ChannelModes: lookup,
	})
	defer mgr.Close()
	b := New(BridgeConfig{
		Manager:  mgr,
		Logger:   quietLogger(),
		Operator: "op1",
		Ctx:      ctx,
		Out:      out,
	})
	err := b.Handle(ctx, Envelope{Kind: KindConnect, Connect: validConnect()})
	if err == nil || !errors.Is(err, ax25conn.ErrChannelAPRSOnly) {
		t.Fatalf("expected ErrChannelAPRSOnly, got %v", err)
	}
}

type aprsOnlyLookup struct{}

func (aprsOnlyLookup) ModeForChannel(_ context.Context, _ uint32) (string, error) {
	return "aprs", nil
}

// recvWithin reads one envelope from out within d, or fails the test.
// The bridge now serializes observer events through an internal pump
// goroutine, so observer-driven envelopes arrive asynchronously.
func recvWithin(t *testing.T, out <-chan Envelope, d time.Duration) Envelope {
	t.Helper()
	select {
	case env := <-out:
		return env
	case <-time.After(d):
		t.Fatal("timed out waiting for envelope")
		return Envelope{}
	}
}

func TestBridge_ObserveStateChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 4)
	b, _ := newTestBridge(t, ctx, out, nil)
	b.observe(ax25conn.OutEvent{Kind: ax25conn.OutStateChange, State: ax25conn.StateConnected})
	env := recvWithin(t, out, time.Second)
	if env.Kind != KindState || env.State == nil || env.State.Name != "CONNECTED" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestBridge_OnFirstConnectedFiresOnceWithConnectArgs(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 4)
	mgr := ax25conn.NewManager(ax25conn.ManagerConfig{TxSink: nopSink{}, Logger: quietLogger()})
	t.Cleanup(mgr.Close)
	calls := make(chan ConnectArgs, 4)
	b := New(BridgeConfig{
		Manager:  mgr,
		Logger:   quietLogger(),
		Operator: "op1",
		Ctx:      ctx,
		Out:      out,
		OnFirstConnected: func(args ConnectArgs) {
			calls <- args
		},
	})
	if err := b.Handle(ctx, Envelope{Kind: KindConnect, Connect: validConnect()}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	// Two CONNECTED transitions back-to-back: callback must fire only
	// for the first one.
	b.observe(ax25conn.OutEvent{Kind: ax25conn.OutStateChange, State: ax25conn.StateConnected})
	b.observe(ax25conn.OutEvent{Kind: ax25conn.OutStateChange, State: ax25conn.StateConnected})

	select {
	case got := <-calls:
		if got.DestCall != "BBS" || got.LocalCall != "ke7xyz" {
			t.Fatalf("connect args drifted: %+v", got)
		}
	case <-time.After(time.Second):
		t.Fatal("OnFirstConnected never fired")
	}
	select {
	case got := <-calls:
		t.Fatalf("OnFirstConnected fired twice: %+v", got)
	case <-time.After(150 * time.Millisecond):
	}
}

// observe is supposed to be non-blocking on every kind so the session
// goroutine never stalls waiting on the WebSocket.
func TestBridge_ObserveNeverBlocksOnDataRX(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope) // unbuffered + never drained
	b, _ := newTestBridge(t, ctx, out, nil)

	done := make(chan struct{})
	go func() {
		// Push enough events to overflow inbox + observe call must
		// never block even though out is jammed.
		for i := 0; i < inboxSize+10; i++ {
			b.observe(ax25conn.OutEvent{Kind: ax25conn.OutDataRX, Data: []byte("x")})
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("observe blocked the session goroutine")
	}
}

func TestBridge_ObserveDataRXOverflowEmitsErrorEnvelope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// out has 2 slots so the overflow error envelope can land even
	// after the pump pushes the first state envelope.
	out := make(chan Envelope, 2)
	var buf bytes.Buffer
	b, _ := newTestBridge(t, ctx, out, captureLogger(&buf))

	// Saturate the inbox without letting the pump drain it. Easiest
	// reliable way: cancel ctx so the pump exits, then push.
	cancel()
	// Wait for the pump to actually exit before testing overflow.
	deadline := time.Now().Add(time.Second)
	for !b.pumpExited() && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	for i := 0; i < inboxSize; i++ {
		b.observe(ax25conn.OutEvent{Kind: ax25conn.OutDataRX, Data: []byte("x")})
	}
	// inbox is full now; this observe call must drop and signal.
	b.observe(ax25conn.OutEvent{Kind: ax25conn.OutDataRX, Data: []byte("y")})

	if !strings.Contains(buf.String(), "observer inbox full") {
		t.Fatalf("expected inbox-full warning, got %q", buf.String())
	}
	// The overflow error envelope should land on out (out has slots
	// since the pump is gone and we never pushed anything to out
	// from the test side).
	select {
	case env := <-out:
		if env.Kind != KindError || env.Error == nil || env.Error.Code != "rx_overflow" {
			t.Fatalf("expected rx_overflow error envelope, got %+v", env)
		}
	default:
		t.Fatal("expected rx_overflow error envelope on out")
	}
}

func TestBridge_ObserveError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 4)
	b, _ := newTestBridge(t, ctx, out, nil)
	b.observe(ax25conn.OutEvent{Kind: ax25conn.OutError, ErrCode: "frmr", ErrMsg: "bad N(R)"})
	env := recvWithin(t, out, time.Second)
	if env.Kind != KindError || env.Error == nil || env.Error.Code != "frmr" {
		t.Fatalf("unexpected envelope: %+v", env)
	}
}

func TestBridge_PumpExitsOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan Envelope, 4)
	b, _ := newTestBridge(t, ctx, out, nil)
	cancel()
	deadline := time.After(time.Second)
	for {
		if b.pumpExited() {
			return
		}
		select {
		case <-deadline:
			t.Fatal("pump did not exit on ctx cancel")
		case <-time.After(time.Millisecond):
		}
	}
}

func TestBridge_CloseWithNoSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 4)
	b, _ := newTestBridge(t, ctx, out, nil)
	// Should not panic and must complete promptly.
	done := make(chan struct{})
	go func() { b.Close(); close(done) }()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Close hung when no session was active")
	}
}

func TestBridge_CloseSubmitsDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 64)
	b, mgr := newTestBridge(t, ctx, out, nil)
	if err := b.Handle(ctx, Envelope{Kind: KindConnect, Connect: validConnect()}); err != nil {
		t.Fatalf("connect: %v", err)
	}
	if mgr.Count() != 1 {
		t.Fatalf("expected 1 session, got %d", mgr.Count())
	}
	// Close should run a clean DISC; the session goroutine then
	// transitions through AWAITING_RELEASE and exits when its T1
	// chain finishes (with no peer responding the manager removes
	// the session). Allow some time.
	closeDone := make(chan struct{})
	go func() { b.Close(); close(closeDone) }()
	cancel()
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close did not complete")
	}
	// Subsequent Close must be a no-op.
	b.Close()
}

func TestBridge_HandleConnectChannelAPRSOnlyEmitsErrorEnvelope(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 4)
	mgr := ax25conn.NewManager(ax25conn.ManagerConfig{
		TxSink:       nopSink{},
		Logger:       quietLogger(),
		ChannelModes: aprsOnlyLookup{},
	})
	defer mgr.Close()
	b := New(BridgeConfig{
		Manager:  mgr,
		Logger:   quietLogger(),
		Operator: "op1",
		Ctx:      ctx,
		Out:      out,
	})
	defer b.Close()
	err := b.Handle(ctx, Envelope{Kind: KindConnect, Connect: validConnect()})
	if err == nil {
		t.Fatal("expected error from APRS-only channel")
	}
	// Operator-visible error envelope so the UI can render the reason.
	select {
	case env := <-out:
		if env.Kind != KindError || env.Error == nil {
			t.Fatalf("expected KindError envelope, got %+v", env)
		}
	case <-time.After(time.Second):
		t.Fatal("expected KindError envelope on Open failure")
	}
}

func TestParseBackoff(t *testing.T) {
	cases := []struct {
		in   string
		ok   bool
		want ax25conn.Backoff
	}{
		{"", false, 0},
		{"none", true, ax25conn.BackoffNone},
		{"linear", true, ax25conn.BackoffLinear},
		{"Exponential", true, ax25conn.BackoffExponential},
		{"exp", true, ax25conn.BackoffExponential},
		{"bogus", false, 0},
	}
	for _, c := range cases {
		got, ok := parseBackoff(c.in)
		if ok != c.ok || got != c.want {
			t.Fatalf("parseBackoff(%q) = (%v, %v); want (%v, %v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

func TestFormatAddr(t *testing.T) {
	if got := formatAddr("k0swe", 0); got != "K0SWE" {
		t.Fatalf("ssid 0: %q", got)
	}
	if got := formatAddr(" w1aw ", 7); got != "W1AW-7" {
		t.Fatalf("ssid 7: %q", got)
	}
}

func TestLinkStatsToPayload(t *testing.T) {
	got := linkStatsToPayload(ax25conn.LinkStats{
		State: ax25conn.StateConnected,
		VS:    3, VR: 5, VA: 2, RC: 1,
		FramesTX: 17, BytesRX: 9001,
		RTT: 850 * time.Millisecond,
	})
	if got.RTTMS != 850 || got.State != "CONNECTED" || got.BytesRX != 9001 {
		t.Fatalf("payload mismatch: %+v", got)
	}
}
