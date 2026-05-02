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

func TestBridge_ObserveStateChange(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 4)
	b, _ := newTestBridge(t, ctx, out, nil)
	b.observe(ax25conn.OutEvent{Kind: ax25conn.OutStateChange, State: ax25conn.StateConnected})
	select {
	case env := <-out:
		if env.Kind != KindState || env.State == nil || env.State.Name != "CONNECTED" {
			t.Fatalf("unexpected envelope: %+v", env)
		}
	default:
		t.Fatal("expected state envelope")
	}
}

func TestBridge_ObserveDataRXBlocksUntilDrain(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope) // unbuffered: forces blocking send
	b, _ := newTestBridge(t, ctx, out, nil)

	done := make(chan struct{})
	go func() {
		b.observe(ax25conn.OutEvent{Kind: ax25conn.OutDataRX, Data: []byte("hello")})
		close(done)
	}()

	select {
	case env := <-out:
		if env.Kind != KindDataRX || string(env.Data) != "hello" {
			t.Fatalf("unexpected envelope: %+v", env)
		}
	case <-time.After(time.Second):
		t.Fatal("observe never produced envelope")
	}
	<-done
}

func TestBridge_ObserveDataRXUnblocksOnCtxCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := make(chan Envelope) // unbuffered, never drained
	b, _ := newTestBridge(t, ctx, out, nil)

	done := make(chan struct{})
	go func() {
		b.observe(ax25conn.OutEvent{Kind: ax25conn.OutDataRX, Data: []byte("hi")})
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("observe did not unblock on ctx cancel")
	}
}

func TestBridge_ObserveLinkStatsDropsWhenFull(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 1)
	out <- Envelope{Kind: KindAck} // saturate
	var buf bytes.Buffer
	lg := captureLogger(&buf)
	b, _ := newTestBridge(t, ctx, out, lg)
	b.observe(ax25conn.OutEvent{Kind: ax25conn.OutLinkStats, Stats: ax25conn.LinkStats{State: ax25conn.StateConnected, RTT: 250 * time.Millisecond}})
	if !strings.Contains(buf.String(), "out buffer full") {
		t.Fatalf("expected drop warning, got %q", buf.String())
	}
}

func TestBridge_ObserveError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	out := make(chan Envelope, 4)
	b, _ := newTestBridge(t, ctx, out, nil)
	b.observe(ax25conn.OutEvent{Kind: ax25conn.OutError, ErrCode: "frmr", ErrMsg: "bad N(R)"})
	env := <-out
	if env.Kind != KindError || env.Error == nil || env.Error.Code != "frmr" {
		t.Fatalf("unexpected envelope: %+v", env)
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
