package app

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
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/digipeater"
	"github.com/chrissnell/graywolf/pkg/internal/testsync"
	"github.com/chrissnell/graywolf/pkg/internal/testtx"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// kissTncHarness wires a minimal App with just the pieces needed to
// exercise Phase 3's RX fanout end-to-end: kiss.Manager, digipeater,
// plog, stationCache, the shared rxFanout channel, and a capturing
// APRS submitter. Tests drive inputs by calling kissTncProduce
// (direct injection) or by connecting a TCP client to a running
// kiss.Server configured in the desired Mode.
type kissTncHarness struct {
	t             *testing.T
	ctx           context.Context
	cancel        context.CancelFunc
	app           *App
	sink          *testtx.Recorder // TX governor sink for Modem-mode KISS + digi emits
	aprsOut       chan *aprs.DecodedAPRSPacket
	aprsSubmitter *aprsSubmitter
	consumerWG    sync.WaitGroup
	digiEmits     *testtx.Recorder // the sink digi.Submit writes to
	// dispatched counts completed dispatchRxFrame calls. Tests that
	// assert on absence-of-effect (dedup suppression, no-further-emit)
	// block on this counter to prove the consumer has processed the
	// injected frame before checking the negative invariant.
	dispatched atomic.Int64
}

func newKissTncHarness(t *testing.T) *kissTncHarness {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	app := &App{
		logger:       quietLogger(),
		plog:         packetlog.New(packetlog.Config{Capacity: 256}),
		stationCache: stationcache.NewPersistentCache(quietLogger()),
	}
	app.rxFanout = make(chan rxFanoutItem, 16)
	app.aprsQueue = make(chan *aprs.DecodedAPRSPacket, 32)

	// TX sink used by both modem-mode KISS (real TxSink) and digipeater.
	sink := testtx.NewRecorder()
	digiEmits := sink // the same recorder observes both for this harness

	app.kissMgr = kiss.NewManager(kiss.ManagerConfig{
		Sink:      sink,
		Logger:    quietLogger(),
		RxIngress: app.kissTncProduce,
	})

	digi, err := digipeater.New(digipeater.Config{
		DedupeWindow: 30 * time.Second,
		Submit: func(ctx context.Context, channel uint32, frame *ax25.Frame, s txgovernor.SubmitSource) error {
			return digiEmits.Submit(ctx, channel, frame, s)
		},
		Logger: quietLogger(),
	})
	if err != nil {
		t.Fatalf("digipeater new: %v", err)
	}
	mycall, _ := ax25.ParseAddress("WOLF-1")
	digi.SetMyCall(mycall)
	digi.SetRules([]digipeater.Rule{
		{FromChannel: 1, ToChannel: 1, Alias: "WIDE", AliasType: "widen", MaxHops: 2, Action: "repeat"},
	})
	digi.SetEnabled(true)
	app.digi = digi

	aprsOut := make(chan *aprs.DecodedAPRSPacket, 16)
	// Use our own aprs channel so we don't race with the real aprsQueue
	// (which the consumer closes on exit). The submitter is a drop-counting
	// non-blocking send, which matches the real code path.
	submitter := newAPRSSubmitter(aprsOut, &fakeCounter{}, quietLogger())

	h := &kissTncHarness{
		t:             t,
		ctx:           ctx,
		cancel:        cancel,
		app:           app,
		sink:          sink,
		digiEmits:     digiEmits,
		aprsOut:       aprsOut,
		aprsSubmitter: submitter,
	}
	h.startConsumer()
	return h
}

func (h *kissTncHarness) startConsumer() {
	h.consumerWG.Add(1)
	go func() {
		defer h.consumerWG.Done()
		for {
			select {
			case <-h.ctx.Done():
				return
			case item := <-h.app.rxFanout:
				h.app.dispatchRxFrame(h.ctx, item, h.aprsSubmitter)
				h.dispatched.Add(1)
			}
		}
	}()
}

// waitDispatched blocks until the consumer has processed n frames.
func (h *kissTncHarness) waitDispatched(n int64, timeout time.Duration) {
	h.t.Helper()
	testsync.WaitFor(h.t, func() bool { return h.dispatched.Load() >= n },
		timeout, "consumer to dispatch frames")
}

func (h *kissTncHarness) stop() {
	h.cancel()
	h.consumerWG.Wait()
	if h.app.stationCache != nil {
		h.app.stationCache.Close()
	}
}

// startKissServer boots a kiss.Server via the Manager with the given
// interface ID, mode, and channel. Returns the ephemeral-port listen
// address once the listener is bound.
func (h *kissTncHarness) startKissServer(id uint32, mode kiss.Mode, channel uint32) string {
	h.t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		h.t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	h.app.kissMgr.Start(h.ctx, id, kiss.ServerConfig{
		Name:       "k" + time.Now().Format("150405.000"),
		ListenAddr: addr,
		Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		ChannelMap: map[uint8]uint32{0: channel},
		Broadcast:  true,
		Mode:       mode,
	})

	testsync.WaitFor(h.t, func() bool {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		_ = c.Close()
		return true
	}, 2*time.Second, "kiss server to bind at "+addr)
	return addr
}

// readKissFrame reads one KISS data frame from conn and returns the
// embedded AX.25 bytes, or "" on timeout.
func readKissFrame(t *testing.T, conn net.Conn, timeout time.Duration) []byte {
	t.Helper()
	_ = conn.SetReadDeadline(time.Now().Add(timeout))
	d := kiss.NewDecoder(conn)
	f, err := d.Next()
	if err != nil {
		return nil
	}
	return f.Data
}

// buildUIFrame builds a canonical UI frame for harness tests.
func buildUIFrame(t *testing.T, srcCall, info string, path []string) *ax25.Frame {
	t.Helper()
	src, _ := ax25.ParseAddress(srcCall)
	dst, _ := ax25.ParseAddress("APRS")
	var pathAddrs []ax25.Address
	for _, p := range path {
		a, err := ax25.ParseAddress(p)
		if err != nil {
			t.Fatalf("parse path %q: %v", p, err)
		}
		pathAddrs = append(pathAddrs, a)
	}
	f, err := ax25.NewUIFrame(src, dst, pathAddrs, []byte(info))
	if err != nil {
		t.Fatalf("build ui frame: %v", err)
	}
	return f
}

// TestKissTncRxFanout covers D1/D2: a TNC-mode interface's ingress
// reaches the digipeater, the APRS submit queue, and the station
// cache — identically to a modem-RX frame on the same channel.
func TestKissTncRxFanout(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()

	addr := h.startKissServer(10, kiss.ModeTnc, 1)

	// Frame with a WIDE2-2 path so the digipeater actually emits.
	f := buildUIFrame(t, "KD7ABC-1", "!4000.00N/10500.00W>", []string{"WIDE2-2"})
	axBytes, _ := f.Encode()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	if _, err := conn.Write(kiss.Encode(0, axBytes)); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Digipeater emits a rewritten copy via the shared sink.
	waitForCaptures(t, h.digiEmits, 1, 2*time.Second)

	// APRS decoded packet should have reached the submit queue.
	select {
	case pkt := <-h.aprsOut:
		if pkt == nil || pkt.Source != "KD7ABC-1" {
			t.Fatalf("aprs packet source=%q, want KD7ABC-1", pkt.Source)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("aprs packet not submitted")
	}

	// Station cache now has an entry for the source callsign. The
	// position payload above decodes to a valid latlon so the cache
	// must have stored it.
	got := h.app.stationCache.Lookup([]string{"KD7ABC-1"})
	if _, ok := got["KD7ABC-1"]; !ok {
		t.Fatalf("station cache has no entry for KD7ABC-1 after TNC ingest; got %v", got)
	}
}

// waitForCaptures blocks until rec has at least n captures. Fails the
// test on timeout.
func waitForCaptures(t *testing.T, rec *testtx.Recorder, n int, timeout time.Duration) {
	t.Helper()
	testsync.WaitFor(t, func() bool { return rec.Len() >= n }, timeout,
		"recorder to reach n captures")
}

// TestKissTncSelfLoopGuard verifies D3: a TNC-mode interface does NOT
// receive its own ingested frame via broadcast, while a co-channel
// Modem-mode interface DOES.
func TestKissTncSelfLoopGuard(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()

	const tncID = 100
	const modemID = 101
	tncAddr := h.startKissServer(tncID, kiss.ModeTnc, 1)
	modemAddr := h.startKissServer(modemID, kiss.ModeModem, 1)

	// Client A on the TNC interface (ingests + expects no echo).
	clientA, err := net.Dial("tcp", tncAddr)
	if err != nil {
		t.Fatalf("dial tnc: %v", err)
	}
	defer clientA.Close()
	// Client B on the Modem interface (expects the broadcast).
	clientB, err := net.Dial("tcp", modemAddr)
	if err != nil {
		t.Fatalf("dial modem: %v", err)
	}
	defer clientB.Close()

	// Block until each server has accepted its client. Without this the
	// frame can reach Broadcast before Client B is registered, and the
	// test would fail on "Modem-mode client did not receive broadcast"
	// for the wrong reason.
	testsync.WaitFor(t, func() bool {
		return h.app.kissMgr.ActiveClients(tncID) == 1 &&
			h.app.kissMgr.ActiveClients(modemID) == 1
	}, 2*time.Second, "both kiss clients to register")

	f := buildUIFrame(t, "N0LOOP-1", ">hello", nil)
	axBytes, _ := f.Encode()
	if _, err := clientA.Write(kiss.Encode(0, axBytes)); err != nil {
		t.Fatalf("write tnc: %v", err)
	}

	// Client B must receive the broadcast.
	gotB := readKissFrame(t, clientB, 2*time.Second)
	if len(gotB) == 0 {
		t.Fatal("Modem-mode client did not receive broadcast")
	}

	// Client A must NOT receive the broadcast. Short deadline — if the
	// skip is wrong the broadcast write is synchronous with the digi/
	// aprs dispatch, so 200ms is ample.
	gotA := readKissFrame(t, clientA, 200*time.Millisecond)
	if len(gotA) != 0 {
		t.Fatalf("TNC-mode client unexpectedly got self-echo: %x", gotA)
	}
}

// TestKissTncDigiMediatedLoopSuppressed exercises the loop scenario
// from D3: the typical home deployment where the hardware TNC and
// graywolf's radio share an RF channel, so a frame the TNC hears gets
// ingested, the digi emits it, the modem demodulates the emission,
// and the frame is re-ingested via the modem path. The existing digi
// dedup window (PathDedupKey, H-bit ignored) catches the exact-match
// replay and prevents unbounded amplification.
//
// The plan flags a stricter dedup target for modified-path replays as
// out of scope for Phase 3 — see the acceptance-test comment in the
// design doc. This test guards the exact-replay case that the current
// dedup handles; a strengthened dedup can extend it later without
// changing the harness.
func TestKissTncDigiMediatedLoopSuppressed(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()

	_ = h.startKissServer(200, kiss.ModeTnc, 1)

	// First ingest via the KISS-TNC producer directly (cheaper than TCP).
	f := buildUIFrame(t, "KD7ABC-1", ">loop-test", []string{"WIDE2-2"})
	axBytes, _ := f.Encode()
	h.app.kissTncProduce(&pb.ReceivedFrame{Channel: 1, Data: axBytes}, ingress.KissTnc(200))

	// First frame reaches the digi via the fanout consumer.
	h.waitDispatched(1, 2*time.Second)
	waitForCaptures(t, h.digiEmits, 1, 2*time.Second)

	// Re-ingest the original frame via the modem RX path, simulating
	// the TNC and the radio sharing a channel. PathDedupKey ignores the
	// H-bit, so the second ingest collides with the dedup window and
	// the digi must not emit again.
	select {
	case h.app.rxFanout <- rxFanoutItem{
		rf:  &pb.ReceivedFrame{Channel: 1, Data: axBytes},
		src: ingress.Modem(),
	}:
	case <-time.After(time.Second):
		t.Fatal("rxFanout send blocked")
	}

	// Block on the observable signal that the replay has been consumed
	// before checking the dedup invariant. dispatched reaches 2 only
	// after dispatchRxFrame returns, so any digi emission would have
	// already landed in digiEmits by then.
	h.waitDispatched(2, 2*time.Second)
	if got := h.digiEmits.Len(); got != 1 {
		t.Fatalf("digi emit count=%d after replay, want 1 (dedup should suppress)", got)
	}
}

// TestKissTncBackpressureDrops verifies D6: blasting a TNC interface
// at 10× its rate limit increments the rate-limiter's Dropped counter
// without stalling the consumer. Uses a fake clock so test timing is
// deterministic — the limiter receives no elapsed time between calls.
func TestKissTncBackpressureDrops(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()

	const ifaceID = 300
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	// Burst of 1, rate 10. Clock never advances → 9 consecutive drops.
	clk := &stoppedClock{now: time.Unix(0, 0)}
	h.app.kissMgr.Start(h.ctx, ifaceID, kiss.ServerConfig{
		Name:             "tnc-cap",
		ListenAddr:       addr,
		Logger:           slog.New(slog.NewTextHandler(io.Discard, nil)),
		ChannelMap:       map[uint8]uint32{0: 1},
		Mode:             kiss.ModeTnc,
		TncIngressRateHz: 10,
		TncIngressBurst:  1,
		Clock:            clk,
	})

	var conn net.Conn
	testsync.WaitFor(t, func() bool {
		c, err := net.Dial("tcp", addr)
		if err != nil {
			return false
		}
		conn = c
		return true
	}, 2*time.Second, "kiss tnc-cap server to bind")
	defer conn.Close()

	ax := buildUIFrame(t, "KD7ABC-1", ">burst", nil)
	axBytes, _ := ax.Encode()

	// 10 frames, serially, same clock tick.
	for i := range 10 {
		if _, err := conn.Write(kiss.Encode(0, axBytes)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	testsync.WaitFor(t, func() bool { return h.app.kissMgr.Dropped(ifaceID) >= 9 },
		2*time.Second, "9 frames to drop at the rate limiter")
	if got := h.app.kissMgr.Dropped(ifaceID); got != 9 {
		t.Fatalf("Dropped=%d, want 9 (1 burst + 9 denies)", got)
	}
	// Modem producer path is untouched by TNC drops: the shared
	// fanoutDropped counter should be zero (consumer kept up).
	if got := h.app.RxFanoutDropped(); got != 0 {
		t.Fatalf("RxFanoutDropped=%d, want 0", got)
	}
}

// stoppedClock is a never-advancing Clock for tests that want to
// freeze the rate limiter. We re-declare here (rather than importing
// kiss's fakeClock) because the kiss test helper is unexported.
type stoppedClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *stoppedClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// TestKissModemModeNotInFanout verifies that a Modem-mode KISS
// interface's inbound frames go to the TX governor and never enter
// the RX fanout — the D4 invariant.
func TestKissModemModeNotInFanout(t *testing.T) {
	h := newKissTncHarness(t)
	defer h.stop()

	addr := h.startKissServer(400, kiss.ModeModem, 1)

	ax := buildUIFrame(t, "KD7ABC-1", ">modem-mode", nil)
	axBytes, _ := ax.Encode()
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write(kiss.Encode(0, axBytes)); err != nil {
		t.Fatal(err)
	}

	// Sink receiving the frame is the positive signal that the server
	// took the Modem-mode switch arm. Once that arm fires, the TNC arm
	// (which would enqueue onto rxFanout) cannot run for the same
	// frame, so the negative assertions below need no additional wait.
	testsync.WaitFor(t, func() bool { return h.sink.Len() == 1 },
		2*time.Second, "modem-mode frame to reach TxGovernor sink")

	select {
	case pkt := <-h.aprsOut:
		t.Fatalf("unexpected aprs packet from modem-mode KISS: %+v", pkt)
	default:
	}
	if got := len(h.app.rxFanout); got != 0 {
		t.Fatalf("rxFanout len=%d after modem-mode ingest, want 0", got)
	}
	if got := h.app.RxFanoutDropped(); got != 0 {
		t.Fatalf("RxFanoutDropped=%d, want 0", got)
	}
}
