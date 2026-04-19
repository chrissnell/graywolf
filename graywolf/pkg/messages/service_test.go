package messages

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// fakeHookRegistry captures AddTxHook calls so tests can assert
// registration happened and drive the hook manually.
type fakeHookRegistry struct {
	mu     sync.Mutex
	hooks  []txgovernor.TxHook
	unregs []bool
}

func (f *fakeHookRegistry) AddTxHook(h txgovernor.TxHook) (uint64, func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	idx := len(f.hooks)
	f.hooks = append(f.hooks, h)
	f.unregs = append(f.unregs, false)
	return uint64(idx + 1), func() {
		f.mu.Lock()
		if idx < len(f.unregs) {
			f.unregs[idx] = true
		}
		f.mu.Unlock()
	}
}

func (f *fakeHookRegistry) numHooks() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.hooks)
}

func (f *fakeHookRegistry) fire(frame *ax25.Frame, src txgovernor.SubmitSource) {
	f.mu.Lock()
	hooks := make([]txgovernor.TxHook, len(f.hooks))
	copy(hooks, f.hooks)
	f.mu.Unlock()
	for _, h := range hooks {
		h(1, frame, src)
	}
}

func (f *fakeHookRegistry) wasUnregistered(idx int) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if idx < len(f.unregs) {
		return f.unregs[idx]
	}
	return false
}

// buildService constructs a Service with fakes around a real configstore.
func buildService(t *testing.T) (*Service, *senderRig, *fakeHookRegistry, func()) {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	store := NewStore(cs.DB())
	sink := &configurableTxSink{}
	igate := &fakeIGateSender{}
	bridge := &fakeBridge{running: true}
	clock := &fakeClock{now: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)}
	hookReg := &fakeHookRegistry{}

	svc, err := NewService(ServiceConfig{
		Store:         store,
		ConfigStore:   cs,
		TxSink:        sink,
		TxHookReg:     hookReg,
		IGate:         igate,
		Bridge:        bridge,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
		Clock:         clock,
		TxChannel:     1,
		IGatePasscode: "12345",
		OurCall:       func() string { return "N0CALL" },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}

	rig := &senderRig{
		sender: svc.Sender(),
		store:  store,
		cs:     cs,
		sink:   sink,
		igate:  igate,
		bridge: bridge,
		clock:  clock,
		ring:   svc.LocalTxRing(),
		prefs:  svc.Preferences(),
		hub:    svc.EventHub(),
	}
	rig.eventC, rig.unsub = svc.EventHub().Subscribe()

	cleanup := func() {
		rig.unsub()
		_ = cs.Close()
	}
	return svc, rig, hookReg, cleanup
}

func TestService_StartRegistersHookAndStopUnregisters(t *testing.T) {
	svc, _, hookReg, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if hookReg.numHooks() != 1 {
		t.Errorf("hooks registered = %d, want 1", hookReg.numHooks())
	}
	svc.Stop()
	if !hookReg.wasUnregistered(0) {
		t.Error("hook not unregistered on Stop")
	}
}

func TestService_StartIsIdempotent(t *testing.T) {
	svc, _, hookReg, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = svc.Start(ctx)
	_ = svc.Start(ctx)
	if hookReg.numHooks() != 1 {
		t.Errorf("hooks after double Start = %d, want 1", hookReg.numHooks())
	}
	svc.Stop()
	svc.Stop() // idempotent
}

func TestService_ReloadTacticalCallsigns_RebuildsSet(t *testing.T) {
	svc, rig, _, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	if err := rig.cs.CreateTacticalCallsign(ctx, &configstore.TacticalCallsign{
		Callsign: "NET",
		Alias:    "Main net",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateTacticalCallsign: %v", err)
	}
	if err := rig.cs.CreateTacticalCallsign(ctx, &configstore.TacticalCallsign{
		Callsign: "EOC",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateTacticalCallsign: %v", err)
	}
	if err := svc.ReloadTacticalCallsigns(ctx); err != nil {
		t.Fatalf("ReloadTacticalCallsigns: %v", err)
	}
	if !svc.TacticalSet().Contains("NET") {
		t.Error("NET not present after reload")
	}
	if !svc.TacticalSet().Contains("EOC") {
		t.Error("EOC not present after reload")
	}
	// Disable one and re-reload.
	row, err := rig.cs.GetTacticalCallsign(ctx, 1)
	if err != nil || row == nil {
		t.Fatalf("GetTacticalCallsign: %v row=%v", err, row)
	}
	row.Enabled = false
	if err := rig.cs.UpdateTacticalCallsign(ctx, row); err != nil {
		t.Fatalf("UpdateTacticalCallsign: %v", err)
	}
	if err := svc.ReloadTacticalCallsigns(ctx); err != nil {
		t.Fatalf("ReloadTacticalCallsigns: %v", err)
	}
	if svc.TacticalSet().Contains("NET") {
		t.Error("disabled NET still present after reload")
	}
}

func TestService_ReloadPreferencesKicksRetry(t *testing.T) {
	svc, rig, _, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	prefs, _ := rig.cs.GetMessagePreferences(ctx)
	prefs.RetryMaxAttempts = 10
	if err := rig.cs.UpsertMessagePreferences(ctx, prefs); err != nil {
		t.Fatalf("UpsertMessagePreferences: %v", err)
	}
	if err := svc.ReloadPreferences(ctx); err != nil {
		t.Fatalf("ReloadPreferences: %v", err)
	}
	if got := svc.Preferences().Current().RetryMaxAttempts; got != 10 {
		t.Errorf("RetryMaxAttempts = %d, want 10", got)
	}
}

func TestService_SendMessage_PersistsAndDispatches(t *testing.T) {
	svc, rig, _, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	// Default is is_fallback; bridge is up, so RF path is used.
	row, err := svc.SendMessage(ctx, SendMessageRequest{
		OurCall: "N0CALL",
		To:      "W1ABC",
		Text:    "hello",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if row.ID == 0 {
		t.Fatal("row.ID = 0 after insert")
	}
	if row.MsgID == "" {
		t.Fatal("DM row has no msg_id")
	}
	if row.ThreadKind != ThreadKindDM {
		t.Errorf("ThreadKind = %q, want dm", row.ThreadKind)
	}
	// Wait for async goroutine to submit.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(rig.sink.list()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(rig.sink.list()) == 0 {
		t.Fatal("no RF submit after SendMessage")
	}
}

func TestService_SendMessage_TacticalDerivedFromSet(t *testing.T) {
	svc, rig, _, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// Seed tactical set BEFORE Start so the reload path picks it up.
	if err := rig.cs.CreateTacticalCallsign(ctx, &configstore.TacticalCallsign{
		Callsign: "NET",
		Enabled:  true,
	}); err != nil {
		t.Fatalf("CreateTacticalCallsign: %v", err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	row, err := svc.SendMessage(ctx, SendMessageRequest{
		OurCall: "N0CALL",
		To:      "NET",
		Text:    "check in",
	})
	if err != nil {
		t.Fatalf("SendMessage tactical: %v", err)
	}
	if row.ThreadKind != ThreadKindTactical {
		t.Errorf("ThreadKind = %q, want tactical (derived from set)", row.ThreadKind)
	}
	if row.MsgID != "" {
		t.Errorf("tactical row has msg_id %q, want empty", row.MsgID)
	}
}

func TestService_SoftDelete_EmitsEvent(t *testing.T) {
	svc, rig, _, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	row, err := svc.SendMessage(ctx, SendMessageRequest{
		OurCall: "N0CALL",
		To:      "W1ABC",
		Text:    "delete me",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	// Drain any intermediate events.
	drainQuickly(rig.eventC, 100*time.Millisecond)
	if err := svc.SoftDelete(ctx, row.ID); err != nil {
		t.Fatalf("SoftDelete: %v", err)
	}
	// Expect a message.deleted event.
	got := drainQuickly(rig.eventC, 200*time.Millisecond)
	var seen bool
	for _, e := range got {
		if e.Type == EventMessageDeleted && e.MessageID == row.ID {
			seen = true
			break
		}
	}
	if !seen {
		t.Errorf("no message.deleted event; got %+v", got)
	}
}

func TestService_TxHookIntegration_FlipsSentAt(t *testing.T) {
	svc, rig, hookReg, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	row, err := svc.SendMessage(ctx, SendMessageRequest{
		OurCall: "N0CALL",
		To:      "W1ABC",
		Text:    "hook test",
	})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	// Wait for submit.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(rig.sink.list()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(rig.sink.list()) == 0 {
		t.Fatal("no submit yet")
	}
	// Fire the hook and verify SentAt flipped.
	frame := rig.sink.list()[0].Frame
	hookReg.fire(frame, txgovernor.SubmitSource{Kind: SubmitKindMessages})
	reloaded, _ := rig.store.GetByID(ctx, row.ID)
	if reloaded.SentAt == nil {
		t.Error("SentAt not flipped after hook fire via Service registration")
	}
}

func drainQuickly(ch <-chan Event, window time.Duration) []Event {
	var out []Event
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		select {
		case e, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, e)
		case <-time.After(10 * time.Millisecond):
		}
	}
	return out
}
