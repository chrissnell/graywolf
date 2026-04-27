package app

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// messagesWiringApp builds an App deep enough to run messagesComponent
// end-to-end: in-memory configstore, a real *txgovernor.Governor (so
// TxHook registration via TxHookRegistry works), a real
// *messages.Service, and a LocalTxRing. The modem bridge, beacon
// scheduler, digipeater, kiss manager, HTTP server, and agw server are
// all skipped — none of them is exercised by the messages pipeline.
//
// Caller owns the returned cancel() which tears down ctx-attached
// goroutines; the t.Cleanup hooks close the store and stop the
// messages component.
func messagesWiringApp(t *testing.T, ourCall string) (*App, context.Context, context.CancelFunc) {
	t.Helper()

	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	// Seed a StationConfig row so OurCall (resolved via
	// StationConfig per D8) returns ourCall. The iGate row carries
	// only transport/policy fields now — identity lives on
	// StationConfig. The Service uses its own injected OurCall
	// closure for this test, but seeding StationConfig matches the
	// real wiring so downstream (e.g. handler-level resolveOurCall)
	// sees the expected value.
	seedCtx := context.Background()
	if err := store.UpsertStationConfig(seedCtx, configstore.StationConfig{Callsign: ourCall}); err != nil {
		t.Fatalf("UpsertStationConfig: %v", err)
	}
	if err := store.UpsertIGateConfig(seedCtx, &configstore.IGateConfig{
		Enabled: false,
		Server:  "rotate.aprs2.net",
		Port:    14580,
	}); err != nil {
		t.Fatalf("UpsertIGateConfig: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// No-op Sender: messages Router auto-ACK submits land here; we
	// don't simulate the RF completion, so the TxHook never fires,
	// which keeps the test focused on the inbound classification path.
	noopSender := func(*pb.TransmitFrame) error { return nil }

	gov := txgovernor.New(txgovernor.Config{
		Sender:      noopSender,
		DcdEvents:   nil,
		DedupWindow: time.Second,
		Logger:      logger,
	})

	a := &App{
		cfg:            DefaultConfig(),
		logger:         logger,
		store:          store,
		metrics:        metrics.New(),
		gov:            gov,
		msgLocalRing:   messages.NewLocalTxRing(messages.DefaultLocalTxRingSize, messages.DefaultLocalTxRingTTL),
		messagesReload: make(chan struct{}, 1),
		plog:           packetlog.New(packetlog.Config{Capacity: 128}),
		stationCache:   stationcache.NewPersistentCache(logger),
	}
	a.msgStore = messages.NewStore(store.DB())

	// Construct messages.Service. Bridge nil → alwaysRF.
	// IGate nil → IS path returns error; sender with "-1" passcode
	// short-circuits IS immediately.
	svc, err := messages.NewService(messages.ServiceConfig{
		Store:         a.msgStore,
		ConfigStore:   store,
		TxSink:        a.gov,
		TxHookReg:     a.gov,
		IGate:         nil,
		Bridge:        nil,
		Logger:        logger.With("component", "messages"),
		IGatePasscode: "-1",
		OurCall:       func() string { return ourCall },
		LocalTxRing:   a.msgLocalRing,
	})
	if err != nil {
		t.Fatalf("messages.NewService: %v", err)
	}
	a.msgSvc = svc

	ctx, cancel := context.WithCancel(context.Background())

	// Governor Run: needed so Submit accepts frames without blocking
	// on a missing worker loop. The governor exits cleanly on ctx cancel.
	a.govWG.Add(1)
	go func() {
		defer a.govWG.Done()
		_ = a.gov.Run(ctx)
	}()

	// Start messagesComponent: registers TxHook, starts Router +
	// RetryManager, spins the reload drainer.
	comp := a.messagesComponent()
	if err := comp.start(ctx); err != nil {
		cancel()
		t.Fatalf("messagesComponent start: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		if err := comp.stop(shutdownCtx); err != nil {
			t.Errorf("messagesComponent stop: %v", err)
		}
	})

	return a, ctx, cancel
}

// TestMessagesWiring_StartStop verifies messagesComponent's start/stop
// lifecycle runs cleanly. The -race flag (enforced in CI) catches any
// data race on Service fields during start/stop interleaving.
func TestMessagesWiring_StartStop(t *testing.T) {
	_, _, cancel := messagesWiringApp(t, "N0CALL")
	defer cancel()

	// Brief settle so the Router consumer goroutine has observed
	// running=true before t.Cleanup fires comp.stop.
	time.Sleep(20 * time.Millisecond)
	// Cleanup hooks fire comp.stop and store.Close; no assertions
	// needed here — the lifecycle is the test. Failures surface as
	// goroutine leaks under -race or as comp.stop errors from the
	// t.Cleanup error path.
}

// TestMessagesWiring_RouterReceivesFromFanOut drives a decoded APRS
// message packet through the fan-out, with the Service's Router
// registered as an output alongside a recordingOutput. The router
// classifies the packet as an inbound DM for our callsign and persists
// a row in the message store.
//
// This is the integration contract the plan specifies: Router must be
// appended to the outputs slice used by runAPRSFanOut so inbound
// packets flow into the classifier alongside LogOutput / packet log /
// iGate output.
func TestMessagesWiring_RouterReceivesFromFanOut(t *testing.T) {
	a, ctx, cancel := messagesWiringApp(t, "N0CALL")
	defer cancel()

	// Mirror the bridgeComponent.start wiring: fan-out queue, one or
	// more outputs, runAPRSFanOut consuming until the queue closes.
	queue := make(chan *aprs.DecodedAPRSPacket, 4)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		runAPRSFanOut(ctx, queue, a.msgSvc.Router())
	}()

	pkt := makeInboundDM(t, "W1ABC-9", "N0CALL", "hello from test", "001")
	queue <- pkt

	// Router is async — poll the store for up to 1s.
	deadline := time.Now().Add(time.Second)
	var rows []configstore.Message
	for time.Now().Before(deadline) {
		rs, _, err := a.msgStore.List(ctx, messages.Filter{})
		if err != nil {
			t.Fatalf("Store.List: %v", err)
		}
		rows = rs
		if len(rows) >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(rows) != 1 {
		t.Fatalf("want 1 row persisted, got %d", len(rows))
	}
	if rows[0].FromCall != "W1ABC-9" {
		t.Errorf("FromCall = %q, want W1ABC-9", rows[0].FromCall)
	}
	if rows[0].ToCall != "N0CALL" {
		t.Errorf("ToCall = %q, want N0CALL", rows[0].ToCall)
	}
	if rows[0].Direction != "in" {
		t.Errorf("Direction = %q, want in", rows[0].Direction)
	}

	close(queue)
	wg.Wait()
}

// TestMessagesWiring_ReloadSignalRoundTrips verifies a non-blocking
// send on messagesReload wakes the drainer goroutine which in turn
// calls Service.ReloadTacticalCallsigns. The test inserts a tactical
// callsign directly (bypassing the REST handler) and asserts the
// Service's TacticalSet picks it up after the signal.
func TestMessagesWiring_ReloadSignalRoundTrips(t *testing.T) {
	a, ctx, cancel := messagesWiringApp(t, "N0CALL")
	defer cancel()

	if a.msgSvc.TacticalSet().Contains("NET") {
		t.Fatal("baseline: TacticalSet unexpectedly contains NET")
	}

	// Simulate a REST CRUD write of a tactical callsign.
	if err := a.store.CreateTacticalCallsign(ctx, &configstore.TacticalCallsign{
		Callsign: "NET",
		Alias:    "Main Ops Net",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateTacticalCallsign: %v", err)
	}

	// Send on the reload channel the same way the webapi handler does.
	select {
	case a.messagesReload <- struct{}{}:
	default:
		t.Fatal("messagesReload blocked on first send")
	}

	// Drainer runs Service.ReloadTacticalCallsigns asynchronously; give
	// it up to 1s to rebuild the set.
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if a.msgSvc.TacticalSet().Contains("NET") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("TacticalSet never picked up NET after reload signal")
}

// makeInboundDM builds a decoded APRS packet representing an inbound
// DM from source to addressee. Mirrors makeMessagePacket in
// pkg/messages/router_test.go — reproduced here because Go test
// helpers don't cross package boundaries.
func makeInboundDM(t *testing.T, source, addressee, text, msgID string) *aprs.DecodedAPRSPacket {
	t.Helper()
	pad := addressee + strings.Repeat(" ", 9-len(addressee))
	info := ":" + pad + ":" + text
	if msgID != "" {
		info += "{" + msgID
	}
	src, err := ax25.ParseAddress(source)
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	dst, err := ax25.ParseAddress("APGRWO")
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
	pkt.Direction = aprs.DirectionRF
	return pkt
}

// TestIGateIsRxHook_FeedsMessagesRouter verifies that the IsRxHook body
// (onIGateIsRxPacket) forwards APRS-IS-received message packets into
// the messages router, so inbound traffic addressed to our call (DM)
// and to an enabled tactical callsign is classified and persisted.
//
// Regression guard: before this wiring existed the hook only recorded
// to the packet log and station cache, and the router — which is only
// bound into the RF fan-out — never saw IS-sourced traffic. Tactical
// messages gated onto APRS-IS by a remote iGate (the common case for
// operators without local RF coverage of the sender) were silently
// dropped. See handleISLine in pkg/igate/igate.go for the full ingress
// path.
func TestIGateIsRxHook_FeedsMessagesRouter(t *testing.T) {
	const ourCall = "N0CALL"
	a, ctx, cancel := messagesWiringApp(t, ourCall)
	defer cancel()

	// Enable a tactical callsign and force the Service to reload its
	// TacticalSet so the router's classifier recognizes it.
	if err := a.store.CreateTacticalCallsign(ctx, &configstore.TacticalCallsign{
		Callsign: "GRAYWOLF",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateTacticalCallsign: %v", err)
	}
	if err := a.msgSvc.ReloadTacticalCallsigns(ctx); err != nil {
		t.Fatalf("ReloadTacticalCallsigns: %v", err)
	}

	// Build two IS-sourced packets: one addressed to our call (DM),
	// one to the tactical GRAYWOLF. The handleISLine path in igate
	// stamps Direction=DirectionIS; replicate that here so the router
	// sees the same provenance the production hook delivers.
	dm := makeInboundDM(t, "W1ABC-9", ourCall, "is-dm-hello", "042")
	dm.Direction = aprs.DirectionIS
	tac := makeInboundDM(t, "KF8EBB-1", "GRAYWOLF", "is-tactical-hello", "")
	tac.Direction = aprs.DirectionIS

	a.onIGateIsRxPacket(dm, "W1ABC-9>APGRWO,qAR,K1AAA::N0CALL   :is-dm-hello{042")
	a.onIGateIsRxPacket(tac, "KF8EBB-1>APGRWO,qAR,K8GI-5::GRAYWOLF :is-tactical-hello")

	// Router is async — poll the store for both rows.
	deadline := time.Now().Add(2 * time.Second)
	var rows []configstore.Message
	for time.Now().Before(deadline) {
		rs, _, err := a.msgStore.List(ctx, messages.Filter{})
		if err != nil {
			t.Fatalf("Store.List: %v", err)
		}
		rows = rs
		if len(rows) >= 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(rows) != 2 {
		t.Fatalf("want 2 rows persisted (DM + tactical), got %d", len(rows))
	}

	var gotDM, gotTactical bool
	for _, r := range rows {
		if r.Direction != "in" {
			t.Errorf("row %d: Direction = %q, want in", r.ID, r.Direction)
		}
		if r.Source != string(aprs.DirectionIS) {
			t.Errorf("row %d: Source = %q, want %q", r.ID, r.Source, aprs.DirectionIS)
		}
		switch r.ThreadKind {
		case messages.ThreadKindDM:
			gotDM = true
			if r.FromCall != "W1ABC-9" {
				t.Errorf("DM FromCall = %q, want W1ABC-9", r.FromCall)
			}
			if r.ThreadKey != "W1ABC-9" {
				t.Errorf("DM ThreadKey = %q, want W1ABC-9", r.ThreadKey)
			}
		case messages.ThreadKindTactical:
			gotTactical = true
			if r.FromCall != "KF8EBB-1" {
				t.Errorf("tactical FromCall = %q, want KF8EBB-1", r.FromCall)
			}
			if r.ThreadKey != "GRAYWOLF" {
				t.Errorf("tactical ThreadKey = %q, want GRAYWOLF", r.ThreadKey)
			}
		default:
			t.Errorf("row %d: unexpected ThreadKind %q", r.ID, r.ThreadKind)
		}
	}
	if !gotDM {
		t.Error("no DM row classified from IS-sourced hook fire")
	}
	if !gotTactical {
		t.Error("no tactical row classified from IS-sourced hook fire")
	}
}

// TestIGateIsRxHook_NoRouterSafe verifies onIGateIsRxPacket does not
// panic when a.msgSvc is nil — the production wireIGate closure ran
// before wireMessages for years, and defending against that ordering
// drift keeps the hook safe even if the service construction fails.
func TestIGateIsRxHook_NoRouterSafe(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	a := &App{
		logger:       logger,
		plog:         packetlog.New(packetlog.Config{Capacity: 16}),
		stationCache: stationcache.NewPersistentCache(logger),
		// msgSvc intentionally left nil.
	}
	pkt := makeInboundDM(t, "W1ABC-9", "N0CALL", "no-svc", "001")
	pkt.Direction = aprs.DirectionIS
	a.onIGateIsRxPacket(pkt, "W1ABC-9>APGRWO,qAR,K1AAA::N0CALL   :no-svc{001")
}
