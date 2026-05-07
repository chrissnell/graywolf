package app

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/app/txbackend"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// recordingKissSender captures Enqueue calls so tests can assert the
// dispatcher actually fanned a frame into the KISS-TNC backend.
type recordingKissSender struct {
	mu      sync.Mutex
	frames  [][]byte
	frameCh chan struct{}
}

func newRecordingKissSender() *recordingKissSender {
	return &recordingKissSender{frameCh: make(chan struct{}, 16)}
}

func (r *recordingKissSender) Enqueue(frame []byte, _ uint64) error {
	r.mu.Lock()
	cp := make([]byte, len(frame))
	copy(cp, frame)
	r.frames = append(r.frames, cp)
	r.mu.Unlock()
	select {
	case r.frameCh <- struct{}{}:
	default:
	}
	return nil
}

func (r *recordingKissSender) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.frames)
}

// TestMessagesWiring_KissOnlyChannelTxFlow exercises the issue #81
// scenario end-to-end: a KISS-only channel (no audio device, modem
// bridge nil) with a registered KissTncBackend. A DM submitted via
// messages.Sender must traverse:
//
//	Sender.SendWithPolicy → rfAvailable() == true (via adapter)
//	→ governor.Submit → governor worker → dispatcher.Send
//	→ KissTncBackend.Submit → recordingKissSender.Enqueue
//	→ governor fires TxHook → sender.onTxComplete → row.SentAt set
//
// Before the fix, rfAvailable() returned false (bridge.IsRunning()
// only) and the row was permanently routed to the IS-fallback path,
// failing with "iGate not configured" or sitting at status=queued.
func TestMessagesWiring_KissOnlyChannelTxFlow(t *testing.T) {
	const ourCall = "N0CALL"
	const peerCall = "W1ABC-9"
	const channelID uint32 = 1

	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	seedCtx := context.Background()
	if err := store.UpsertStationConfig(seedCtx, configstore.StationConfig{Callsign: ourCall}); err != nil {
		t.Fatalf("UpsertStationConfig: %v", err)
	}

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Dispatcher + registry with a real KissTncBackend wired to a
	// recording sender. Snapshot is published once; we don't exercise
	// the watcher loop here.
	dispatcher := txbackend.New(txbackend.Config{
		Registry: txbackend.NewRegistry(),
		Logger:   logger,
	})
	kissSender := newRecordingKissSender()
	kissBackend := txbackend.NewKissTncBackend(kissSender, 99 /*interfaceID*/, channelID)
	dispatcher.Registry().Publish(&txbackend.Snapshot{
		ByChannel: map[uint32][]txbackend.Backend{
			channelID: {kissBackend},
		},
		CsmaSkip: map[uint32]bool{channelID: true},
	})

	// Governor with Sender = dispatcher.Send. CSMA-skip mirrors the
	// production wiring so KISS-only channels don't wait on a DCD
	// signal that will never arrive.
	gov := txgovernor.New(txgovernor.Config{
		Sender:      dispatcher.Send,
		DcdEvents:   nil,
		DedupWindow: time.Second,
		Logger:      logger,
	})
	gov.SetSkipCSMA(dispatcher.SkipCSMA)

	// rfAvailability adapter: bridge nil (no modem), registry from
	// dispatcher. Mirrors a.rfAvailability() in production wiring.
	rfAvail := rfAvailabilityAdapter{bridge: nil, reg: dispatcher.Registry()}
	if !rfAvail.IsRunningForChannel(channelID) {
		t.Fatal("baseline: adapter must report KISS-only channel available")
	}

	// App skeleton just deep enough for messagesComponent.start to
	// register the TxHook. Mirrors messagesWiringApp but injects our
	// adapter as Bridge so the sender's pre-check sees the KISS path.
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

	svc, err := messages.NewService(messages.ServiceConfig{
		Store:         a.msgStore,
		ConfigStore:   store,
		TxSink:        gov,
		TxHookReg:     gov,
		IGate:         nil,
		Bridge:        rfAvail,
		Logger:        logger.With("component", "messages"),
		TxChannel:     channelID,
		IGatePasscode: "-1",
		OurCall:       func() string { return ourCall },
		LocalTxRing:   a.msgLocalRing,
	})
	if err != nil {
		t.Fatalf("messages.NewService: %v", err)
	}
	a.msgSvc = svc

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a.govWG.Add(1)
	go func() {
		defer a.govWG.Done()
		_ = gov.Run(ctx)
	}()

	comp := a.messagesComponent()
	if err := comp.start(ctx); err != nil {
		t.Fatalf("messagesComponent start: %v", err)
	}
	t.Cleanup(func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer shutdownCancel()
		_ = comp.stop(shutdownCtx)
	})

	// Compose + send the DM. Use the Service so retry enrollment runs
	// the same way it does in production.
	row, err := svc.SendMessage(ctx, messages.SendMessageRequest{
		To:      peerCall,
		OurCall: ourCall,
		Text:    "kiss-only-tx-test",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Wait for the dispatcher to forward the frame to the KISS backend.
	select {
	case <-kissSender.frameCh:
	case <-time.After(2 * time.Second):
		t.Fatalf("KissTncBackend.Submit was not called within 2s; sent frames=%d", kissSender.count())
	}
	if got := kissSender.count(); got != 1 {
		t.Fatalf("KISS backend received %d frames, want 1", got)
	}

	// Wait for the TxHook to flip SentAt.
	deadline := time.Now().Add(2 * time.Second)
	var reloaded *configstore.Message
	for time.Now().Before(deadline) {
		reloaded, err = a.msgStore.GetByID(ctx, row.ID)
		if err == nil && reloaded != nil && reloaded.SentAt != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if reloaded == nil {
		t.Fatalf("row %d disappeared", row.ID)
	}
	if reloaded.SentAt == nil {
		t.Fatalf("SentAt never set; status would derive to %q",
			deriveStatusForTest(reloaded))
	}
	if got := reloaded.FailureReason; got != "" {
		t.Errorf("FailureReason = %q, want empty", got)
	}
}

// deriveStatusForTest mirrors the bits of dto.DeriveMessageStatus this
// test cares about, without dragging the dto import in.
func deriveStatusForTest(m *configstore.Message) string {
	switch {
	case m.SentAt != nil:
		return "sent_rf"
	case m.Attempts > 0:
		return "tx_submitted"
	default:
		return "queued"
	}
}

