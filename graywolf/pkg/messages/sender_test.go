package messages

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

// ---------------------------------------------------------------------------
// Sender harness
// ---------------------------------------------------------------------------

// fakeBridge controls the RF-availability signal for the sender.
type fakeBridge struct {
	running bool
	mu      sync.Mutex
}

func (b *fakeBridge) IsRunning() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.running
}
func (b *fakeBridge) setRunning(v bool) {
	b.mu.Lock()
	b.running = v
	b.mu.Unlock()
}

// configurableTxSink adds an err-setter to the existing fakeTxSink so
// tests can toggle the return code between calls. A per-call error
// override is also supported via setErrOnce.
type configurableTxSink struct {
	mu        sync.Mutex
	submitted []fakeSubmit
	err       error
	errOnce   error
}

func (f *configurableTxSink) Submit(_ context.Context, ch uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.errOnce != nil {
		e := f.errOnce
		f.errOnce = nil
		return e
	}
	if f.err != nil {
		return f.err
	}
	f.submitted = append(f.submitted, fakeSubmit{Channel: ch, Frame: frame, Src: src})
	return nil
}

func (f *configurableTxSink) list() []fakeSubmit {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeSubmit, len(f.submitted))
	copy(out, f.submitted)
	return out
}
func (f *configurableTxSink) setErr(e error) {
	f.mu.Lock()
	f.err = e
	f.mu.Unlock()
}
func (f *configurableTxSink) setErrOnce(e error) {
	f.mu.Lock()
	f.errOnce = e
	f.mu.Unlock()
}

// senderRig bundles the pieces tests construct repeatedly.
type senderRig struct {
	sender *Sender
	store  *Store
	cs     *configstore.Store
	sink   *configurableTxSink
	igate  *fakeIGateSender
	bridge *fakeBridge
	clock  *fakeClock
	ring   *LocalTxRing
	prefs  *Preferences
	hub    *EventHub
	eventC <-chan Event
	unsub  func()
}

func (r *senderRig) close() {
	r.unsub()
	_ = r.cs.Close()
}

// buildSender constructs a Sender with fakes. policy is the fallback
// policy to seed; "" uses the configstore default.
func buildSender(t *testing.T, policy string, rfRunning bool) *senderRig {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	if policy != "" {
		prefs, err := cs.GetMessagePreferences(context.Background())
		if err != nil {
			t.Fatalf("GetMessagePreferences: %v", err)
		}
		if prefs == nil {
			prefs = &configstore.MessagePreferences{}
		}
		prefs.FallbackPolicy = policy
		if err := cs.UpsertMessagePreferences(context.Background(), prefs); err != nil {
			t.Fatalf("UpsertMessagePreferences: %v", err)
		}
	}
	store := NewStore(cs.DB())
	sink := &configurableTxSink{}
	igate := &fakeIGateSender{}
	bridge := &fakeBridge{running: rfRunning}
	clock := &fakeClock{now: time.Date(2026, 4, 18, 12, 0, 0, 0, time.UTC)}
	ring := NewLocalTxRing(16, time.Minute)
	hub := NewEventHub(16)
	prefs := NewPreferences(cs)
	if _, err := prefs.Load(context.Background()); err != nil {
		t.Fatalf("prefs Load: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	sender, err := NewSender(SenderConfig{
		Store:       store,
		TxSink:      sink,
		IGateSender: igate,
		Bridge:      bridge,
		LocalTxRing: ring,
		Preferences: prefs,
		EventHub:    hub,
		Logger:      logger,
		Clock:       clock,
		TxChannel:   1,
		IGatePasscode: "12345",
	})
	if err != nil {
		t.Fatalf("NewSender: %v", err)
	}
	ch, unsub := hub.Subscribe()
	return &senderRig{
		sender: sender,
		store:  store,
		cs:     cs,
		sink:   sink,
		igate:  igate,
		bridge: bridge,
		clock:  clock,
		ring:   ring,
		prefs:  prefs,
		hub:    hub,
		eventC: ch,
		unsub:  unsub,
	}
}

// newOutboundDM inserts a fresh DM outbound row and returns a handle
// to it.
func newOutboundDM(t *testing.T, rig *senderRig, ourCall, toCall, text string) *configstore.Message {
	t.Helper()
	id, err := rig.store.AllocateMsgID(context.Background(), toCall)
	if err != nil {
		t.Fatalf("AllocateMsgID: %v", err)
	}
	row := &configstore.Message{
		Direction:  "out",
		OurCall:    ourCall,
		FromCall:   ourCall,
		ToCall:     toCall,
		Text:       text,
		MsgID:      id,
		ThreadKind: ThreadKindDM,
		AckState:   AckStateNone,
		CreatedAt:  rig.clock.Now(),
	}
	if err := rig.store.Insert(context.Background(), row); err != nil {
		t.Fatalf("Insert: %v", err)
	}
	return row
}

// newOutboundTactical inserts a fresh tactical outbound row.
func newOutboundTactical(t *testing.T, rig *senderRig, ourCall, label, text string) *configstore.Message {
	t.Helper()
	row := &configstore.Message{
		Direction:  "out",
		OurCall:    ourCall,
		FromCall:   ourCall,
		ToCall:     label,
		Text:       text,
		ThreadKind: ThreadKindTactical,
		AckState:   AckStateNone,
		CreatedAt:  rig.clock.Now(),
	}
	if err := rig.store.Insert(context.Background(), row); err != nil {
		t.Fatalf("Insert tactical: %v", err)
	}
	return row
}

// ---------------------------------------------------------------------------
// Tests — happy paths + fallback policies
// ---------------------------------------------------------------------------

func TestSender_RF_HappyPath_SentAtOnlyFlipsOnHookFire(t *testing.T) {
	rig := buildSender(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hello")

	res := rig.sender.Send(context.Background(), row)
	if res.Err != nil {
		t.Fatalf("Send returned error: %v", res.Err)
	}
	if res.Path != SendPathRF {
		t.Fatalf("Path = %q, want rf", res.Path)
	}
	if !res.Retryable {
		t.Error("DM Retryable should be true")
	}
	// Governor received the frame.
	if got := rig.sink.list(); len(got) != 1 {
		t.Fatalf("submitted frames = %d, want 1", len(got))
	}
	// SkipDedup must be set so APRS-101 retransmission (identical
	// frames under the 30s dedup window) actually reaches the wire.
	if !rig.sink.list()[0].Src.SkipDedup {
		t.Error("SubmitSource.SkipDedup = false, want true for messages TX")
	}
	if rig.sink.list()[0].Src.Kind != SubmitKindMessages {
		t.Errorf("SubmitSource.Kind = %q, want %q", rig.sink.list()[0].Src.Kind, SubmitKindMessages)
	}
	// SentAt MUST still be nil — TxHook hasn't fired yet.
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if reloaded.SentAt != nil {
		t.Error("SentAt flipped before TxHook fire")
	}
	// LocalTxRing recorded the outbound.
	if !rig.ring.Contains("N0CALL", row.MsgID) {
		t.Error("LocalTxRing did not record (source, msgid)")
	}

	// Simulate the TxHook firing. The sender's onTxComplete should
	// flip SentAt.
	frame := rig.sink.list()[0].Frame
	rig.sender.onTxComplete(1, frame, txgovernor.SubmitSource{Kind: SubmitKindMessages})
	reloaded, _ = rig.store.GetByID(context.Background(), row.ID)
	if reloaded.SentAt == nil {
		t.Error("SentAt not flipped after TxHook fire")
	}
	if reloaded.AckState != AckStateNone {
		t.Errorf("DM AckState after hook = %q, want none", reloaded.AckState)
	}
	// Expect a message.sent_rf event.
	select {
	case e := <-rig.eventC:
		if e.Type != EventMessageSentRF {
			t.Errorf("event type = %q, want %q", e.Type, EventMessageSentRF)
		}
		if e.MessageID != row.ID {
			t.Errorf("event id = %d, want %d", e.MessageID, row.ID)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("no sent_rf event observed")
	}
}

func TestSender_RF_QueueFull_RetryableShortRetry(t *testing.T) {
	rig := buildSender(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")
	rig.sink.setErrOnce(txgovernor.ErrQueueFull)

	res := rig.sender.Send(context.Background(), row)
	if !errors.Is(res.Err, txgovernor.ErrQueueFull) {
		t.Fatalf("Err = %v, want ErrQueueFull", res.Err)
	}
	if !res.Retryable {
		t.Error("queue-full must be retryable")
	}
	// No frame should be recorded as submitted.
	if got := rig.sink.list(); len(got) != 0 {
		t.Errorf("submitted = %d, want 0", len(got))
	}
	// FailureReason populated.
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if !strings.Contains(reloaded.FailureReason, "queue full") {
		t.Errorf("FailureReason = %q", reloaded.FailureReason)
	}
}

func TestSender_RF_GovernorStopped_TerminalOnRFOnly(t *testing.T) {
	rig := buildSender(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")
	rig.sink.setErr(txgovernor.ErrStopped)

	res := rig.sender.Send(context.Background(), row)
	if !errors.Is(res.Err, txgovernor.ErrStopped) {
		t.Fatalf("Err = %v, want ErrStopped", res.Err)
	}
	if res.Retryable {
		t.Error("ErrStopped on rf_only must not be retryable")
	}
}

func TestSender_RFUnavailable_ISFallback(t *testing.T) {
	rig := buildSender(t, FallbackPolicyISFallback, false /* bridge not running */)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")

	res := rig.sender.Send(context.Background(), row)
	if res.Err != nil {
		t.Fatalf("Send err: %v", res.Err)
	}
	if res.Path != SendPathIS {
		t.Errorf("Path = %q, want is", res.Path)
	}
	// IS line dispatched.
	if len(rig.igate.list()) != 1 {
		t.Errorf("IS lines = %d, want 1", len(rig.igate.list()))
	}
	// SentAt flipped inline on IS.
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if reloaded.SentAt == nil {
		t.Error("SentAt not flipped on IS send")
	}
}

func TestSender_ISOnly_SkipsRF(t *testing.T) {
	rig := buildSender(t, FallbackPolicyISOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")

	res := rig.sender.Send(context.Background(), row)
	if res.Err != nil {
		t.Fatalf("Send: %v", res.Err)
	}
	if res.Path != SendPathIS {
		t.Errorf("Path = %q, want is", res.Path)
	}
	if got := rig.sink.list(); len(got) != 0 {
		t.Errorf("RF submits = %d, want 0 (is_only)", len(got))
	}
	if len(rig.igate.list()) != 1 {
		t.Errorf("IS lines = %d, want 1", len(rig.igate.list()))
	}
}

func TestSender_Both_FansOut(t *testing.T) {
	rig := buildSender(t, FallbackPolicyBoth, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")

	res := rig.sender.Send(context.Background(), row)
	if res.Err != nil {
		t.Fatalf("Send: %v", res.Err)
	}
	if res.Path != SendPathBoth {
		t.Errorf("Path = %q, want both", res.Path)
	}
	if len(rig.sink.list()) != 1 {
		t.Errorf("RF submits = %d, want 1", len(rig.sink.list()))
	}
	if len(rig.igate.list()) != 1 {
		t.Errorf("IS lines = %d, want 1", len(rig.igate.list()))
	}
}

func TestSender_IGateDisconnected_ISFails(t *testing.T) {
	rig := buildSender(t, FallbackPolicyISOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")
	rig.igate.err = errors.New("igate: not connected")

	res := rig.sender.Send(context.Background(), row)
	if res.Err == nil {
		t.Fatal("expected error on disconnected igate")
	}
	if res.Retryable {
		t.Error("IS disconnect should NOT be retryable (no server-side retry budget)")
	}
}

func TestSender_TacticalOutbound_FlipsBroadcastOnHook(t *testing.T) {
	rig := buildSender(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundTactical(t, rig, "N0CALL", "NET", "net is up")

	res := rig.sender.Send(context.Background(), row)
	if res.Err != nil {
		t.Fatalf("Send: %v", res.Err)
	}
	// Tactical should NOT be retryable (no DM enrollment).
	if res.Retryable {
		t.Error("Tactical outbound should not be retryable")
	}
	frame := rig.sink.list()[0].Frame
	rig.sender.onTxComplete(1, frame, txgovernor.SubmitSource{Kind: SubmitKindMessages})
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if reloaded.AckState != AckStateBroadcast {
		t.Errorf("AckState = %q, want broadcast", reloaded.AckState)
	}
	if reloaded.SentAt == nil {
		t.Error("SentAt not flipped")
	}
}

func TestSender_TxHook_IgnoresOtherKinds(t *testing.T) {
	rig := buildSender(t, FallbackPolicyRFOnly, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")

	_ = rig.sender.Send(context.Background(), row)
	frame := rig.sink.list()[0].Frame

	// Hook fires for a non-messages origin — sender should ignore.
	rig.sender.onTxComplete(1, frame, txgovernor.SubmitSource{Kind: "digipeater"})
	reloaded, _ := rig.store.GetByID(context.Background(), row.ID)
	if reloaded.SentAt != nil {
		t.Error("SentAt flipped for non-messages hook")
	}
}

func TestSender_ReadOnlyPasscode_ISFails(t *testing.T) {
	rig := buildSender(t, FallbackPolicyISOnly, true)
	defer rig.close()
	// Override the sender to a read-only passcode.
	rig.sender.cfg.IGatePasscode = "-1"
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")

	res := rig.sender.Send(context.Background(), row)
	if res.Err == nil {
		t.Fatal("expected error on read-only passcode")
	}
	if len(rig.igate.list()) != 0 {
		t.Errorf("IS lines = %d, want 0 on read-only passcode", len(rig.igate.list()))
	}
}

func TestSender_ISFallback_RFFailsDefinitively(t *testing.T) {
	rig := buildSender(t, FallbackPolicyISFallback, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")
	rig.sink.setErr(txgovernor.ErrStopped)

	res := rig.sender.Send(context.Background(), row)
	if res.Err != nil {
		t.Fatalf("Send err: %v (expected IS fallback to succeed)", res.Err)
	}
	if res.Path != SendPathIS {
		t.Errorf("Path = %q, want is (after RF fallback)", res.Path)
	}
	if len(rig.igate.list()) != 1 {
		t.Errorf("IS lines = %d, want 1", len(rig.igate.list()))
	}
}

func TestSender_ISFallback_QueueFullStaysOnRF(t *testing.T) {
	rig := buildSender(t, FallbackPolicyISFallback, true)
	defer rig.close()
	row := newOutboundDM(t, rig, "N0CALL", "W1ABC", "hi")
	rig.sink.setErrOnce(txgovernor.ErrQueueFull)

	res := rig.sender.Send(context.Background(), row)
	// ErrQueueFull should not trigger IS fallback — it's a transient
	// back-pressure signal, not a definitive RF failure.
	if !errors.Is(res.Err, txgovernor.ErrQueueFull) {
		t.Fatalf("Err = %v, want ErrQueueFull", res.Err)
	}
	if len(rig.igate.list()) != 0 {
		t.Errorf("IS lines = %d, want 0 (queue-full should not fallback)", len(rig.igate.list()))
	}
}
