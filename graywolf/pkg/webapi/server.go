// Package webapi is graywolf's REST management API.
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
)

// Server routes /api/* requests. It does not own the underlying
// listener; cmd/graywolf composes it into its main mux.
type Server struct {
	store            *configstore.Store
	bridge           *modembridge.Bridge
	kissManager      *kiss.Manager
	kissCtx          context.Context // long-lived context for KISS server goroutines
	logger           *slog.Logger
	startedAt        time.Time
	igateStatusFn    func() igate.Status
	gpsReload        chan struct{}                              // signalled when GPS config changes
	beaconReload     chan struct{}                              // signalled when beacon config changes
	digipeaterReload chan struct{}                              // signalled when digipeater config/rules change
	igateReload      chan struct{}                              // signalled when igate config/filters change
	positionLogReload chan struct{}                             // signalled when position log config changes
	beaconSendNow    func(ctx context.Context, id uint32) error // triggers an immediate beacon send
}

// Config bundles the dependencies for NewServer.
type Config struct {
	Store       *configstore.Store
	Bridge      *modembridge.Bridge
	KissManager *kiss.Manager
	KissCtx     context.Context // parent context for dynamically started KISS servers
	Logger      *slog.Logger
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
		store:       cfg.Store,
		bridge:      cfg.Bridge,
		kissManager: cfg.KissManager,
		kissCtx:     kissCtx,
		logger:      logger.With("component", "webapi"),
		startedAt:   time.Now(),
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

	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleStatus)
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

// SetIgateStatusFn installs the function used by /api/status to report
// igate counters.
func (s *Server) SetIgateStatusFn(fn func() igate.Status) { s.igateStatusFn = fn }

// --- misc helpers --------------------------------------------------------

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"time":       time.Now().UTC().Format(time.RFC3339),
		"started_at": s.startedAt.UTC().Format(time.RFC3339),
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

func parseID(s string) (uint32, error) {
	// Strip trailing path segments (e.g. "1/stats" → "1")
	if i := strings.Index(s, "/"); i >= 0 {
		s = s[:i]
	}
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
