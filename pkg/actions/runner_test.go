package actions

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

type fakeReplySink struct {
	mu      sync.Mutex
	replies []string
}

func (f *fakeReplySink) SendReply(_ context.Context, _ uint32, _ Source, _ string, text string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.replies = append(f.replies, text)
	return nil
}

func (f *fakeReplySink) snapshot() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, len(f.replies))
	copy(out, f.replies)
	return out
}

type fakeAudit struct {
	mu   sync.Mutex
	rows []configstore.ActionInvocation
}

func (f *fakeAudit) Insert(_ context.Context, row *configstore.ActionInvocation) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, *row)
	return nil
}

func (f *fakeAudit) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.rows)
}

type sleepyExecutor struct {
	dur time.Duration
}

func (s sleepyExecutor) Execute(ctx context.Context, _ ExecRequest) Result {
	select {
	case <-ctx.Done():
		return Result{Status: StatusTimeout}
	case <-time.After(s.dur):
		return Result{Status: StatusOK, OutputCapture: "done"}
	}
}

func TestRunnerHappyPath(t *testing.T) {
	reg := NewExecutorRegistry()
	_ = reg.Register("command", sleepyExecutor{dur: 5 * time.Millisecond})
	sink := &fakeReplySink{}
	audit := &fakeAudit{}
	r := NewRunner(RunnerConfig{Registry: reg, Replies: sink, Audit: audit})
	defer r.Stop()

	a := &configstore.Action{ID: 1, Name: "Echo", Type: "command", QueueDepth: 4, RateLimitSec: 0, Enabled: true}
	r.Submit(context.Background(), Invocation{ActionID: a.ID, ActionName: a.Name, SenderCall: "NW5W-7", Source: SourceRF}, a, 1)
	waitFor(t, func() bool { return len(sink.snapshot()) == 1 && audit.count() == 1 })
	if got := sink.snapshot()[0]; got != "ok: done" {
		t.Fatalf("reply: %q", got)
	}
}

func TestRunnerAuditStampsOTPCredentialID(t *testing.T) {
	reg := NewExecutorRegistry()
	_ = reg.Register("command", sleepyExecutor{dur: time.Millisecond})
	sink := &fakeReplySink{}
	audit := &fakeAudit{}
	r := NewRunner(RunnerConfig{Registry: reg, Replies: sink, Audit: audit})
	defer r.Stop()

	a := &configstore.Action{ID: 1, Name: "Echo", Type: "command", QueueDepth: 4, Enabled: true}
	r.Submit(context.Background(), Invocation{
		ActionID: a.ID, ActionName: a.Name,
		OTPVerified: true, OTPCredName: "chris", OTPCredentialID: 42,
	}, a, 1)
	waitFor(t, func() bool { return audit.count() == 1 })
	audit.mu.Lock()
	defer audit.mu.Unlock()
	row := audit.rows[0]
	if row.OTPCredentialID == nil || *row.OTPCredentialID != 42 {
		t.Fatalf("expected audit row OTPCredentialID=42, got %v", row.OTPCredentialID)
	}
	if !row.OTPVerified {
		t.Fatal("expected OTPVerified=true on audit row")
	}
}

type panickyExecutor struct{}

func (panickyExecutor) Execute(_ context.Context, _ ExecRequest) Result {
	panic("oops")
}

func TestRunnerExecutorPanicMappedToStatusError(t *testing.T) {
	reg := NewExecutorRegistry()
	_ = reg.Register("command", panickyExecutor{})
	sink := &fakeReplySink{}
	audit := &fakeAudit{}
	r := NewRunner(RunnerConfig{Registry: reg, Replies: sink, Audit: audit})
	defer r.Stop()

	a := &configstore.Action{ID: 1, Name: "Boom", Type: "command", QueueDepth: 4, Enabled: true}
	r.Submit(context.Background(), Invocation{ActionID: a.ID, ActionName: a.Name}, a, 1)
	waitFor(t, func() bool { return len(sink.snapshot()) == 1 && audit.count() == 1 })
	if got := sink.snapshot()[0]; got != "error: panic" {
		t.Fatalf("expected 'error: panic' reply, got %q", got)
	}
	// A second submit must still process — the worker goroutine
	// survived the panic via defer recover.
	r.Submit(context.Background(), Invocation{ActionID: a.ID, ActionName: a.Name}, a, 1)
	waitFor(t, func() bool { return len(sink.snapshot()) == 2 })
}

func TestRunnerAuditOmitsOTPCredentialIDWhenZero(t *testing.T) {
	reg := NewExecutorRegistry()
	_ = reg.Register("command", sleepyExecutor{dur: time.Millisecond})
	sink := &fakeReplySink{}
	audit := &fakeAudit{}
	r := NewRunner(RunnerConfig{Registry: reg, Replies: sink, Audit: audit})
	defer r.Stop()

	a := &configstore.Action{ID: 1, Name: "Echo", Type: "command", QueueDepth: 4, Enabled: true}
	r.Submit(context.Background(), Invocation{ActionID: a.ID, ActionName: a.Name}, a, 1)
	waitFor(t, func() bool { return audit.count() == 1 })
	audit.mu.Lock()
	defer audit.mu.Unlock()
	if audit.rows[0].OTPCredentialID != nil {
		t.Fatalf("expected nil OTPCredentialID for non-OTP action, got %v", audit.rows[0].OTPCredentialID)
	}
}

func TestRunnerRateLimit(t *testing.T) {
	reg := NewExecutorRegistry()
	_ = reg.Register("command", sleepyExecutor{dur: time.Millisecond})
	sink := &fakeReplySink{}
	audit := &fakeAudit{}
	r := NewRunner(RunnerConfig{Registry: reg, Replies: sink, Audit: audit})
	defer r.Stop()

	a := &configstore.Action{ID: 1, Name: "X", Type: "command", QueueDepth: 4, RateLimitSec: 60, Enabled: true}
	for i := 0; i < 2; i++ {
		r.Submit(context.Background(), Invocation{ActionID: a.ID, ActionName: a.Name}, a, 1)
	}
	waitFor(t, func() bool { return len(sink.snapshot()) == 2 })
	got := sink.snapshot()
	// Order is timing-dependent: the rate-limited reply is sent
	// synchronously inside Submit while the OK reply waits on the
	// worker, so either may land first. Just assert one of each.
	var ok, rl int
	for _, r := range got {
		switch r {
		case "ok: done":
			ok++
		case "rate-limited":
			rl++
		}
	}
	if ok != 1 || rl != 1 {
		t.Fatalf("want 1 ok + 1 rate-limited, got %v", got)
	}
}

func TestRunnerBusyOnQueueOverflow(t *testing.T) {
	reg := NewExecutorRegistry()
	_ = reg.Register("command", sleepyExecutor{dur: 100 * time.Millisecond})
	sink := &fakeReplySink{}
	audit := &fakeAudit{}
	r := NewRunner(RunnerConfig{Registry: reg, Replies: sink, Audit: audit})
	defer r.Stop()

	a := &configstore.Action{ID: 1, Name: "X", Type: "command", QueueDepth: 1, Enabled: true}
	for i := 0; i < 4; i++ {
		r.Submit(context.Background(), Invocation{ActionID: a.ID, ActionName: a.Name}, a, 1)
	}
	waitFor(t, func() bool { return len(sink.snapshot()) >= 3 })
	busyCount := 0
	for _, rpl := range sink.snapshot() {
		if rpl == "busy" {
			busyCount++
		}
	}
	if busyCount == 0 {
		t.Fatalf("expected at least one busy, got %v", sink.snapshot())
	}
}

func TestRunnerDisabled(t *testing.T) {
	reg := NewExecutorRegistry()
	_ = reg.Register("command", sleepyExecutor{dur: time.Millisecond})
	sink := &fakeReplySink{}
	audit := &fakeAudit{}
	r := NewRunner(RunnerConfig{Registry: reg, Replies: sink, Audit: audit})
	defer r.Stop()

	a := &configstore.Action{ID: 9, Name: "Off", Type: "command", QueueDepth: 4, Enabled: false}
	r.Submit(context.Background(), Invocation{ActionID: a.ID, ActionName: a.Name}, a, 1)
	waitFor(t, func() bool { return len(sink.snapshot()) == 1 })
	if got := sink.snapshot()[0]; got != "disabled" {
		t.Fatalf("reply: %q", got)
	}
}

func TestRunnerNoCredential(t *testing.T) {
	reg := NewExecutorRegistry()
	_ = reg.Register("command", sleepyExecutor{dur: time.Millisecond})
	sink := &fakeReplySink{}
	audit := &fakeAudit{}
	r := NewRunner(RunnerConfig{Registry: reg, Replies: sink, Audit: audit})
	defer r.Stop()

	a := &configstore.Action{ID: 9, Name: "Need", Type: "command", QueueDepth: 4, Enabled: true, OTPRequired: true}
	r.Submit(context.Background(), Invocation{ActionID: a.ID, ActionName: a.Name}, a, 1)
	waitFor(t, func() bool { return len(sink.snapshot()) == 1 })
	if got := sink.snapshot()[0]; got != "no-credential" {
		t.Fatalf("reply: %q", got)
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timeout waiting for condition")
}
