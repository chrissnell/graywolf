package messages

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// ---------------------------------------------------------------------------
// Test harness
// ---------------------------------------------------------------------------

type fakeTxSink struct {
	mu        sync.Mutex
	submitted []fakeSubmit
	err       error
}

type fakeSubmit struct {
	Channel uint32
	Frame   *ax25.Frame
	Src     txgovernor.SubmitSource
}

func (f *fakeTxSink) Submit(_ context.Context, ch uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.submitted = append(f.submitted, fakeSubmit{Channel: ch, Frame: frame, Src: src})
	return nil
}

func (f *fakeTxSink) list() []fakeSubmit {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeSubmit, len(f.submitted))
	copy(out, f.submitted)
	return out
}

type fakeIGateSender struct {
	mu    sync.Mutex
	lines []string
	err   error
}

func (f *fakeIGateSender) SendLine(l string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.lines = append(f.lines, l)
	return nil
}

func (f *fakeIGateSender) list() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.lines))
	copy(out, f.lines)
	return out
}

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// buildRouter wires a Router with fakes. Returns the router, store,
// sink, igate sender, clock, and event subscription channel.
func buildRouter(t *testing.T, ourCall string, tacticals []string) (
	*Router,
	*Store,
	*fakeTxSink,
	*fakeIGateSender,
	*fakeClock,
	<-chan Event,
	func(),
) {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	store := NewStore(cs.DB())

	sink := &fakeTxSink{}
	igs := &fakeIGateSender{}
	clock := &fakeClock{now: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)}
	ring := NewLocalTxRing(16, time.Minute)
	set := NewTacticalSet()
	if len(tacticals) > 0 {
		m := make(map[string]struct{}, len(tacticals))
		for _, k := range tacticals {
			m[k] = struct{}{}
		}
		set.Store(m)
	}
	hub := NewEventHub(16)
	eventCh, unsub := hub.Subscribe()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r, err := NewRouter(RouterConfig{
		Store:       store,
		TxSink:      sink,
		IGateSender: igs,
		OurCall:     func() string { return ourCall },
		LocalTxRing: ring,
		TacticalSet: set,
		EventHub:    hub,
		Logger:      logger,
		Clock:       clock,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	r.Start(context.Background())
	cleanup := func() {
		r.Stop()
		unsub()
		_ = cs.Close()
	}
	return r, store, sink, igs, clock, eventCh, cleanup
}

// makeMessagePacket constructs a parsed DecodedAPRSPacket for a simple
// message ":AAAAAAAAA:text{id".
func makeMessagePacket(t *testing.T, source, addressee, text, msgID string, dir aprs.Direction) *aprs.DecodedAPRSPacket {
	t.Helper()
	// Build info field manually so the test exercises exactly the
	// parse path the router sees in production.
	pad := addressee + strings.Repeat(" ", 9-len(addressee))
	info := ":" + pad + ":" + text
	if msgID != "" {
		info += "{" + msgID
	}
	src, err := ax25.ParseAddress(source)
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	dst, err := ax25.ParseAddress("APGRWF")
	if err != nil {
		t.Fatalf("ParseAddress dest: %v", err)
	}
	f, err := ax25.NewUIFrame(src, dst, nil, []byte(info))
	if err != nil {
		t.Fatalf("NewUIFrame: %v", err)
	}
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("aprs.Parse: %v", err)
	}
	pkt.Direction = dir
	return pkt
}

// makeAckRejPacket constructs ":AAA:ack001" style reply.
func makeAckRejPacket(t *testing.T, source, addressee, prefix, msgID string) *aprs.DecodedAPRSPacket {
	t.Helper()
	pad := addressee + strings.Repeat(" ", 9-len(addressee))
	info := ":" + pad + ":" + prefix + msgID
	src, _ := ax25.ParseAddress(source)
	dst, _ := ax25.ParseAddress("APGRWF")
	f, _ := ax25.NewUIFrame(src, dst, nil, []byte(info))
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("aprs.Parse: %v", err)
	}
	pkt.Direction = aprs.DirectionRF
	return pkt
}

// makeReplyAckPacket constructs ":AAA:text{12}ackID".
func makeReplyAckPacket(t *testing.T, source, addressee, text, msgID, replyAckID string) *aprs.DecodedAPRSPacket {
	t.Helper()
	pad := addressee + strings.Repeat(" ", 9-len(addressee))
	info := ":" + pad + ":" + text + "{" + msgID + "}" + replyAckID
	src, _ := ax25.ParseAddress(source)
	dst, _ := ax25.ParseAddress("APGRWF")
	f, _ := ax25.NewUIFrame(src, dst, nil, []byte(info))
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("aprs.Parse: %v", err)
	}
	pkt.Direction = aprs.DirectionRF
	return pkt
}

// waitFor polls cond until true or the deadline expires.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for: %s", msg)
}

// drainEvents reads events until the channel is quiescent for a short
// window, returning all received events. Used to assert that exactly
// N events were published.
func drainEvents(ch <-chan Event, minCount int, timeout time.Duration) []Event {
	deadline := time.Now().Add(timeout)
	var out []Event
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return out
		}
		select {
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e)
			if len(out) >= minCount {
				// quick settle: allow a tiny follow-on window for
				// additional events that might be in flight.
				settle := 20 * time.Millisecond
				for {
					select {
					case ee, okk := <-ch:
						if !okk {
							return out
						}
						out = append(out, ee)
					case <-time.After(settle):
						return out
					}
				}
			}
		case <-time.After(remaining):
			return out
		}
	}
}

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

func TestRouterDMInboundForOurCallPersistsAndAutoACKs(t *testing.T) {
	r, store, sink, _, _, eventCh, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC-9", "N0CALL", "hello", "001", aprs.DirectionRF)
	if err := r.SendPacket(context.Background(), pkt); err != nil {
		t.Fatalf("SendPacket: %v", err)
	}

	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "row persisted")

	ms, _, _ := store.List(context.Background(), Filter{})
	if ms[0].FromCall != "W1ABC-9" {
		t.Fatalf("FromCall = %q", ms[0].FromCall)
	}
	if ms[0].ThreadKind != ThreadKindDM {
		t.Fatalf("ThreadKind = %q", ms[0].ThreadKind)
	}
	if !ms[0].Unread {
		t.Fatal("inbound should be unread")
	}

	submitted := sink.list()
	if len(submitted) != 1 {
		t.Fatalf("want 1 auto-ACK, got %d", len(submitted))
	}
	if !submitted[0].Src.SkipDedup {
		t.Fatal("auto-ACK must SkipDedup")
	}
	if submitted[0].Src.Priority != txgovernor.PriorityIGateMsg {
		t.Fatalf("unexpected priority: %d", submitted[0].Src.Priority)
	}
	// Info must be ":W1ABC-9  :ack001"
	info := submitted[0].Frame.Info
	want := ":W1ABC-9  :ack001"
	if string(info) != want {
		t.Fatalf("ack info = %q, want %q", string(info), want)
	}

	evts := drainEvents(eventCh, 1, 200*time.Millisecond)
	if len(evts) != 1 || evts[0].Type != EventMessageReceived {
		t.Fatalf("want 1 received event, got %v", evts)
	}
}

// ---------- Concrete tests using buildRouter directly. -----------------------

func TestRouterDMInboundForOurCallWithSSID(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL-5", nil)
	defer cleanup()

	// Addressee uses base call, not ours base matches.
	pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "hi", "007", aprs.DirectionRF)
	if err := r.SendPacket(context.Background(), pkt); err != nil {
		t.Fatalf("SendPacket: %v", err)
	}
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "row persisted")

	if got := len(sink.list()); got != 1 {
		t.Fatalf("auto-ACK count = %d", got)
	}
}

func TestRouterDMInboundNotForUsDropped(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC", "K1XYZ", "not for us", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("expected no persist, got %d rows", len(ms))
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("expected no auto-ACK, got %d", got)
	}
}

func TestRouterTacticalInboundPersistsNoAutoACK(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", []string{"NET"})
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC", "NET", "check-in", "100", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)

	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "row persisted")

	ms, _, _ := store.List(context.Background(), Filter{})
	if ms[0].ThreadKind != ThreadKindTactical {
		t.Fatalf("ThreadKind = %q", ms[0].ThreadKind)
	}
	if ms[0].ThreadKey != "NET" {
		t.Fatalf("ThreadKey = %q", ms[0].ThreadKey)
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("tactical must NOT auto-ACK, got %d", got)
	}
}

func TestRouterTacticalInboundLowercaseAddresseeNormalized(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "N0CALL", []string{"NET"})
	defer cleanup()

	// Lowercase addressee on the wire — must still match the uppercase
	// tactical set.
	pkt := makeMessagePacket(t, "W1ABC", "net", "hello", "100", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "persisted")
	ms, _, _ := store.List(context.Background(), Filter{})
	if ms[0].ThreadKey != "NET" {
		t.Fatalf("ThreadKey = %q, want NET", ms[0].ThreadKey)
	}
}

func TestRouterTacticalNotInSetDropped(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", []string{"NET"})
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC", "EOC", "not monitored", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("expected drop, got %d rows", len(ms))
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("no auto-ACK expected, got %d", got)
	}
}

func TestRouterDMAckCorrelationFlipsOutbound(t *testing.T) {
	r, store, _, _, _, eventCh, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	// Seed an outbound row we sent to W1ABC with msgid 042.
	ctx := context.Background()
	out := &configstore.Message{
		Direction:  "out",
		OurCall:    "N0CALL",
		FromCall:   "N0CALL",
		ToCall:     "W1ABC",
		Text:       "ping",
		MsgID:      "042",
		ThreadKind: ThreadKindDM,
		Source:     "rf",
	}
	if err := store.Insert(ctx, out); err != nil {
		t.Fatalf("Insert out: %v", err)
	}

	pkt := makeAckRejPacket(t, "W1ABC", "N0CALL", "ack", "042")
	_ = r.SendPacket(ctx, pkt)

	waitFor(t, time.Second, func() bool {
		got, _ := store.GetByID(ctx, out.ID)
		return got != nil && got.AckState == AckStateAcked
	}, "ack state flip")

	got, _ := store.GetByID(ctx, out.ID)
	if got.AckedAt == nil {
		t.Fatal("AckedAt must be set")
	}
	// Ack/rej rows do not persist — store should still have only one
	// row (the outbound).
	ms, _, _ := store.List(ctx, Filter{})
	if len(ms) != 1 {
		t.Fatalf("expected 1 row, got %d", len(ms))
	}
	// Event emitted.
	evts := drainEvents(eventCh, 1, 200*time.Millisecond)
	found := false
	for _, e := range evts {
		if e.Type == EventMessageAcked && e.MessageID == out.ID {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected message.acked event, got %v", evts)
	}
}

func TestRouterDMRejCorrelationFlipsOutbound(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()
	ctx := context.Background()

	out := &configstore.Message{
		Direction:  "out",
		OurCall:    "N0CALL",
		FromCall:   "N0CALL",
		ToCall:     "W1ABC",
		Text:       "nope",
		MsgID:      "055",
		ThreadKind: ThreadKindDM,
	}
	if err := store.Insert(ctx, out); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	pkt := makeAckRejPacket(t, "W1ABC", "N0CALL", "rej", "055")
	_ = r.SendPacket(ctx, pkt)
	waitFor(t, time.Second, func() bool {
		got, _ := store.GetByID(ctx, out.ID)
		return got != nil && got.AckState == AckStateRejected
	}, "rej state flip")
}

func TestRouterReplyAckCorrelationDMClosesAsAcked(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()
	ctx := context.Background()

	out := &configstore.Message{
		Direction:  "out",
		OurCall:    "N0CALL",
		FromCall:   "N0CALL",
		ToCall:     "W1ABC",
		Text:       "hi",
		MsgID:      "100",
		ThreadKind: ThreadKindDM,
	}
	if err := store.Insert(ctx, out); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// W1ABC sends us a new message with reply-ack trailer acking 100.
	pkt := makeReplyAckPacket(t, "W1ABC", "N0CALL", "yo", "12", "100")
	_ = r.SendPacket(ctx, pkt)
	waitFor(t, time.Second, func() bool {
		got, _ := store.GetByID(ctx, out.ID)
		return got != nil && got.AckState == AckStateAcked
	}, "DM reply-ack flip")
}

func TestRouterReplyAckTacticalSetsReceivedByCallOnly(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "N0CALL", []string{"NET"})
	defer cleanup()
	ctx := context.Background()

	// Seed tactical outbound in the "broadcast" terminal state (Phase
	// 3 would write that at send completion; we simulate).
	out := &configstore.Message{
		Direction:  "out",
		OurCall:    "N0CALL",
		FromCall:   "N0CALL",
		ToCall:     "NET",
		Text:       "net check",
		MsgID:      "200",
		ThreadKind: ThreadKindTactical,
		AckState:   AckStateBroadcast,
	}
	if err := store.Insert(ctx, out); err != nil {
		t.Fatalf("Insert: %v", err)
	}

	// An inbound tactical message from W1ABC addressed to NET, with
	// reply-ack trailer for msgid 200.
	pkt := makeReplyAckPacket(t, "W1ABC", "NET", "copy", "33", "200")
	_ = r.SendPacket(ctx, pkt)

	waitFor(t, time.Second, func() bool {
		got, _ := store.GetByID(ctx, out.ID)
		return got != nil && got.ReceivedByCall != ""
	}, "ReceivedByCall set")

	got, _ := store.GetByID(ctx, out.ID)
	if got.ReceivedByCall != "W1ABC" {
		t.Fatalf("ReceivedByCall = %q", got.ReceivedByCall)
	}
	if got.AckState != AckStateBroadcast {
		t.Fatalf("tactical AckState changed: %q", got.AckState)
	}
}

func TestRouterDedupWindowSuppressesInsertButStillACKs(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "hello", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "first insert")

	// Send a byte-for-byte duplicate — within the 30 s window.
	pkt2 := makeMessagePacket(t, "W1ABC", "N0CALL", "hello", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt2)

	waitFor(t, time.Second, func() bool {
		return len(sink.list()) >= 2
	}, "second auto-ACK")

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 1 {
		t.Fatalf("dedup must suppress second Insert, got %d rows", len(ms))
	}
	// APRS101 §14.2: both copies get acked.
	if got := len(sink.list()); got < 2 {
		t.Fatalf("want 2 auto-ACKs, got %d", got)
	}
}

func TestRouterSelfFilterByCallsign(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	// Our own packet heard via digipeater — must be dropped.
	pkt := makeMessagePacket(t, "N0CALL-7", "N0CALL", "loopback", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("self-filter failed: %d rows", len(ms))
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("self-filter auto-ACK leak: %d", got)
	}
}

func TestRouterSelfFilterByLocalTxRing(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	// Seed the ring as if we just transmitted (W1ABC, 001) — for some
	// obscure reason (third-party gating, test simulation). Router
	// must treat an inbound match as local loopback even though the
	// source callsign differs from ours.
	r.cfg.LocalTxRing.Add("W1ABC", "001")

	pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "hi", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("ring self-filter failed: %d rows", len(ms))
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("ring self-filter auto-ACK leak: %d", got)
	}
}

func TestRouterBulletinNeverACKed(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	// BLN* addressee — never our call, never tactical. Must be
	// dropped entirely, no ACK.
	pkt := makeMessagePacket(t, "W1ABC", "BLNALL", "stormy weather", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("bulletin must drop, got %d rows", len(ms))
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("bulletin must not auto-ACK, got %d", got)
	}
}

func TestRouterNWSNeverACKed(t *testing.T) {
	r, _, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()
	for _, ad := range []string{"NWS-1", "SKY100", "CWA001"} {
		pkt := makeMessagePacket(t, "W1ABC", ad, "warn", "001", aprs.DirectionRF)
		_ = r.SendPacket(context.Background(), pkt)
	}
	time.Sleep(50 * time.Millisecond)
	if got := len(sink.list()); got != 0 {
		t.Fatalf("NWS/SKY/CWA must not auto-ACK, got %d", got)
	}
}

func TestRouterAckOnSoftDeletedRowStillFlips(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()
	ctx := context.Background()

	out := &configstore.Message{
		Direction:  "out",
		OurCall:    "N0CALL",
		FromCall:   "N0CALL",
		ToCall:     "W1ABC",
		Text:       "tombstone me",
		MsgID:      "077",
		ThreadKind: ThreadKindDM,
	}
	if err := store.Insert(ctx, out); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	// Operator deletes.
	if err := store.SoftDelete(ctx, out.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	// Ack arrives late.
	pkt := makeAckRejPacket(t, "W1ABC", "N0CALL", "ack", "077")
	_ = r.SendPacket(ctx, pkt)

	waitFor(t, time.Second, func() bool {
		// Use .Unscoped() via FindOutstandingByMsgID (which already
		// includes deleted rows).
		rows, _ := store.FindOutstandingByMsgID(ctx, "077", "W1ABC")
		if len(rows) == 0 {
			return false
		}
		return rows[0].AckState == AckStateAcked && rows[0].DeletedAt.Valid
	}, "ack state set on tombstoned row")
}

func TestRouterTelemetryMetaExcluded(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	// PARM. message — parser sets TelemetryMeta, router must skip.
	pad := "N0CALL" + strings.Repeat(" ", 9-len("N0CALL"))
	info := ":" + pad + ":PARM.A1,A2,A3"
	src, _ := ax25.ParseAddress("W1ABC")
	dst, _ := ax25.ParseAddress("APGRWF")
	f, _ := ax25.NewUIFrame(src, dst, nil, []byte(info))
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkt.Direction = aprs.DirectionRF

	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)

	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("telemetry-meta must skip, got %d rows", len(ms))
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("telemetry-meta must not auto-ACK, got %d", got)
	}
}

func TestRouterCaseInsensitiveAddresseeMatchForOurCall(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "n0call", nil)
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "mixed case our", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "persisted with case-insensitive our_call")
}

func TestRouterISInboundMirrorsToIGate(t *testing.T) {
	r, _, sink, igs, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "from is", "001", aprs.DirectionIS)
	_ = r.SendPacket(context.Background(), pkt)
	waitFor(t, time.Second, func() bool {
		return len(sink.list()) == 1 && len(igs.list()) == 1
	}, "IS mirror")

	// Validate the TNC-2 line shape.
	line := igs.list()[0]
	if !strings.HasPrefix(line, "N0CALL>APGRWF::") {
		t.Fatalf("unexpected line: %q", line)
	}
	if !strings.HasSuffix(line, ":ack001") {
		t.Fatalf("unexpected line tail: %q", line)
	}
}

func TestRouterRFInboundDoesNotMirror(t *testing.T) {
	r, _, sink, igs, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "from rf", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	waitFor(t, time.Second, func() bool {
		return len(sink.list()) == 1
	}, "rf auto-ACK")
	if got := len(igs.list()); got != 0 {
		t.Fatalf("RF auto-ACK must not mirror, got %d lines", got)
	}
}

func TestRouterThirdPartyUnwrapReattributesSource(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	// Outer packet: W2XYZ -> N0CALL, but text begins with '}' and
	// wraps a real message from K1ABC to N0CALL. The router should
	// re-attribute the source to K1ABC.
	inner := ":N0CALL   :real msg{042"
	body := "}K1ABC>APGRWF,WIDE1-1:" + inner
	pad := "N0CALL" + strings.Repeat(" ", 9-len("N0CALL"))
	info := ":" + pad + ":" + body
	src, _ := ax25.ParseAddress("W2XYZ")
	dst, _ := ax25.ParseAddress("APGRWF")
	f, _ := ax25.NewUIFrame(src, dst, nil, []byte(info))
	pkt, err := aprs.Parse(f)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	pkt.Direction = aprs.DirectionRF

	_ = r.SendPacket(context.Background(), pkt)
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "unwrapped row persisted")

	ms, _, _ := store.List(context.Background(), Filter{})
	if ms[0].FromCall != "K1ABC" {
		t.Fatalf("FromCall = %q, want K1ABC", ms[0].FromCall)
	}
	if ms[0].MsgID != "042" {
		t.Fatalf("MsgID = %q", ms[0].MsgID)
	}

	// Auto-ACK goes to the inner source (K1ABC), not the envelope
	// (W2XYZ).
	sub := sink.list()
	if len(sub) != 1 {
		t.Fatalf("want 1 auto-ACK, got %d", len(sub))
	}
	wantAddr := "K1ABC    " // 9-char padded
	want := ":" + wantAddr + ":ack042"
	if string(sub[0].Frame.Info) != want {
		t.Fatalf("ack info = %q, want %q", string(sub[0].Frame.Info), want)
	}
}

func TestRouterQueueFullDropsOldest(t *testing.T) {
	// Build a router by hand with a small queue to exercise the drop
	// path deterministically.
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	store := NewStore(cs.DB())
	sink := &fakeTxSink{}
	ring := NewLocalTxRing(8, time.Minute)
	set := NewTacticalSet()
	hub := NewEventHub(16)
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	r, err := NewRouter(RouterConfig{
		Store:         store,
		TxSink:        sink,
		OurCall:       func() string { return "N0CALL" },
		LocalTxRing:   ring,
		TacticalSet:   set,
		EventHub:      hub,
		Logger:        logger,
		QueueCapacity: 2,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	// Mark running but do NOT Start — we want the queue to stay
	// full so drops are observable.
	r.running.Store(true)

	pkts := make([]*aprs.DecodedAPRSPacket, 0, 5)
	for i := 0; i < 5; i++ {
		pkts = append(pkts, makeMessagePacket(t, "W1ABC", "N0CALL",
			fmt.Sprintf("msg-%d", i), fmt.Sprintf("%03d", i), aprs.DirectionRF))
	}
	for _, p := range pkts {
		if err := r.SendPacket(context.Background(), p); err != nil {
			t.Fatalf("SendPacket returned err: %v", err)
		}
	}
	if got := len(r.queue); got > 2 {
		t.Fatalf("queue exceeded capacity: %d", got)
	}
}

func TestRouterEmptyMsgIDNoAutoACK(t *testing.T) {
	r, _, sink, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	// Message with no {id trailer → MsgID is empty. Per spec, we do
	// not auto-ACK when msgid is absent.
	pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "no id here", "", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)
	if got := len(sink.list()); got != 0 {
		t.Fatalf("no-msgid must not auto-ACK, got %d", got)
	}
}

func TestRouterOurCallEmptySkipsAutoACK(t *testing.T) {
	r, store, sink, _, _, _, cleanup := buildRouter(t, "", nil)
	defer cleanup()

	pkt := makeMessagePacket(t, "W1ABC", "FOO", "trying", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(50 * time.Millisecond)
	ms, _, _ := store.List(context.Background(), Filter{})
	// Nothing addressed to us (we have no call); tactical set empty;
	// drop. No ACK either.
	if len(ms) != 0 {
		t.Fatalf("expected drop when our_call empty, got %d", len(ms))
	}
	if got := len(sink.list()); got != 0 {
		t.Fatalf("expected no auto-ACK, got %d", got)
	}
}

func TestRouterTacticalSetReloadMidStream(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "N0CALL", []string{"NET"})
	defer cleanup()

	// First packet: addressed to EOC, not yet monitored.
	pkt := makeMessagePacket(t, "W1ABC", "EOC", "first", "001", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(30 * time.Millisecond)
	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("expected drop before reload, got %d rows", len(ms))
	}

	// Operator adds EOC to the monitored set.
	r.cfg.TacticalSet.Store(map[string]struct{}{"NET": {}, "EOC": {}})

	pkt2 := makeMessagePacket(t, "W1ABC", "EOC", "second", "002", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), pkt2)
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 1
	}, "post-reload persist")
}

func TestRouterSameSenderDMAndTacticalKeptSeparate(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "N0CALL", []string{"NET"})
	defer cleanup()

	// Same sender to our DM and to the tactical NET — must produce
	// two distinct threads.
	p1 := makeMessagePacket(t, "W1ABC", "N0CALL", "personal", "001", aprs.DirectionRF)
	p2 := makeMessagePacket(t, "W1ABC", "NET", "group", "002", aprs.DirectionRF)
	_ = r.SendPacket(context.Background(), p1)
	_ = r.SendPacket(context.Background(), p2)
	waitFor(t, time.Second, func() bool {
		ms, _, _ := store.List(context.Background(), Filter{})
		return len(ms) == 2
	}, "both persisted")

	ms, _, _ := store.List(context.Background(), Filter{})
	kinds := make(map[string]string)
	for _, m := range ms {
		kinds[m.ThreadKind] = m.ThreadKey
	}
	if kinds[ThreadKindDM] != "W1ABC" {
		t.Fatalf("DM key = %q", kinds[ThreadKindDM])
	}
	if kinds[ThreadKindTactical] != "NET" {
		t.Fatalf("tactical key = %q", kinds[ThreadKindTactical])
	}
}

func TestRouterAckWithEmptyMsgIDIgnored(t *testing.T) {
	r, store, _, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()

	// Construct an ack with no id on the wire (odd, but observed
	// in the wild). The parser produces IsAck=true and MessageID="".
	pkt := makeAckRejPacket(t, "W1ABC", "N0CALL", "ack", "")
	_ = r.SendPacket(context.Background(), pkt)
	time.Sleep(30 * time.Millisecond)

	// No correlation happens, no crash, no row.
	ms, _, _ := store.List(context.Background(), Filter{})
	if len(ms) != 0 {
		t.Fatalf("expected no row for empty-id ack, got %d", len(ms))
	}
}

func TestRouterSendPacketNeverErrors(t *testing.T) {
	r, _, _, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()
	for i := 0; i < 1000; i++ {
		pkt := makeMessagePacket(t, "W1ABC", "N0CALL", "flood", fmt.Sprintf("%03d", i), aprs.DirectionRF)
		if err := r.SendPacket(context.Background(), pkt); err != nil {
			t.Fatalf("SendPacket returned error: %v", err)
		}
	}
}

func TestRouterStopIsIdempotent(t *testing.T) {
	r, _, _, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()
	// Calling Stop twice (plus the cleanup's Stop) must not panic or
	// deadlock.
	r.Stop()
	r.Stop()
}

func TestRouterCloseReturnsNil(t *testing.T) {
	r, _, _, _, _, _, cleanup := buildRouter(t, "N0CALL", nil)
	defer cleanup()
	if err := r.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}
