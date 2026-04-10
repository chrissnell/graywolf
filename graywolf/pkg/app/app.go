package app

import (
	"context"
	"fmt"
	"log/slog"
)

// namedComponent is one entry in the App's ordered startup list. Each
// component provides a start and a stop closure; the App invokes start
// in forward order from Start() and stop in reverse order from Stop().
// Stop closures must be idempotent and must wait for their component's
// goroutines to actually exit — no fire-and-forget shutdown.
type namedComponent struct {
	name  string
	start func(ctx context.Context) error
	stop  func(ctx context.Context) error
}

// App is the graywolf composition root. It holds the resolved Config,
// a logger, and — once Start has run — a slice of live components in
// the order they were brought up. Stop tears them down in reverse.
//
// Construction is a two-phase process:
//  1. New(cfg, logger) builds an empty App.
//  2. Run(ctx) (or, in tests, directly populating startOrder and
//     calling Start/Stop) wires services and drives the lifecycle.
type App struct {
	cfg    Config
	logger *slog.Logger

	// startOrder is the full list of components wireServices produced.
	// It is populated before Start runs; tests may set it directly.
	startOrder []namedComponent

	// started is the prefix of startOrder that Start successfully
	// brought up. Stop iterates this slice in reverse so a partial
	// startup (e.g. the third of seven components failing) still only
	// tears down what actually came up.
	started []namedComponent
}

// New returns an App with the given config and logger. It does not
// open any resources; call Run (or Start) to bring the app online.
func New(cfg Config, logger *slog.Logger) *App {
	if logger == nil {
		logger = slog.Default()
	}
	return &App{cfg: cfg, logger: logger}
}

// Config returns the App's resolved Config. Exposed for tests and for
// the few places in wiring that need to read a value after construction.
func (a *App) Config() Config { return a.cfg }

// Run brings every wired component online, blocks until ctx is done,
// then tears everything back down with a derived shutdown context
// bounded by Config.ShutdownTimeout. Run returns the first non-nil
// error from startup or shutdown.
//
// The shutdown context is derived from context.Background() because
// ctx itself has already been cancelled by the time shutdown begins;
// deriving from ctx would yield an already-dead deadline. This is the
// second of three deliberate context.Background() exceptions in
// pkg/app (QueryModemVersion is the first; the third is the configstore
// open in wireServices which predates any context).
func (a *App) Run(ctx context.Context) error {
	if err := a.wireServices(ctx); err != nil {
		return fmt.Errorf("wire services: %w", err)
	}
	if err := a.Start(ctx); err != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
		defer cancel()
		_ = a.Stop(shutdownCtx)
		return err
	}

	<-ctx.Done()
	a.logger.Info("shutdown signal received", "cause", context.Cause(ctx))

	shutdownCtx, cancel := context.WithTimeout(context.Background(), a.cfg.ShutdownTimeout)
	defer cancel()
	return a.Stop(shutdownCtx)
}

// Start iterates startOrder and brings every component online, in
// order. The first start error short-circuits the loop — only the
// components that came up successfully are recorded into a.started,
// so a subsequent Stop tears down exactly that prefix.
func (a *App) Start(ctx context.Context) error {
	for _, c := range a.startOrder {
		a.logger.Info("starting component", "name", c.name)
		if err := c.start(ctx); err != nil {
			a.logger.Error("component start failed", "name", c.name, "err", err)
			return fmt.Errorf("start %s: %w", c.name, err)
		}
		a.started = append(a.started, c)
	}
	return nil
}
