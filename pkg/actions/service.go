package actions

import (
	"context"
	"errors"
	"log/slog"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
)

// Service is the composition root for the Actions subsystem. One
// instance per graywolf process. The wiring layer constructs it after
// the messages.Service is up and parks it on App for the rxfanout
// classifier hook to reach.
type Service struct {
	store      serviceStore
	classifier *Classifier
	runner     *Runner
	verifier   *OTPVerifier
	listeners  *AddresseeSet
	registry   *ExecutorRegistry
	stopAudit  func()
	logger     *slog.Logger
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

	s := &Service{
		store:      cfg.Store,
		classifier: classifier,
		runner:     runner,
		verifier:   verifier,
		listeners:  listeners,
		registry:   registry,
		stopAudit:  stop,
		logger:     logger,
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
	return &Service{
		store:      store,
		classifier: classifier,
		runner:     runner,
		verifier:   verifier,
		listeners:  listeners,
		registry:   registry,
		stopAudit:  stop,
		logger:     logger,
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

// Stop releases background resources. Safe to call multiple times.
func (s *Service) Stop() {
	if s.stopAudit != nil {
		s.stopAudit()
	}
	if s.runner != nil {
		s.runner.Stop()
	}
}
