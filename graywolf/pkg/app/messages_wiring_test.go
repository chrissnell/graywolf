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

	// Seed an iGate config row so OurCall resolves correctly. The
	// messages.Service reads Callsign lazily via the OurCall closure
	// passed to NewService; without this row the closure returns "".
	seedCtx := context.Background()
	if err := store.UpsertIGateConfig(seedCtx, &configstore.IGateConfig{
		Enabled:  false,
		Callsign: ourCall,
		Server:   "rotate.aprs2.net",
		Port:     14580,
		Passcode: "-1", // read-only; blocks IS-only fallback
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
	pkt.Direction = aprs.DirectionRF
	return pkt
}
