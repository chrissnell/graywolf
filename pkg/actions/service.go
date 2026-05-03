package actions

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
)

// TestFireSenderCall is the synthetic SenderCall stamped on
// invocations originated from the REST test-fire endpoint. It bypasses
// any real callsign while still producing an audit row so operators
// can see the dry-run history.
const TestFireSenderCall = "(test-web)"

// Service is the composition root for the Actions subsystem. One
// instance per graywolf process. The wiring layer constructs it after
// the messages.Service is up and parks it on App for the rxfanout
// classifier hook to reach.
type Service struct {
	store        serviceStore
	classifier   *Classifier
	runner       *Runner
	verifier     *OTPVerifier
	listeners    *AddresseeSet
	registry     *ExecutorRegistry
	stopAudit    func()
	stopOTPSweep func()
	logger       *slog.Logger
}

// ServiceConfig wires the subsystem to the host process.
type ServiceConfig struct {
	// Store is the configstore.Store; required.
	Store *configstore.Store
	// Messages is the running messages.Service; required so replies
	// flow through the same outbound path operator-composed messages
	// take.
	Messages *messages.Service
	// OurCall returns the current primary callsign; used by the
	// classifier (addressee match) and by the reply adapter (From
	// address). Required.
	OurCall func() string
	// TacticalSet is the live tactical-alias set; the classifier
	// matches against it on every inbound packet. Required.
	TacticalSet *messages.TacticalSet
	// Logger is optional; nil falls back to slog.Default().
	Logger *slog.Logger
	// AuditPruner overrides the audit-log retention defaults. Zero
	// values fall back to package defaults.
	AuditPruner AuditPrunerConfig
}

// serviceStore narrows configstore.Store to the methods Service
// needs. Lets unit tests substitute an in-memory fake.
type serviceStore interface {
	ActionLookup
	CredentialLookup
	InvocationPruner
	InsertActionInvocation(ctx context.Context, row *configstore.ActionInvocation) error
	ListActionListenerAddressees(ctx context.Context) ([]configstore.ActionListenerAddressee, error)
}

// storeAuditSink adapts a serviceStore to the AuditSink interface.
// AuditSink expects Insert(ctx, *ActionInvocation); the configstore
// repo exposes InsertActionInvocation.
type storeAuditSink struct{ s serviceStore }

func (a storeAuditSink) Insert(ctx context.Context, row *configstore.ActionInvocation) error {
	return a.s.InsertActionInvocation(ctx, row)
}

// NewService builds the subsystem and starts background workers
// (audit pruner). The caller is responsible for invoking Stop on
// shutdown.
func NewService(ctx context.Context, cfg ServiceConfig) (*Service, error) {
	if cfg.Store == nil {
		return nil, errors.New("actions: ServiceConfig.Store is required")
	}
	if cfg.Messages == nil {
		return nil, errors.New("actions: ServiceConfig.Messages is required")
	}
	if cfg.OurCall == nil {
		return nil, errors.New("actions: ServiceConfig.OurCall is required")
	}
	if cfg.TacticalSet == nil {
		return nil, errors.New("actions: ServiceConfig.TacticalSet is required")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}

	registry := NewExecutorRegistry()
	if err := registry.Register("command", NewCommandExecutor()); err != nil {
		return nil, err
	}
	if err := registry.Register("webhook", NewWebhookExecutor()); err != nil {
		return nil, err
	}

	listeners := NewAddresseeSet()
	verifier := NewOTPVerifier(OTPVerifierConfig{})
	replies := NewMessagesReplySender(cfg.Messages, cfg.OurCall)
	runner := NewRunner(RunnerConfig{
		Registry: registry,
		Replies:  replies,
		Audit:    storeAuditSink{s: cfg.Store},
		Logger:   logger,
	})
	classifier := NewClassifier(ClassifierConfig{
		OurCall:     cfg.OurCall,
		TacticalSet: cfg.TacticalSet,
		Listeners:   listeners,
		ActionStore: cfg.Store,
		CredStore:   cfg.Store,
		OTPVerifier: verifier,
		Runner:      runner,
	})

	stop := StartAuditPruner(ctx, cfg.Store, cfg.AuditPruner)
	stopSweep := StartOTPSweeper(ctx, verifier, 0)

	s := &Service{
		store:        cfg.Store,
		classifier:   classifier,
		runner:       runner,
		verifier:     verifier,
		listeners:    listeners,
		registry:     registry,
		stopAudit:    stop,
		stopOTPSweep: stopSweep,
		logger:       logger,
	}
	if err := s.ReloadListeners(ctx); err != nil {
		return nil, err
	}
	return s, nil
}

// newServiceForTest wires a Service against a custom store and
// replyer, bypassing the production messages.Service constructor.
// Test-only.
func newServiceForTest(ctx context.Context, store serviceStore, replies ReplySender, ourCall func() string, tac *messages.TacticalSet, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	registry := NewExecutorRegistry()
	listeners := NewAddresseeSet()
	verifier := NewOTPVerifier(OTPVerifierConfig{})
	runner := NewRunner(RunnerConfig{
		Registry: registry,
		Replies:  replies,
		Audit:    storeAuditSink{s: store},
		Logger:   logger,
	})
	classifier := NewClassifier(ClassifierConfig{
		OurCall:     ourCall,
		TacticalSet: tac,
		Listeners:   listeners,
		ActionStore: store,
		CredStore:   store,
		OTPVerifier: verifier,
		Runner:      runner,
	})
	stop := StartAuditPruner(ctx, store, AuditPrunerConfig{})
	stopSweep := StartOTPSweeper(ctx, verifier, 0)
	return &Service{
		store:        store,
		classifier:   classifier,
		runner:       runner,
		verifier:     verifier,
		listeners:    listeners,
		registry:     registry,
		stopAudit:    stop,
		stopOTPSweep: stopSweep,
		logger:       logger,
	}
}

// Classifier returns the inbound classifier; the wiring layer hooks
// it into the rxfanout APRS-message branch.
func (s *Service) Classifier() *Classifier { return s.classifier }

// Registry returns the executor registry so test harnesses or future
// REST handlers can register custom executors.
func (s *Service) Registry() *ExecutorRegistry { return s.registry }

// ReloadListeners refreshes the in-memory listener-addressee snapshot
// from the store. Called on startup and whenever the REST handler
// mutates the table.
func (s *Service) ReloadListeners(ctx context.Context) error {
	rows, err := s.store.ListActionListenerAddressees(ctx)
	if err != nil {
		return err
	}
	names := make([]string, 0, len(rows))
	for _, r := range rows {
		names = append(names, r.Addressee)
	}
	s.listeners.Replace(names)
	return nil
}

// TestFire runs a one-shot invocation through the executor without
// going through the classifier or the per-Action queue. The OTP
// requirement and sender allowlist are intentionally bypassed — the
// caller is operator-authenticated via the REST cookie, so the
// dry-run is the point. The arg sanitizer still runs at the call
// site (the handler invokes SanitizeFromMap before calling us). No
// reply is dispatched to RF/IS; the result is returned synchronously
// for the HTTP response.
//
// An audit row is written using SenderCall=TestFireSenderCall and
// Source=SourceRF so operators can spot dry-runs separately from
// real-air invocations. The persisted invocation id is returned for
// the UI to deep-link into.
func (s *Service) TestFire(ctx context.Context, a *configstore.Action, kvs []KeyValue) (Result, uint) {
	res := s.runTestFireExecutor(ctx, a, kvs)
	id := s.writeTestFireAudit(ctx, a, kvs, res)
	return res, id
}

func (s *Service) runTestFireExecutor(ctx context.Context, a *configstore.Action, kvs []KeyValue) Result {
	exe, ok := s.registry.Lookup(a.Type)
	if !ok {
		return Result{Status: StatusError, StatusDetail: fmt.Sprintf("no executor for type %q", a.Type)}
	}
	timeout := time.Duration(a.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	inv := Invocation{
		ActionID: a.ID, ActionName: a.Name,
		SenderCall: TestFireSenderCall, Source: SourceRF,
		Args: kvs, StartedAt: time.Now(),
	}
	defer func() {
		if rec := recover(); rec != nil {
			s.logger.Error("actions: test-fire executor panic",
				"action", a.Name, "type", a.Type, "panic", rec)
		}
	}()
	return exe.Execute(ctx, ExecRequest{Action: a, Invocation: inv, Timeout: timeout})
}

func (s *Service) writeTestFireAudit(ctx context.Context, a *configstore.Action, kvs []KeyValue, res Result) uint {
	text, truncated := FormatReply(res)
	aid := a.ID
	row := &configstore.ActionInvocation{
		ActionID:      &aid,
		ActionNameAt:  a.Name,
		SenderCall:    TestFireSenderCall,
		Source:        string(SourceRF),
		OTPVerified:   false,
		RawArgsJSON:   marshalArgs(kvs),
		Status:        string(res.Status),
		StatusDetail:  res.StatusDetail,
		ExitCode:      res.ExitCode,
		HTTPStatus:    res.HTTPStatus,
		OutputCapture: res.OutputCapture,
		ReplyText:     text,
		Truncated:     truncated,
		CreatedAt:     time.Now().UTC(),
	}
	if err := s.store.InsertActionInvocation(ctx, row); err != nil {
		s.logger.Error("actions: test-fire audit insert failed",
			"action", a.Name, "err", err)
	}
	return row.ID
}

// Stop releases background resources. Safe to call multiple times.
func (s *Service) Stop() {
	if s.stopOTPSweep != nil {
		s.stopOTPSweep()
	}
	if s.stopAudit != nil {
		s.stopAudit()
	}
	if s.runner != nil {
		s.runner.Stop()
	}
}
