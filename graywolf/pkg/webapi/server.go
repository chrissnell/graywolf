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

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/igate"
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/modembridge"
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
	gpsReload         chan struct{}                              // signalled when GPS config changes
	beaconReload      chan struct{}                              // signalled when beacon config changes
	digipeaterReload  chan struct{}                              // signalled when digipeater config/rules change
	igateReload       chan struct{}                              // signalled when igate config/filters change
	positionLogReload chan struct{}                              // signalled when position log config changes
	agwReload         chan struct{}                              // signalled when AGW config changes
	smartBeaconReload chan struct{}                              // signalled when smart-beacon singleton config changes
	beaconSendNow     func(ctx context.Context, id uint32) error // triggers an immediate beacon send
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
	return &Server{
		store:         cfg.Store,
		bridge:        cfg.Bridge,
		kissManager:   cfg.KissManager,
		kissCtx:       kissCtx,
		logger:        logger.With("component", "webapi"),
		startedAt:     time.Now(),
		historyDBPath: cfg.HistoryDBPath,
		version:       cfg.Version,
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
	s.registerGps(mux)
	s.registerPositionLog(mux)
	s.registerSmartBeacon(mux)

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

// SetIgateStatusFn installs the function used by /api/status to report
// igate counters.
func (s *Server) SetIgateStatusFn(fn func() igate.Status) { s.igateStatusFn = fn }

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

// notifyBridgeReload triggers a single full bridge reload.
func (s *Server) notifyBridgeReload(ctx context.Context) {
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
