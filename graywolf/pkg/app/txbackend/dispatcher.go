package txbackend

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// Outcome enumerates per-instance Submit results recorded by the
// dispatcher. Values are stable — they appear as the `outcome` label
// on graywolf_tx_backend_submits_total.
const (
	OutcomeOK          = "ok"
	OutcomeErr         = "err"
	OutcomeBackendBusy = "backend_busy"
	OutcomeBackendDown = "backend_down"
)

// Metrics is the minimal interface the dispatcher uses to record
// per-submit telemetry. The concrete implementation in pkg/metrics
// satisfies it; tests use a fake that captures calls for assertion.
type Metrics interface {
	// ObserveTxBackendSubmit records one per-instance fan-out outcome.
	ObserveTxBackendSubmit(channel uint32, backend, instance, outcome string, d time.Duration)
	// ObserveTxNoBackend records a dispatcher drop because no backend
	// was registered for the frame's channel.
	ObserveTxNoBackend(channel uint32)
	// ObserveTxFrame records the single per-frame submission counter
	// (mirrors the pre-Phase-3 behaviour — one increment per governor
	// Submit regardless of fan-out size).
	ObserveTxFrame(channel uint32)
}

// nopMetrics is the default used when callers pass nil. Keeps Submit
// safe to call in unit tests that don't care about metrics.
type nopMetrics struct{}

func (nopMetrics) ObserveTxBackendSubmit(uint32, string, string, string, time.Duration) {}
func (nopMetrics) ObserveTxNoBackend(uint32)                                             {}
func (nopMetrics) ObserveTxFrame(uint32)                                                 {}

// Dispatcher routes every TX frame through the Registry snapshot to
// zero-or-more Backend instances, fanning out and joining per-instance
// errors. Single hot-path instance per process.
type Dispatcher struct {
	reg     *Registry
	metrics Metrics
	logger  *slog.Logger

	// closed flips to true on StopAccepting. Readers check it before
	// loading the snapshot so a late Send after shutdown returns
	// ErrStopped rather than blindly fanning out to backends whose
	// Close has already been called.
	closed atomic.Bool

	// watcher goroutine lifecycle.
	watcherWG   sync.WaitGroup
	watcherDone chan struct{}
}

// Config is the Dispatcher's constructor argument.
type Config struct {
	// Registry is the backing snapshot store. Required. Use NewRegistry.
	Registry *Registry
	// Metrics records per-submit telemetry. nil → no-op recorder.
	Metrics Metrics
	// Logger is optional; slog.Default is used when nil.
	Logger *slog.Logger
}

// New returns a Dispatcher. The watcher goroutine is not started
// automatically — callers wire it separately via StartWatcher so tests
// that don't need live config updates can skip it.
func New(cfg Config) *Dispatcher {
	lg := cfg.Logger
	if lg == nil {
		lg = slog.Default()
	}
	m := cfg.Metrics
	if m == nil {
		m = nopMetrics{}
	}
	reg := cfg.Registry
	if reg == nil {
		reg = NewRegistry()
	}
	return &Dispatcher{
		reg:         reg,
		metrics:     m,
		logger:      lg.With("component", "txbackend"),
		watcherDone: make(chan struct{}),
	}
}

// Registry returns the dispatcher's backing registry. Exposed for
// tests and the watcher wiring in pkg/app; Send callers never need it.
func (d *Dispatcher) Registry() *Registry { return d.reg }

// Send is the hot-path entry point the Governor calls once per frame.
// It resolves the frame's channel to a set of backends, fans out, and
// returns the joined error. Returns nil when at least one backend
// accepted the frame, ErrNoBackend when the channel has no backend,
// ErrStopped after StopAccepting, or errors.Join(perInstance...) when
// every backend failed.
//
// Send does NOT block on slow I/O: Backend implementations either
// hand off to an in-process IPC channel (modem) or a bounded queue
// (kiss). A momentarily full kiss queue returns ErrBackendBusy
// immediately; the dispatcher records the outcome and moves on. See
// the package godoc for the full fanout contract.
func (d *Dispatcher) Send(tf *pb.TransmitFrame) error {
	if tf == nil {
		return fmt.Errorf("txbackend: nil transmit frame")
	}
	if d.closed.Load() {
		return ErrStopped
	}

	snap := d.reg.Load()
	backends := snap.ByChannel[tf.Channel]
	if len(backends) == 0 {
		d.metrics.ObserveTxNoBackend(tf.Channel)
		d.logger.Warn("tx drop: no backend for channel",
			"channel", tf.Channel,
			"frame_id", tf.FrameId,
			"len", len(tf.Data))
		return ErrNoBackend
	}

	// Per-frame counter: one increment regardless of fan-out size.
	d.metrics.ObserveTxFrame(tf.Channel)

	d.logger.Debug("tx dispatch",
		"channel", tf.Channel,
		"frame_id", tf.FrameId,
		"backends", len(backends))

	ctx := context.Background()
	var (
		errs     []error
		accepted int
	)
	for _, b := range backends {
		start := time.Now()
		err := b.Submit(ctx, tf)
		dur := time.Since(start)
		outcome := classifyOutcome(err)
		d.metrics.ObserveTxBackendSubmit(tf.Channel, b.Name(), b.InstanceID(), outcome, dur)
		if err == nil {
			accepted++
			d.logger.Debug("tx backend accepted",
				"channel", tf.Channel,
				"frame_id", tf.FrameId,
				"backend", b.Name(),
				"instance", b.InstanceID(),
				"duration_ms", dur.Milliseconds())
			continue
		}
		errs = append(errs, fmt.Errorf("%s/%s: %w", b.Name(), b.InstanceID(), err))
		d.logger.Warn("tx backend rejected",
			"channel", tf.Channel,
			"frame_id", tf.FrameId,
			"backend", b.Name(),
			"instance", b.InstanceID(),
			"outcome", outcome,
			"err", err)
	}
	if accepted > 0 {
		return nil
	}
	// Defensive: backends slice non-empty but every backend returned
	// nil without incrementing accepted is not reachable today. Guard
	// anyway so a future backend that silently skips never maps a
	// submission failure to nil at the governor.
	if len(errs) == 0 {
		return ErrNoBackend
	}
	return errors.Join(errs...)
}

// SkipCSMA reports whether the given channel should bypass the
// governor's p-persistence / slot-time / DCD wait. True for KISS-only
// channels (no modem backend) since TCP links have no carrier to
// sense. The governor calls this once per processOne iteration
// immediately before the CSMA branch.
func (d *Dispatcher) SkipCSMA(channel uint32) bool {
	snap := d.reg.Load()
	return snap.CsmaSkip[channel]
}

// StopAccepting marks the dispatcher closed so subsequent Send calls
// return ErrStopped. Idempotent; safe to call from the shutdown
// orchestrator. Does NOT close the watcher goroutine — that exits on
// its own ctx cancellation in StartWatcher.
func (d *Dispatcher) StopAccepting() {
	d.closed.Store(true)
}

// StartWatcher launches the single rebuild goroutine that consumes
// config-change signals and republishes snapshots. Build must return
// a fresh snapshot on each call; the dispatcher does not cache. The
// goroutine exits when ctx is cancelled.
//
// signals is the fan-in channel the caller uses to trigger rebuilds.
// A send with no buffer capacity is dropped — every caller of
// NotifyReload does a non-blocking select, so the watcher just needs
// to coalesce bursts.
//
// An initial rebuild is performed synchronously before StartWatcher
// returns so the first Send after startup observes a populated
// snapshot rather than the empty constructor default.
func (d *Dispatcher) StartWatcher(ctx context.Context, signals <-chan struct{}, build func() *Snapshot) {
	// Initial publish so the first Submit after wiring sees real
	// backends rather than the empty constructor default.
	d.reg.Publish(build())
	d.watcherWG.Add(1)
	go func() {
		defer d.watcherWG.Done()
		defer close(d.watcherDone)
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-signals:
				if !ok {
					return
				}
				d.reg.Publish(build())
				d.logger.Debug("txbackend registry rebuilt")
			}
		}
	}()
}

// WaitWatcher blocks until the watcher goroutine has exited. Called by
// the shutdown orchestrator after ctx cancel so stop returns only once
// the rebuild goroutine is truly gone.
func (d *Dispatcher) WaitWatcher() {
	d.watcherWG.Wait()
}

// classifyOutcome maps a Backend.Submit error into the metric label.
// Unknown errors collapse to "err" so label cardinality stays bounded.
func classifyOutcome(err error) string {
	switch {
	case err == nil:
		return OutcomeOK
	case errors.Is(err, ErrBackendBusy):
		return OutcomeBackendBusy
	case errors.Is(err, ErrBackendDown):
		return OutcomeBackendDown
	default:
		return OutcomeErr
	}
}
