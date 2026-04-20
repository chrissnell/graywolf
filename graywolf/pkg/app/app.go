package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/chrissnell/graywolf/pkg/agw"
	"github.com/chrissnell/graywolf/pkg/app/ingress"
	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/beacon"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/digipeater"
	"github.com/chrissnell/graywolf/pkg/gps"
	"github.com/chrissnell/graywolf/pkg/igate"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/metrics"
	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/chrissnell/graywolf/pkg/webapi"
	"github.com/chrissnell/graywolf/pkg/webauth"
)

// rxFanoutItem is one inbound RX frame + its ingress source, produced
// by the modem bridge and any KISS-TNC interface, consumed by the
// single fanout goroutine that dispatches to digi / broadcast / APRS /
// station cache subscribers. Unexported: nothing outside pkg/app
// legitimately constructs one.
type rxFanoutItem struct {
	rf  *pb.ReceivedFrame
	src ingress.Source
}

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

	// --- Owned components (populated by wireServices) ------------------
	store        *configstore.Store
	authStore    *webauth.AuthStore
	metrics      *metrics.Metrics
	plog         *packetlog.Log
	stationCache *stationcache.PersistentCache
	bridge       *modembridge.Bridge
	gov          *txgovernor.Governor
	// govHookUnregister releases the packetlog/stationcache TX hook on
	// shutdown so later test runs in the same process don't accumulate
	// stale hooks against a stopped governor.
	govHookUnregister func()
	kissMgr           *kiss.Manager
	agwServer         *agw.Server // nil if AGW is disabled in config
	// agwMu guards access to agwServer so a reload can swap in a new
	// instance while the modem-bridge frame consumer is calling
	// BroadcastMonitoredUI on the old one. Readers use currentAgw();
	// the reload goroutine takes the write lock to stop the old server
	// and install (or clear) a replacement.
	agwMu       sync.Mutex
	digi        *digipeater.Digipeater
	gpsCache    *gps.MemCache
	stationPos  *gps.StationPos
	gpsMgr      *gpsManager
	beaconSched *beacon.Scheduler
	ig          *igate.Igate // nil if iGate is disabled in config
	apiSrv      *webapi.Server
	httpSrv     *http.Server

	// --- Messages service --------------------------------------------------
	// msgSvc owns the inbound Router + outbound Sender + RetryManager for
	// APRS text messaging. Its Router is registered with the APRS fan-out
	// in bridgeComponent so inbound UI-message packets reach the router's
	// classification path alongside the existing log/igate outputs.
	// msgLocalRing is the LocalOriginRing the iGate consults to suppress
	// re-gating of our own outbound messages heard back via digipeater;
	// it is shared by construction between the iGate (via Config.LocalOrigin)
	// and the messages.Service (via ServiceConfig.LocalTxRing).
	msgSvc       *messages.Service
	msgStore     *messages.Store
	msgLocalRing *messages.LocalTxRing

	// resolvedModem is the absolute path to the graywolf-modem binary
	// after running through ResolveModemPath. Retained so diagnostic
	// messages can name the exact binary being used.
	resolvedModem string

	// --- Reload channels (webapi signals, drained by reload goroutines) -
	gpsReload         chan struct{}
	beaconReload      chan struct{}
	smartBeaconReload chan struct{}
	digipeaterReload  chan struct{}
	igateReload       chan struct{}
	positionLogReload chan struct{}
	agwReload         chan struct{}
	messagesReload    chan struct{}

	// --- APRS fan-out plumbing ------------------------------------------
	aprsQueue       chan *aprs.DecodedAPRSPacket
	fanOutWG        sync.WaitGroup
	frameConsumerWG sync.WaitGroup

	// --- RX fanout (Phase 3: modem + KISS-TNC producers) ---------------
	// rxFanout carries every inbound RX frame (modem-demodulated OR
	// KISS-TNC ingested) alongside its ingress.Source so the single
	// consumer goroutine can dispatch to subscribers with loop-safe
	// source tagging. Modem-RX is a blocking producer (preserves
	// existing demod backpressure); KISS-TNC is a non-blocking producer
	// so a stuck fanout consumer drops off-air frames before it
	// stalls hardware-TNC socket reads. Sized to match the APRS
	// fan-out queue; the two have similar steady-state rates.
	rxFanout chan rxFanoutItem
	// rxFanoutDropped counts KISS-TNC producer drops at the shared
	// fanout channel (non-blocking send failed because the consumer
	// was behind). Modem-RX never increments this: its send is
	// blocking by design.
	rxFanoutDropped atomic.Uint64
	// rxFanoutWG tracks the single fanout consumer goroutine. Separate
	// from frameConsumerWG (which tracks the modem producer) so stop
	// can sequence "close producers → consumer drains → aprsQueue closes"
	// in the right order.
	rxFanoutWG sync.WaitGroup

	// --- Per-component goroutine tracking ------------------------------
	// Each component that spawns its own goroutine(s) gets an isolated
	// WaitGroup so the stop closure can wait on exactly the work it
	// owns without tangling with siblings. Having one catchall WG would
	// force every stop to wait for every other component to exit,
	// defeating the ordered-teardown contract.
	govWG               sync.WaitGroup
	statsWG             sync.WaitGroup
	kissWG              sync.WaitGroup
	agwWG               sync.WaitGroup
	agwReloadWG         sync.WaitGroup
	digiReloadWG        sync.WaitGroup
	igateReloadWG       sync.WaitGroup
	gpsWG               sync.WaitGroup
	beaconWG            sync.WaitGroup
	beaconReloadWG      sync.WaitGroup
	positionLogReloadWG sync.WaitGroup
	httpWG              sync.WaitGroup
	messagesReloadWG    sync.WaitGroup

	// --- Lifecycle ------------------------------------------------------
	// startOrder is the full list of components wireServices produced.
	// It is populated before Start runs; tests may set it directly.
	startOrder []namedComponent

	// started is the prefix of startOrder that Start successfully
	// brought up. Stop iterates this slice in reverse so a partial
	// startup (e.g. the third of seven components failing) still only
	// tears down what actually came up.
	started []namedComponent

	// beaconReloadDone is an optional test-only hook. When non-nil, the
	// beacon reload goroutine performs a non-blocking send onto it
	// after every successful reload pass so tests can wait for a
	// specific reload to land without polling. Unset in production.
	beaconReloadDone chan struct{}
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

// RxFanoutDropped returns the number of KISS-TNC inbound frames dropped
// at the shared RX fanout channel because the single consumer was
// behind (non-blocking send failed). Modem-RX never increments this —
// its producer is a blocking send. Phase 5 wires this into a
// Prometheus counter.
func (a *App) RxFanoutDropped() uint64 { return a.rxFanoutDropped.Load() }

// KissTncDropped returns the cumulative rate-limiter drop count for
// the KISS interface with the given DB ID. Zero if the interface is
// not running or the limiter is in unlimited mode. Phase 5 uses this
// for the per-interface rate-limit counter.
func (a *App) KissTncDropped(ifaceID uint32) uint64 {
	if a.kissMgr == nil {
		return 0
	}
	return a.kissMgr.Dropped(ifaceID)
}

// KissTncQueueOverflow returns the cumulative per-interface ingress
// queue overflow count for the KISS interface with the given DB ID.
// Zero if the interface is not running. Phase 5 uses this for the
// per-interface queue-overflow counter.
func (a *App) KissTncQueueOverflow(ifaceID uint32) uint64 {
	if a.kissMgr == nil {
		return 0
	}
	return a.kissMgr.QueueOverflow(ifaceID)
}

// Run brings every wired component online, blocks until ctx is done,
// then tears everything back down with a derived shutdown context
// bounded by Config.ShutdownTimeout. Run returns the first non-nil
// error from startup or shutdown.
//
// The shutdown context is derived from context.Background() because
// ctx itself has already been cancelled by the time shutdown begins;
// deriving from ctx would yield an already-dead deadline. This is
// one of only two deliberate context.Background() uses in pkg/app;
// QueryModemVersion in modem.go is the other, and for the same kind
// of reason (it runs before the App context exists).
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
