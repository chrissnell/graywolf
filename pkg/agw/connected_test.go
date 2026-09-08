package agw

import (
	"bytes"
	"context"
	"encoding/binary"
	"log/slog"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/ax25conn"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// fakeChannelModes stands in for configstore.ChannelModeLookup. The
// zero-value map answers ChannelModeAPRS for every channel, matching
// the conservative default the real Store applies to unknown rows.
type fakeChannelModes struct {
	modes map[uint32]string
}

func (f *fakeChannelModes) ModeForChannel(_ context.Context, ch uint32) (string, error) {
	if m, ok := f.modes[ch]; ok {
		return m, nil
	}
	return configstore.ChannelModeAPRS, nil
}

// connTestRig bundles the pieces a connected-mode test drives: a live
// server, one dialled client connection, and the sink the LAPB layer
// transmits into.
type connTestRig struct {
	srv  *Server
	conn net.Conn
	sink *fakeSink
	mgr  *ax25conn.Manager
	logs *logBuffer
}

// logBuffer collects the server's log output; slog handlers write from
// the server's goroutines while the test reads.
type logBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *logBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// newConnTestRig starts an AGW server wired to a real ax25conn.Manager
// (the field is a concrete type, so there is nothing to stub) and dials
// one client into it. mode is the channel mode reported for channel 1;
// pass "" to leave the manager unwired, exercising the
// no-connected-mode-available path.
func newConnTestRig(t *testing.T, mode string) *connTestRig {
	t.Helper()
	sink := newFakeSink()
	logs := &logBuffer{}
	cfg := ServerConfig{
		PortCallsigns: []string{"N0CALL"},
		PortToChannel: map[uint8]uint32{0: 1},
		Sink:          sink,
		Logger:        slog.New(slog.NewTextHandler(logs, nil)),
	}
	rig := &connTestRig{sink: sink, logs: logs}
	if mode != "" {
		rig.mgr = ax25conn.NewManager(ax25conn.ManagerConfig{
			TxSink:       sink,
			ChannelModes: &fakeChannelModes{modes: map[uint32]string{1: mode}},
			Logger:       cfg.Logger,
		})
		t.Cleanup(rig.mgr.Close)
		cfg.AX25Manager = rig.mgr
	}

	// Bind then release a port so the server has a concrete address to
	// listen on and the test has one to dial.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	cfg.ListenAddr = ln.Addr().String()
	_ = ln.Close()

	rig.srv = NewServer(cfg)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_ = rig.srv.ListenAndServe(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})

	for i := 0; i < 100; i++ {
		c, err := net.Dial("tcp", cfg.ListenAddr)
		if err == nil {
			rig.conn = c
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if rig.conn == nil {
		t.Fatal("could not connect to test server")
	}
	t.Cleanup(func() { _ = rig.conn.Close() })
	return rig
}

// send writes one client→server frame.
func (r *connTestRig) send(t *testing.T, h *Header, data []byte) {
	t.Helper()
	if err := WriteFrame(r.conn, h, data); err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
}

// expect reads one server→client frame and fails if it is not of kind.
func (r *connTestRig) expect(t *testing.T, kind byte) (*Header, []byte) {
	t.Helper()
	_ = r.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	h, data, err := ReadFrame(r.conn)
	if err != nil {
		t.Fatalf("waiting for %q: %v", kind, err)
	}
	if h.DataKind != kind {
		t.Fatalf("got kind %q (%q), want %q", h.DataKind, data, kind)
	}
	return h, data
}

// connectFrame builds a 'C' request from CallFrom to CallTo on port 0.
func connectFrame(from, to string) *Header {
	return &Header{DataKind: KindConnect, PID: 0xF0, CallFrom: from, CallTo: to}
}

// registerCallsign sends an 'X' Register Callsign frame for call and
// waits for the engine's success ack, as the AGWPE spec has a client do
// before connecting as that callsign. Not every client does -- see
// TestConnectAcceptedWithoutRegistration.
func (r *connTestRig) registerCallsign(t *testing.T, call string) {
	t.Helper()
	r.send(t, &Header{DataKind: KindRegisterCallsign, CallFrom: call}, nil)
	_, data := r.expect(t, KindRegisterCallsign)
	if len(data) != 1 || data[0] != 0x01 {
		t.Fatalf("register callsign %q: ack = %v, want success", call, data)
	}
}

// deliver routes an inbound RF frame to whichever session owns it, the
// same way the RX fanout does in production.
func (r *connTestRig) deliver(t *testing.T, local, peer string, c ax25conn.Control, info []byte) {
	t.Helper()
	r.mgr.Dispatch(1, &ax25conn.Frame{
		Source:  mustAddr(t, peer),
		Dest:    mustAddr(t, local),
		Control: c,
		PID:     0xF0,
		Info:    info,
	})
}

// awaitTx blocks until the session has transmitted its next frame and
// returns it.
func (r *connTestRig) awaitTx(t *testing.T, what string) *ax25.Frame {
	t.Helper()
	select {
	case <-r.sink.ch:
	case <-time.After(3 * time.Second):
		t.Fatalf("timed out waiting for %s to be transmitted", what)
	}
	frames := r.sink.Frames()
	return frames[len(frames)-1]
}

func mustAddr(t *testing.T, s string) ax25.Address {
	t.Helper()
	a, err := ax25.ParseAddress(s)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", s, err)
	}
	return a
}

// The full outbound path an AGWPE client walks: 'C' raises the link,
// the peer's UA turns into a 'C' back to the client, data crosses in
// both directions, and 'd' tears it down.
func TestConnectedModeRoundTrip(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.registerCallsign(t, "W1AW")
	rig.send(t, connectFrame("W1AW", "BBS"), nil)

	if f := rig.awaitTx(t, "SABM"); f.Source.String() != "W1AW" || f.Dest.String() != "BBS" {
		t.Fatalf("SABM addressed %s->%s, want W1AW->BBS", f.Source, f.Dest)
	}

	// Peer accepts.
	rig.deliver(t, "W1AW", "BBS", ax25conn.Control{Kind: ax25conn.FrameUA, PF: true}, nil)

	h, data := rig.expect(t, KindConnect)
	if h.CallFrom != "BBS" || h.CallTo != "W1AW" {
		t.Errorf("connect notice addressed %s->%s, want BBS->W1AW", h.CallFrom, h.CallTo)
	}
	if !strings.Contains(string(data), "CONNECTED") {
		t.Errorf("connect notice text = %q, want a *** CONNECTED line", data)
	}

	// Client → RF.
	rig.send(t, &Header{
		DataKind: KindConnectedData,
		PID:      0xF0,
		CallFrom: "W1AW",
		CallTo:   "BBS",
	}, []byte("hello bbs"))
	if f := rig.awaitTx(t, "I-frame"); string(f.Info) != "hello bbs" {
		t.Errorf("transmitted info = %q, want %q", f.Info, "hello bbs")
	}

	// While that I-frame is unacked, 'Y' must say so.
	rig.send(t, &Header{DataKind: KindOutstandingFrames, CallFrom: "W1AW", CallTo: "BBS"}, nil)
	_, ydata := rig.expect(t, KindOutstandingFrames)
	if n := binary.LittleEndian.Uint32(ydata); n != 1 {
		t.Errorf("outstanding = %d with one unacked I-frame, want 1", n)
	}

	// RF → client. N(R)=1 acks our I-frame, N(S)=0 carries the reply.
	rig.deliver(t, "W1AW", "BBS",
		ax25conn.Control{Kind: ax25conn.FrameI, NS: 0, NR: 1}, []byte("hi ham"))
	h, data = rig.expect(t, KindConnectedData)
	if h.CallFrom != "BBS" || h.CallTo != "W1AW" {
		t.Errorf("data addressed %s->%s, want BBS->W1AW", h.CallFrom, h.CallTo)
	}
	if string(data) != "hi ham" {
		t.Errorf("received data = %q, want %q", data, "hi ham")
	}

	// Client hangs up; the session sends DISC and the peer's UA closes it.
	rig.send(t, &Header{
		DataKind: KindDisconnect,
		PID:      0xF0,
		CallFrom: "W1AW",
		CallTo:   "BBS",
	}, nil)
	if f := rig.awaitTx(t, "DISC"); f.Dest.String() != "BBS" {
		t.Fatalf("DISC addressed to %s, want BBS", f.Dest)
	}
	rig.deliver(t, "W1AW", "BBS", ax25conn.Control{Kind: ax25conn.FrameUA, PF: true}, nil)

	_, data = rig.expect(t, KindDisconnect)
	if !strings.Contains(string(data), "DISCONNECTED") {
		t.Errorf("disconnect notice text = %q, want a *** DISCONNECTED line", data)
	}

	// The session must be retired, or the same link can never be reopened.
	waitFor(t, "session to be removed", func() bool { return rig.mgr.Count() == 0 })
}

// A graceful disconnect must draw exactly one 'd' frame. The session's
// own setState(StateDisconnected) and its Run-loop cleanup used to both
// notify the observer, so the client saw a duplicate -- and a fast
// reconnect landing between the two could have its new session deleted
// by the stale second notification.
func TestDisconnectNotifiesOnce(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.registerCallsign(t, "W1AW")
	rig.send(t, connectFrame("W1AW", "BBS"), nil)
	rig.awaitTx(t, "SABM")
	rig.deliver(t, "W1AW", "BBS", ax25conn.Control{Kind: ax25conn.FrameUA, PF: true}, nil)
	rig.expect(t, KindConnect)

	rig.send(t, &Header{
		DataKind: KindDisconnect,
		PID:      0xF0,
		CallFrom: "W1AW",
		CallTo:   "BBS",
	}, nil)
	rig.awaitTx(t, "DISC")
	rig.deliver(t, "W1AW", "BBS", ax25conn.Control{Kind: ax25conn.FrameUA, PF: true}, nil)

	rig.expect(t, KindDisconnect)

	_ = rig.conn.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	h, data, err := ReadFrame(rig.conn)
	if err == nil {
		t.Fatalf("got unexpected second %q frame (%q) after disconnect", h.DataKind, data)
	}
	if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("want read timeout, got %v", err)
	}
}

// A client that vanishes mid-link must not strand the RF session.
func TestClientDisconnectClosesSession(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.registerCallsign(t, "W1AW")
	rig.send(t, connectFrame("W1AW", "BBS"), nil)
	rig.awaitTx(t, "SABM")
	rig.deliver(t, "W1AW", "BBS", ax25conn.Control{Kind: ax25conn.FrameUA, PF: true}, nil)
	rig.expect(t, KindConnect)

	_ = rig.conn.Close()

	// EventAbort drives a graceful DISC rather than stranding the link.
	if f := rig.awaitTx(t, "DISC"); f.Dest.String() != "BBS" {
		t.Fatalf("DISC addressed to %s, want BBS", f.Dest)
	}
	rig.deliver(t, "W1AW", "BBS", ax25conn.Control{Kind: ax25conn.FrameUA, PF: true}, nil)
	waitFor(t, "session to be removed", func() bool { return rig.mgr.Count() == 0 })
}

// waitFor polls cond until it holds or the test times out. Used for
// state that settles on the session goroutine with no channel to wait on.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// encodeVia builds the AGWPE via payload shared by 'v' and 'V': a
// one-byte digipeater count followed by that many 10-byte NUL-padded
// callsigns.
func encodeVia(calls ...string) []byte {
	out := make([]byte, 1+10*len(calls))
	out[0] = byte(len(calls))
	for i, c := range calls {
		copy(out[1+i*10:1+(i+1)*10], c)
	}
	return out
}

// The 'v' payload is count-prefixed fixed-width fields, not a
// space-separated string. Parsing it the wrong way dropped the whole
// digipeater path and connected direct.
func TestConnectViaUsesDigipeaterPath(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.registerCallsign(t, "W1AW")
	h := connectFrame("W1AW", "BBS")
	h.DataKind = KindConnectVia
	rig.send(t, h, encodeVia("RELAY-1", "WIDE2-2"))

	select {
	case <-rig.sink.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("connect never transmitted a SABM")
	}
	f := rig.sink.Frames()[0]
	if len(f.Path) != 2 {
		t.Fatalf("SABM path = %v, want 2 digipeaters", f.Path)
	}
	if f.Path[0].String() != "RELAY-1" || f.Path[1].String() != "WIDE2-2" {
		t.Errorf("SABM path = %v, want [RELAY-1 WIDE2-2]", f.Path)
	}
}

func TestConnectViaRefusesMalformedPath(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.registerCallsign(t, "W1AW")
	h := connectFrame("W1AW", "BBS")
	h.DataKind = KindConnectVia
	// Count byte claims one digipeater, but its 10-byte field is cut short.
	rig.send(t, h, encodeVia("RELAY-1")[:7])

	_, data := rig.expect(t, KindDisconnect)
	if !strings.Contains(string(data), "digipeater") {
		t.Errorf("refusal text %q should name the digipeater list", data)
	}
}

// A connect that cannot be satisfied must draw a 'd' back. Without it
// the client sits waiting for a 'C' that never arrives -- the "nothing
// happens" symptom of graywolf #561.
func TestConnectRefusedWithoutManager(t *testing.T) {
	rig := newConnTestRig(t, "")
	rig.registerCallsign(t, "W1AW")
	rig.send(t, connectFrame("W1AW", "BBS"), nil)

	h, data := rig.expect(t, KindDisconnect)
	if h.CallFrom != "BBS" || h.CallTo != "W1AW" {
		t.Errorf("refusal addressed %s->%s, want BBS->W1AW", h.CallFrom, h.CallTo)
	}
	if !strings.HasPrefix(string(data), "*** DISCONNECTED") {
		t.Errorf("refusal text %q lacks the *** DISCONNECTED prefix clients match on", data)
	}
}

// Channels default to mode 'aprs', so this is the refusal an operator
// who never touched the channel mode will actually hit. The reason text
// has to name the fix or the log is useless to them.
func TestConnectRefusedOnAPRSOnlyChannel(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModeAPRS)
	rig.registerCallsign(t, "W1AW")
	rig.send(t, connectFrame("W1AW", "BBS"), nil)

	_, data := rig.expect(t, KindDisconnect)
	text := string(data)
	if !strings.Contains(text, "APRS-only") || !strings.Contains(text, "aprs+packet") {
		t.Errorf("refusal %q should name the channel mode and how to fix it", text)
	}
}

func TestConnectRefusedOnInvalidCallsign(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.send(t, connectFrame("W1AW", "NOT A CALL"), nil)

	_, data := rig.expect(t, KindDisconnect)
	if !strings.Contains(string(data), "invalid destination callsign") {
		t.Errorf("refusal text %q should say the callsign was rejected", data)
	}
}

// The AGWPE spec says the originating callsign must have been registered
// with 'X', but EasyTerm registers only its setup callsign and then
// connects as whatever the operator typed. Refusing would pin such
// clients to a single callsign, so the connect goes through and is
// logged instead.
func TestConnectAcceptedWithoutRegistration(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.registerCallsign(t, "N1LKS-2")
	rig.send(t, connectFrame("N1LKS-3", "BBS"), nil)

	if f := rig.awaitTx(t, "SABM"); f.Source.String() != "N1LKS-3" || f.Dest.String() != "BBS" {
		t.Errorf("SABM %s>%s, want N1LKS-3>BBS", f.Source, f.Dest)
	}

	logs := rig.logs.String()
	if !strings.Contains(logs, "connection from unregistered callsign") ||
		!strings.Contains(logs, "call_from=N1LKS-3") {
		t.Errorf("log %q should warn about the unregistered call_from", logs)
	}
}

func TestDuplicateConnectRefused(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.registerCallsign(t, "W1AW")
	rig.send(t, connectFrame("W1AW", "BBS"), nil)

	// The first connect opens a session, which transmits a SABM. Wait
	// for it so the second request cannot race the map insert.
	select {
	case <-rig.sink.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("first connect never transmitted a SABM")
	}

	rig.send(t, connectFrame("W1AW", "BBS"), nil)
	_, data := rig.expect(t, KindDisconnect)
	if !strings.Contains(string(data), "already") {
		t.Errorf("refusal text %q should say the link already exists", data)
	}
}

// Clients are inconsistent about case and about spelling a zero SSID.
// Keying the session map on the raw header bytes meant a link opened as
// "W1AW-0" could not be found by data addressed to "w1aw", and the
// payload was dropped.
func TestSessionLookupNormalizesCallsigns(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.registerCallsign(t, "W1AW-0")
	rig.send(t, connectFrame("W1AW-0", "BBS"), nil)

	select {
	case <-rig.sink.ch:
	case <-time.After(3 * time.Second):
		t.Fatal("connect never transmitted a SABM")
	}

	// Same link, spelled differently. This must reach the session, not
	// draw a "no such link" refusal.
	rig.send(t, &Header{
		DataKind: KindConnectedData,
		PID:      0xF0,
		CallFrom: "w1aw",
		CallTo:   "bbs",
	}, []byte("hello"))

	_ = rig.conn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	h, data, err := ReadFrame(rig.conn)
	if err == nil {
		t.Fatalf("got unexpected %q frame (%q); data should have reached the session",
			h.DataKind, data)
	}
	if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
		t.Fatalf("want read timeout, got %v", err)
	}
}

// Data for a link the server does not know about used to vanish, which
// reads to the operator as the far end ignoring them.
func TestConnectedDataWithoutSessionIsReported(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.send(t, &Header{
		DataKind: KindConnectedData,
		PID:      0xF0,
		CallFrom: "W1AW",
		CallTo:   "BBS",
	}, []byte("hello?"))

	_, data := rig.expect(t, KindDisconnect)
	if !strings.Contains(string(data), "no such link") {
		t.Errorf("refusal text %q should say there is no link", data)
	}
}

// 'Y' must answer with a real count, LSB first. Zero is correct only
// when there is no such link.
func TestOutstandingFramesForUnknownLinkIsZero(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.send(t, &Header{
		DataKind: KindOutstandingFrames,
		CallFrom: "W1AW",
		CallTo:   "BBS",
	}, nil)

	h, data := rig.expect(t, KindOutstandingFrames)
	if len(data) != 4 {
		t.Fatalf("payload is %d bytes, want the spec's 4", len(data))
	}
	if n := binary.LittleEndian.Uint32(data); n != 0 {
		t.Errorf("outstanding = %d with no link open, want 0", n)
	}
	if h.CallFrom != "W1AW" || h.CallTo != "BBS" {
		t.Errorf("reply addressed %s->%s, want the request's pair echoed back",
			h.CallFrom, h.CallTo)
	}
}

func TestDisconnectWithoutSessionIsAcknowledged(t *testing.T) {
	rig := newConnTestRig(t, configstore.ChannelModePacket)
	rig.send(t, &Header{
		DataKind: KindDisconnect,
		PID:      0xF0,
		CallFrom: "W1AW",
		CallTo:   "BBS",
	}, nil)

	rig.expect(t, KindDisconnect)
}
