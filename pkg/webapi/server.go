// Package webapi is graywolf's REST management API.
//
// @title       Graywolf Management API
// @version     1.0
// @description REST API for graywolf configuration and control.
// @BasePath    /api
// @schemes     http
//
// @securityDefinitions.apikey  CookieAuth
// @in                          header
// @name                        Cookie
// @description                 Session cookie issued by POST /api/auth/login.
// @description                 Swagger 2.0 lacks a native `in: cookie` apiKey
// @description                 location, so the spec models the same credential
// @description                 as a `Cookie:` request header. Browsers send this
// @description                 automatically once the session cookie is set; the
// @description                 session cookie is named `graywolf_session`.
//
// Tag-group ordering is applied post-generation by
// pkg/webapi/docs/cmd/tagify — swag v1.16.x silently drops package-
// level `@tag.name`/`@tag.description` directives, and swag v2 (RC5)
// still drops them plus mangles POST/PUT bodies in OpenAPI 3.1 mode,
// so we stay on v1.16.x and inject the `tags:` array ourselves.
package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25conn"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/mapsauth"
	"github.com/chrissnell/graywolf/pkg/mapscache"
	"github.com/chrissnell/graywolf/pkg/mapscatalog"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/packetlog"
	"github.com/chrissnell/graywolf/pkg/updatescheck"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// Server routes /api/* requests. It does not own the underlying
// listener; cmd/graywolf composes it into its main mux.
type Server struct {
	store             *configstore.Store
	bridge            *modembridge.Bridge
	kissManager       *kiss.Manager
	kissCtx           context.Context // long-lived context for KISS server goroutines
	logger            *slog.Logger
	startedAt         time.Time
	historyDBPath     string // read-only; set by -history-db flag
	version           string // build-time version string returned by GET /api/version
	igateStatusFn     func() igate.Status
	gpsReload         chan struct{} // signalled when GPS config changes
	beaconReload      chan struct{} // signalled when beacon config changes
	digipeaterReload  chan struct{} // signalled when digipeater config/rules change
	igateReload       chan struct{} // signalled when igate config/filters change
	positionLogReload chan struct{} // signalled when position log config changes
	agwReload         chan struct{} // signalled when AGW config changes
	smartBeaconReload chan struct{} // signalled when smart-beacon singleton config changes
	messagesReload    chan struct{} // signalled when messages preferences or tactical callsigns change
	updatesReloadCh   chan struct{} // signalled when the updates-check toggle flips so the checker re-evaluates immediately
	updatesChecker    *updatescheck.Checker
	mapsAuth          *mapsauth.Client    // client for auth.nw5w.com /register; defaulted in NewServer
	mapsCache         *mapscache.Manager  // PMTiles cache; nil until P2-T5 wires it up — handlers return 503 when nil
	catalog           *mapscatalog.Cache  // download catalog cache; nil until wired — handlers return 503 when nil
	// txBackendReload is the Phase 3 dispatcher's rebuild signal.
	// Nudged after any change that could alter the channel-backing
	// map (kiss interface add/remove/mode/allow_tx flip, channel
	// add/remove, audio device add/remove). Buffered size 1 +
	// non-blocking send coalesces bursts.
	txBackendReload chan struct{}
	beaconSendNow   func(ctx context.Context, id uint32) error // triggers an immediate beacon send

	// messages-service is late-bound: it exists only after the Phase 5
	// app wiring has constructed the configstore + txgovernor + igate,
	// so the webapi Server handlers must guard against a nil value and
	// return 503 until SetMessagesService is called. See
	// RegisterMessages / registerMessages.
	messagesService MessagesService
	messagesStore   MessagesStore // optional: defaults to messagesService.Store() via adapter
	messagesBotDir  messages.BotDirectory

	// ax25Mgr is wired by pkg/app at startup via SetAX25Manager. The
	// /api/ax25/terminal WebSocket handler returns 503 until the
	// manager is installed so ordering bugs in wiring fail loud.
	ax25Mgr *ax25conn.Manager

	// packetLog is the live packet ring shared with /api/packets and
	// the AX.25 terminal raw-tail mode. Wired post-construction via
	// SetPacketLog so cmd/graywolf can build it once and share it.
	packetLog *packetlog.Log

	// actions is the Actions subsystem. Wired post-construction via
	// SetActionsService once pkg/app builds the service. Listener-
	// addressee mutations call ReloadListeners on it after the store
	// write. Test-fire dispatches the runtime invocation through it.
	// Nil until set; mutating handlers no-op the reload, test-fire
	// returns 503.
	actions ActionsService
}

// ActionsService is the narrow surface the webapi handlers consume
// from pkg/actions.Service. Kept as an interface so the webapi package
// doesn't drag in the full actions runner at import time and tests can
// inject a fake.
type ActionsService interface {
	ReloadListeners(ctx context.Context) error
}

// MessagesService is the narrow surface the webapi handlers consume
// from pkg/messages.*Service. Kept as an interface so tests can inject
// a fake and so the webapi package doesn't drag in the router / retry
// goroutines at import time.
type MessagesService interface {
	SendMessage(ctx context.Context, req messages.SendMessageRequest) (*configstore.Message, error)
	Resend(ctx context.Context, id uint64) (messages.SendResult, error)
	SoftDelete(ctx context.Context, id uint64) error
	SoftDeleteThread(ctx context.Context, kind, key string) (int, error)
	MarkRead(ctx context.Context, id uint64) error
	MarkUnread(ctx context.Context, id uint64) error
	ReloadTacticalCallsigns(ctx context.Context) error
	ReloadPreferences(ctx context.Context) error
	EventHub() *messages.EventHub
}

// MessagesStore is the narrow read surface the handlers consume for
// pure queries (list, get, conversations, participants). Wiring passes
// a *messages.Store directly; tests may swap.
type MessagesStore interface {
	List(ctx context.Context, f messages.Filter) ([]configstore.Message, string, error)
	GetByID(ctx context.Context, id uint64) (*configstore.Message, error)
	ConversationRollup(ctx context.Context, limit int) ([]messages.ConversationSummary, error)
	ListParticipants(ctx context.Context, tacticalKey string, within time.Duration) ([]messages.Participant, time.Duration, error)
	QueryMessageHistoryByPeer(ctx context.Context, prefix string, limit int) ([]messages.MessageHistoryEntry, error)
}

// Config bundles the dependencies for NewServer.
type Config struct {
	Store         *configstore.Store
	Bridge        *modembridge.Bridge
	KissManager   *kiss.Manager
	KissCtx       context.Context // parent context for dynamically started KISS servers
	Logger        *slog.Logger
	HistoryDBPath string // path to history database, from -history-db flag
	Version       string // build-time version string reported by GET /api/version
	// MapsAuth is the registration client used by
	// POST /api/preferences/maps/register. Optional; NewServer
	// defaults to a client pointed at mapsauth.DefaultBaseURL when
	// nil so production wiring doesn't need to set it explicitly.
	// Tests inject a client pointed at an httptest.Server.
	MapsAuth *mapsauth.Client
	// MapsCache is the PMTiles download/cache manager. Optional —
	// the /api/maps/downloads handlers and ServeTilesPMTiles return
	// 503 when nil so wiring can defer construction until the cache
	// directory is known. Tests inject a Manager pointed at a temp
	// dir + httptest upstream.
	MapsCache *mapscache.Manager
	// Catalog is the download-catalog fetcher. Optional — the
	// /api/maps/catalog handler and slug-validation paths return 503
	// when nil. Tests inject a Cache pointed at an httptest upstream.
	Catalog *mapscatalog.Cache
}

// NewServer constructs a Server. Store is required; Logger defaults to
// slog.Default().
func NewServer(cfg Config) (*Server, error) {
	if cfg.Store == nil {
		return nil, fmt.Errorf("webapi: nil store")
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	kissCtx := cfg.KissCtx
	if kissCtx == nil {
		kissCtx = context.Background()
	}
	mapsClient := cfg.MapsAuth
	if mapsClient == nil {
		mapsClient = mapsauth.NewClient(mapsauth.DefaultBaseURL)
	}
	return &Server{
		store:           cfg.Store,
		bridge:          cfg.Bridge,
		kissManager:     cfg.KissManager,
		kissCtx:         kissCtx,
		logger:          logger.With("component", "webapi"),
		startedAt:       time.Now(),
		historyDBPath:   cfg.HistoryDBPath,
		version:         cfg.Version,
		updatesReloadCh: make(chan struct{}, 1),
		mapsAuth:        mapsClient,
		mapsCache:       cfg.MapsCache,
		catalog:         cfg.Catalog,
	}, nil
}

// RegisterRoutes installs the /api/* handlers on mux. Each resource
// owns its own routes via a registerX method so this stays a short
// dispatch list.
//
// Out-of-band endpoints are installed by separate helpers that
// cmd/graywolf calls explicitly after RegisterRoutes:
//
//	/api/igate              — webapi.RegisterIgate (status + simulation)
//	/api/packets            — webapi.RegisterPackets
//	/api/position           — webapi.RegisterPosition
//	/api/version            — webapi.RegisterVersion (public; mounted on
//	                          the outer mux, not the RequireAuth-wrapped
//	                          apiMux)
//
// Invariant — apiMux is the sole handler for /api/* on the outer mux
// (see pkg/app/wiring.go: mux.Handle("/api/", webauth.RequireAuth(apiMux))).
// Nothing bolts routes onto the outer mux under /api/; everything goes
// through the mux passed to RegisterRoutes (and to the RegisterXxx
// out-of-band helpers). Any middleware placed in front of apiMux
// (today: webauth.RequireAuth) MUST pass through the response status
// code and all headers — in particular the `Allow` header that Go
// 1.22's per-mux method-pattern routing generates on a 405 — unchanged.
// The handler-split work in later phases relies on that 405-with-Allow
// contract reaching the client; violating it breaks OpenAPI-derived
// clients that use method-not-allowed as a routing signal.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	s.registerChannels(mux)
	s.registerAudioDevices(mux)
	s.registerBeacons(mux)
	s.registerPtt(mux)
	s.registerTxTiming(mux)
	s.registerKiss(mux)
	s.registerAgw(mux)
	s.registerIgateConfig(mux)
	s.registerDigipeater(mux)
	s.registerStationConfig(mux)
	s.registerGps(mux)
	s.registerPositionLog(mux)
	s.registerSmartBeacon(mux)
	s.registerMessages(mux)
	s.registerMessagesConfig(mux)
	s.registerAX25Terminal(mux)
	s.registerAX25TerminalConfig(mux)
	s.registerAX25Profiles(mux)
	s.registerAX25Transcripts(mux)
	s.registerTacticals(mux)
	s.registerUpdates(mux)
	s.registerUnits(mux)
	s.registerTheme(mux)
	s.registerMaps(mux)
	s.registerCatalog(mux)
	s.registerDownloads(mux)
	s.registerActions(mux)
	s.registerOTPCredentials(mux)
	s.registerActionListeners(mux)
	s.registerActionInvocations(mux)

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/status", s.handleStatus)
}

// --- cross-component wiring setters --------------------------------------

// SetGPSReload installs the channel signalled when GPS config is saved.
func (s *Server) SetGPSReload(ch chan struct{}) { s.gpsReload = ch }

// SetBeaconReload installs the channel signalled when beacon config is
// created, updated, or deleted.
func (s *Server) SetBeaconReload(ch chan struct{}) { s.beaconReload = ch }

// SetBeaconSendNow installs the callback used by POST /api/beacons/{id}/send
// to trigger an immediate one-shot transmission of a beacon.
func (s *Server) SetBeaconSendNow(fn func(ctx context.Context, id uint32) error) {
	s.beaconSendNow = fn
}

// SetDigipeaterReload installs the channel signalled after successful
// digipeater config/rule writes. main.go drains it from a dedicated
// goroutine that pushes updated state into the running digipeater
// engine (enabled flag, mycall, dedup window, rules), so changes take
// effect without a restart. The channel is expected to be buffered
// (size 1) so signals coalesce under rapid edits.
func (s *Server) SetDigipeaterReload(ch chan struct{}) { s.digipeaterReload = ch }

// SetIgateReload installs the channel signalled after successful
// igate config or filter writes, so the running igate can pick up
// changes without a restart.
func (s *Server) SetIgateReload(ch chan struct{}) { s.igateReload = ch }

// SetPositionLogReload installs the channel signalled after successful
// position log config writes.
func (s *Server) SetPositionLogReload(ch chan struct{}) { s.positionLogReload = ch }

// SetAgwReload installs the channel signalled after a successful AGW
// config write. Wiring (pkg/app) is expected to drain this channel
// and restart the AGW TCP server so new ListenAddr / callsign /
// enabled state takes effect without a graywolf restart.
func (s *Server) SetAgwReload(ch chan struct{}) { s.agwReload = ch }

// SetSmartBeaconReload installs the channel signalled after a
// successful PUT /api/smart-beacon. Wiring (pkg/app) is expected to
// drain this channel and re-run the beacon reload pipeline so new
// curve parameters take effect without a graywolf restart. Buffer size
// 1 + coalesced non-blocking sends keep rapid edits from stacking.
func (s *Server) SetSmartBeaconReload(ch chan struct{}) { s.smartBeaconReload = ch }

// SetTxBackendReload installs the channel the Phase 3 dispatcher's
// watcher drains. Nudged by handlers (and by notifyBridgeReload) on
// every config change that might alter channel-backing membership so
// the dispatcher's atomic snapshot re-publishes.
func (s *Server) SetTxBackendReload(ch chan struct{}) { s.txBackendReload = ch }

// notifyTxBackendReload does a non-blocking send on txBackendReload.
// Coalesces bursts into a single rebuild; safe to call from any handler.
func (s *Server) notifyTxBackendReload() {
	if s.txBackendReload == nil {
		return
	}
	select {
	case s.txBackendReload <- struct{}{}:
	default:
	}
}

// SetMessagesReload installs the channel signalled after a successful
// messages preferences or tactical-callsign mutation. Wiring (pkg/app)
// drains this channel and calls Service.ReloadPreferences +
// Service.ReloadTacticalCallsigns so the router / sender pick up the
// new snapshot. Buffer size 1 + coalesced non-blocking sends keep rapid
// edits from stacking. The handlers also call the service's reload
// methods inline where the new value must be visible before the
// request returns (e.g. immediately after registering a new tactical
// label so the next compose classifies it as tactical).
func (s *Server) SetMessagesReload(ch chan struct{}) { s.messagesReload = ch }

// MessagesReload returns the messages reload channel for wiring's
// drainer goroutine. Safe to call before SetMessagesReload; nil is
// returned and the drainer becomes a no-op until the channel is
// installed.
func (s *Server) MessagesReload() <-chan struct{} { return s.messagesReload }

// SetMessagesService installs the messages service Phase 5 wiring
// constructed. Until this is called, the messages handlers return 503.
// The service is optional so tests that don't exercise the messages
// surface can omit it entirely.
func (s *Server) SetMessagesService(svc MessagesService) { s.messagesService = svc }

// SetMessagesStore installs the message repository used by pure read
// handlers (list, get, conversations, participants). When nil the
// read handlers return 503. Wiring passes a *messages.Store directly.
func (s *Server) SetMessagesStore(store MessagesStore) { s.messagesStore = store }

// SetMessagesBotDirectory overrides the bot directory used by the
// stations autocomplete endpoint. Useful for tests; production leaves
// it unset so the package default (messages.DefaultBotDirectory) wins.
func (s *Server) SetMessagesBotDirectory(dir messages.BotDirectory) { s.messagesBotDir = dir }

// SetAX25Manager installs the ax25conn session manager wiring
// constructs. Until this is called the /api/ax25/terminal WebSocket
// handler returns 503 so an ordering bug in wiring fails loud rather
// than letting the operator open a session that has nowhere to go.
func (s *Server) SetAX25Manager(m *ax25conn.Manager) { s.ax25Mgr = m }

// SetPacketLog installs the shared packet ring buffer so the AX.25
// terminal raw-tail mode can fan out live decodes. Until this is set
// the bridge surfaces a typed `raw_tail_unsupported` error envelope.
func (s *Server) SetPacketLog(l *packetlog.Log) { s.packetLog = l }

// SetActionsService installs the running Actions subsystem so the
// listener-addressee handlers can signal a reload after mutations and
// the test-fire handler can dispatch through the runtime path. Nil
// until pkg/app wiring sets it; mutating listener handlers skip the
// reload signal when nil.
func (s *Server) SetActionsService(svc ActionsService) { s.actions = svc }

// SetIgateStatusFn installs the function used by /api/status to report
// igate counters.
func (s *Server) SetIgateStatusFn(fn func() igate.Status) { s.igateStatusFn = fn }

// SetUpdatesChecker installs the updates checker post-construction.
// Called by pkg/app wiring once the checker has been built with the
// running version and configstore handle. Safe to call before the
// server starts serving; not safe to call after. Until this is called,
// GET /api/updates/status returns a synthesized "pending" response
// rather than panicking.
func (s *Server) SetUpdatesChecker(c *updatescheck.Checker) { s.updatesChecker = c }

// UpdatesReloadCh exposes the updates reload channel to wiring so the
// checker goroutine can receive on it. The channel itself is owned by
// the Server (created in NewServer, buffer size 1) so its lifetime
// matches the request handlers that send on it.
func (s *Server) UpdatesReloadCh() <-chan struct{} { return s.updatesReloadCh }

// signalUpdatesReload does a non-blocking send on updatesReloadCh.
// Coalesces bursts via the size-1 buffer (see pkg/updatescheck D4 for
// the coalescing invariant). Mirrors signalIgateReload /
// signalDigipeaterReload.
func (s *Server) signalUpdatesReload() {
	if s.updatesReloadCh == nil {
		return
	}
	select {
	case s.updatesReloadCh <- struct{}{}:
	default:
	}
}

// --- misc helpers --------------------------------------------------------

// handleHealth returns a small liveness probe payload. Used by
// orchestration (systemd, docker healthcheck) and the web UI header.
//
// @Summary  Health check
// @Tags     health
// @ID       getHealth
// @Produce  json
// @Success  200 {object} dto.HealthResponse
// @Router   /health [get]
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, dto.HealthResponse{
		Status:    "ok",
		Time:      time.Now().UTC().Format(time.RFC3339),
		StartedAt: s.startedAt.UTC().Format(time.RFC3339),
	})
}

// notifyBridgeForChannel triggers a single bridge reload for the given
// channel. ReconfigureAudioDevice does a full reload, so we only need
// to call it once regardless of how many devices are involved.
func (s *Server) notifyBridgeForChannel(ctx context.Context, _ uint32) {
	s.notifyBridgeReload(ctx)
}

// notifyBridgeReload triggers a single full bridge reload. Also kicks
// the Phase 3 TX dispatcher so the channel→backend snapshot rebuilds
// with any channel / device changes that preceded this reload.
func (s *Server) notifyBridgeReload(ctx context.Context) {
	s.notifyTxBackendReload()
	if s.bridge == nil {
		return
	}
	if err := s.bridge.ReconfigureAudioDevice(ctx, 0); err != nil {
		s.logger.Warn("bridge reconfigure", "err", err)
	}
}

// parseID parses a uint32 id from a clean path segment. Callers are
// expected to pass a pre-extracted single segment (e.g. from
// r.PathValue("id") under a Go 1.22 method-scoped route, or from a
// manually split path); parseID does no slash stripping of its own.
// A bad or empty string returns an error — route the result through
// badRequest. The strict parse guards against routing bugs: if a
// pattern accidentally captures extra path or a caller forgets to
// split, the failure is loud instead of silently succeeding.
func parseID(s string) (uint32, error) {
	n, err := strconv.ParseUint(s, 10, 32)
	return uint32(n), err
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		slog.Default().Warn("webapi: json encode failed", "err", err)
	}
}

// StripAPIPrefix is a tiny helper for tests and middleware that need
// to know whether a URL belongs to this package.
func StripAPIPrefix(path string) (string, bool) {
	const prefix = "/api/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return path[len(prefix):], true
}
