package actions

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
)

// fakeServiceStore is a minimal in-memory implementation of
// serviceStore used by Service end-to-end tests. It is intentionally
// not concurrency-tested — Service tests serialize calls.
type fakeServiceStore struct {
	mu          sync.Mutex
	actions     map[string]*configstore.Action
	creds       map[uint]*configstore.OTPCredential
	listeners   []configstore.ActionListenerAddressee
	invocations []*configstore.ActionInvocation
}

func newFakeServiceStore() *fakeServiceStore {
	return &fakeServiceStore{
		actions: map[string]*configstore.Action{},
		creds:   map[uint]*configstore.OTPCredential{},
	}
}

func (f *fakeServiceStore) GetActionByName(_ context.Context, name string) (*configstore.Action, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.actions[name]
	if !ok {
		return nil, nil
	}
	return a, nil
}

func (f *fakeServiceStore) GetOTPCredential(_ context.Context, id uint) (*configstore.OTPCredential, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.creds[id]
	if !ok {
		return nil, nil
	}
	return c, nil
}

func (f *fakeServiceStore) ListActionListenerAddressees(_ context.Context) ([]configstore.ActionListenerAddressee, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]configstore.ActionListenerAddressee, len(f.listeners))
	copy(out, f.listeners)
	return out, nil
}

func (f *fakeServiceStore) PruneActionInvocations(_ context.Context, _ int, _ time.Duration) (int, error) {
	return 0, nil
}

func (f *fakeServiceStore) InsertActionInvocation(_ context.Context, row *configstore.ActionInvocation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	row.ID = uint(len(f.invocations) + 1)
	f.invocations = append(f.invocations, row)
	return nil
}

type capturingReplies struct {
	mu      sync.Mutex
	calls   []capturedReply
	gotCh   chan struct{}
	gotOnce sync.Once
}

type capturedReply struct {
	channel uint32
	source  Source
	to      string
	text    string
}

func newCapturingReplies() *capturingReplies {
	return &capturingReplies{gotCh: make(chan struct{}, 16)}
}

func (c *capturingReplies) SendReply(_ context.Context, channel uint32, source Source, to, text string) error {
	c.mu.Lock()
	c.calls = append(c.calls, capturedReply{channel: channel, source: source, to: to, text: text})
	c.mu.Unlock()
	select {
	case c.gotCh <- struct{}{}:
	default:
	}
	return nil
}

func (c *capturingReplies) waitForReply(t *testing.T) {
	t.Helper()
	select {
	case <-c.gotCh:
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for reply")
	}
}

func (c *capturingReplies) snapshot() []capturedReply {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]capturedReply, len(c.calls))
	copy(out, c.calls)
	return out
}

// fakeExecutor is a synchronous Executor stand-in that returns a
// scripted Result.
type fakeExecutor struct {
	res Result
}

func (f *fakeExecutor) Execute(_ context.Context, _ ExecRequest) Result { return f.res }

func TestServiceClassifyEndToEnd(t *testing.T) {
	store := newFakeServiceStore()
	store.actions["ping"] = &configstore.Action{
		ID: 1, Name: "ping", Type: "fake",
		Enabled: true, OTPRequired: false,
	}

	replies := newCapturingReplies()
	tac := messages.NewTacticalSet()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := newServiceForTest(ctx, store, replies, func() string { return "N0CALL" }, tac, nil)
	defer svc.Stop()

	if err := svc.Registry().Register("fake", &fakeExecutor{res: Result{Status: StatusOK, OutputCapture: "pong"}}); err != nil {
		t.Fatal(err)
	}

	pkt := &aprs.DecodedAPRSPacket{
		Source: "K1ABC", Type: aprs.PacketMessage, Direction: aprs.DirectionRF,
		Message: &aprs.Message{Addressee: "N0CALL", Text: "@@#ping"},
	}
	if !svc.Classifier().Classify(ctx, pkt) {
		t.Fatal("expected classifier to consume packet")
	}

	replies.waitForReply(t)
	got := replies.snapshot()
	if len(got) != 1 {
		t.Fatalf("expected 1 reply, got %+v", got)
	}
	if got[0].source != SourceRF || got[0].to != "K1ABC" {
		t.Fatalf("unexpected reply target/source: %+v", got[0])
	}
	if got[0].text != "ok: pong" {
		t.Fatalf("unexpected reply text: %q", got[0].text)
	}

	// The runner audits asynchronously after the executor returns. The
	// reply send is part of the same path, so once we observed the
	// reply, the audit insert is at most a few hundred microseconds
	// behind. Loop briefly to absorb that race.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		store.mu.Lock()
		n := len(store.invocations)
		store.mu.Unlock()
		if n >= 1 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.invocations) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(store.invocations))
	}
	row := store.invocations[0]
	if row.ActionNameAt != "ping" || row.SenderCall != "K1ABC" {
		t.Fatalf("unexpected audit row: %+v", row)
	}
	if row.Status != string(StatusOK) {
		t.Fatalf("expected status=ok, got %q", row.Status)
	}
}

func TestServiceReloadListenersUpdatesSnapshot(t *testing.T) {
	store := newFakeServiceStore()
	store.listeners = []configstore.ActionListenerAddressee{{Addressee: "GWACT"}}

	replies := newCapturingReplies()
	tac := messages.NewTacticalSet()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := newServiceForTest(ctx, store, replies, func() string { return "N0CALL" }, tac, nil)
	defer svc.Stop()
	if err := svc.ReloadListeners(ctx); err != nil {
		t.Fatal(err)
	}
	if !svc.listeners.Contains("GWACT") {
		t.Fatal("expected GWACT in listeners after reload")
	}

	// Mutate underlying table; snapshot must not change until we
	// reload.
	store.mu.Lock()
	store.listeners = []configstore.ActionListenerAddressee{{Addressee: "CABIN"}}
	store.mu.Unlock()
	if !svc.listeners.Contains("GWACT") {
		t.Fatal("snapshot must not auto-refresh")
	}
	if err := svc.ReloadListeners(ctx); err != nil {
		t.Fatal(err)
	}
	if svc.listeners.Contains("GWACT") {
		t.Fatal("GWACT should be gone after reload")
	}
	if !svc.listeners.Contains("CABIN") {
		t.Fatal("CABIN should be present after reload")
	}
}

func TestServiceTestFireRunsExecutorAndAudits(t *testing.T) {
	store := newFakeServiceStore()
	replies := newCapturingReplies()
	tac := messages.NewTacticalSet()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := newServiceForTest(ctx, store, replies, func() string { return "N0CALL" }, tac, nil)
	defer svc.Stop()

	if err := svc.Registry().Register("fake", &fakeExecutor{res: Result{Status: StatusOK, OutputCapture: "fired"}}); err != nil {
		t.Fatal(err)
	}

	a := &configstore.Action{
		ID: 7, Name: "switch", Type: "fake",
		Enabled: true, OTPRequired: true, // OTP required is bypassed by TestFire.
		TimeoutSec: 5,
	}
	res, id := svc.TestFire(ctx, a, []KeyValue{{Key: "state", Value: "on"}})
	if res.Status != StatusOK || res.OutputCapture != "fired" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if id == 0 {
		t.Fatalf("expected non-zero invocation id")
	}

	// No reply must have been dispatched.
	if got := replies.snapshot(); len(got) != 0 {
		t.Fatalf("test-fire must not send a reply, got %+v", got)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	if len(store.invocations) != 1 {
		t.Fatalf("expected 1 audit row, got %d", len(store.invocations))
	}
	row := store.invocations[0]
	if row.SenderCall != TestFireSenderCall {
		t.Fatalf("sender_call=%q, want %q", row.SenderCall, TestFireSenderCall)
	}
	if row.Status != string(StatusOK) || row.ActionNameAt != "switch" {
		t.Fatalf("unexpected audit row: %+v", row)
	}
}

func TestServiceStopIsIdempotent(t *testing.T) {
	store := newFakeServiceStore()
	replies := newCapturingReplies()
	tac := messages.NewTacticalSet()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	svc := newServiceForTest(ctx, store, replies, func() string { return "N0CALL" }, tac, nil)
	svc.Stop()
	svc.Stop() // must not panic
}
