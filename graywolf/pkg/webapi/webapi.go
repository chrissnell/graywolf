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
	store         *configstore.Store
	bridge        *modembridge.Bridge
	kissManager   *kiss.Manager
	kissCtx       context.Context // long-lived context for KISS server goroutines
	logger        *slog.Logger
	startedAt     time.Time
	igateStatusFn func() igate.Status
	gpsReload        chan struct{}                              // signalled when GPS config changes
	beaconReload     chan struct{}                              // signalled when beacon config changes
	digipeaterReload chan struct{}                              // signalled when digipeater config/rules change
	beaconSendNow    func(ctx context.Context, id uint32) error // installed by main.go to trigger immediate beacon send
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

// RegisterRoutes installs the /api/* handlers on mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	s.registerChannels(mux)
	s.registerAudioDevices(mux)
	s.registerBeacons(mux)

	// PTT — upsert/get by channel
	mux.HandleFunc("/api/ptt", s.handlePttCollection)
	mux.HandleFunc("/api/ptt/", s.handlePttByChannel)

	// TX timing — upsert/get by channel
	mux.HandleFunc("/api/tx-timing", s.handleTxTimingCollection)
	mux.HandleFunc("/api/tx-timing/", s.handleTxTimingByChannel)

	// KISS interfaces — full CRUD
	mux.HandleFunc("/api/kiss", s.handleKissCollection)
	mux.HandleFunc("/api/kiss/", s.handleKissItem)

	// AGW — singleton get/update
	mux.HandleFunc("/api/agw", s.handleAgw)

	// iGate config — singleton get/update (igate.go still has status + sim)
	mux.HandleFunc("/api/igate/config", s.handleIgateConfig)
	mux.HandleFunc("/api/igate/filters", s.handleIgateFilters)
	mux.HandleFunc("/api/igate/filters/", s.handleIgateFilter)

	// Digipeater — config singleton + rules CRUD
	mux.HandleFunc("/api/digipeater", s.handleDigipeaterConfig)
	mux.HandleFunc("/api/digipeater/rules", s.handleDigipeaterRules)
	mux.HandleFunc("/api/digipeater/rules/", s.handleDigipeaterRule)

	// GPS — singleton get/update + serial port enumeration
	mux.HandleFunc("/api/gps", s.handleGps)
	mux.HandleFunc("/api/gps/available", s.handleGpsAvailable)

	// /api/igate — status + simulation in igate.go (RegisterIgate)
	// /api/packets — in packets.go (RegisterPackets)
	// /api/position — in position.go (RegisterPosition)
	mux.HandleFunc("/api/health", s.handleHealth)
	mux.HandleFunc("/api/status", s.handleStatus)
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"time":       time.Now().UTC().Format(time.RFC3339),
		"started_at": s.startedAt.UTC().Format(time.RFC3339),
	})
}

// SetGPSReload installs the channel signalled when GPS config is saved.
func (s *Server) SetGPSReload(ch chan struct{}) {
	s.gpsReload = ch
}

// SetBeaconReload installs the channel signalled when beacon config is
// created, updated, or deleted.
func (s *Server) SetBeaconReload(ch chan struct{}) {
	s.beaconReload = ch
}

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
func (s *Server) SetDigipeaterReload(ch chan struct{}) {
	s.digipeaterReload = ch
}

// SetIgateStatusFn installs the function used by /api/status to report igate counters.
func (s *Server) SetIgateStatusFn(fn func() igate.Status) {
	s.igateStatusFn = fn
}

// StatusDTO is the JSON shape returned by GET /api/status.
type StatusDTO struct {
	UptimeSeconds int64           `json:"uptime_seconds"`
	Channels      []StatusChannel `json:"channels"`
	Igate         *igate.Status   `json:"igate,omitempty"`
}

// StatusChannel pairs a channel config with its live stats.
type StatusChannel struct {
	ID        uint32  `json:"id"`
	Name      string  `json:"name"`
	ModemType string  `json:"modem_type"`
	BitRate   uint32  `json:"bit_rate"`
	RxFrames  uint64  `json:"rx_frames"`
	TxFrames  uint64  `json:"tx_frames"`
	DcdState  bool    `json:"dcd_state"`
	AudioPeak float32 `json:"audio_peak"`
}

// GET /api/status — aggregated dashboard data
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	dto := StatusDTO{
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
	}

	// Channels + stats
	channels, err := s.store.ListChannels(r.Context())
	if err == nil {
		for _, ch := range channels {
			sc := StatusChannel{
				ID:        ch.ID,
				Name:      ch.Name,
				ModemType: ch.ModemType,
				BitRate:   ch.BitRate,
			}
			if s.bridge != nil {
				if stats, ok := s.bridge.GetChannelStats(uint32(ch.ID)); ok {
					sc.RxFrames = stats.RxFrames
					sc.TxFrames = stats.TxFrames
					sc.DcdState = stats.DcdState
					sc.AudioPeak = stats.AudioLevelPeak
				}
			}
			dto.Channels = append(dto.Channels, sc)
		}
	}

	// iGate
	if s.igateStatusFn != nil {
		st := s.igateStatusFn()
		dto.Igate = &st
	}

	writeJSON(w, http.StatusOK, dto)
}

// notifyBridgeForDevice tells the modem bridge to hot-reconfigure a device.
// Best-effort: logs on failure but does not propagate to the caller.
func (s *Server) notifyBridgeForDevice(ctx context.Context, deviceID uint32) {
	if s.bridge == nil {
		return
	}
	if err := s.bridge.ReconfigureAudioDevice(ctx, deviceID); err != nil {
		s.logger.Warn("bridge reconfigure", "device_id", deviceID, "err", err)
	}
}

// notifyBridgeForChannel looks up the channel's audio devices and reconfigures them.
func (s *Server) notifyBridgeForChannel(ctx context.Context, channelID uint32) {
	if s.bridge == nil {
		return
	}
	ch, err := s.store.GetChannel(ctx, channelID)
	if err != nil {
		s.logger.Warn("bridge reconfigure: get channel", "channel_id", channelID, "err", err)
		return
	}
	s.notifyBridgeForDevice(ctx, ch.InputDeviceID)
	if ch.OutputDeviceID != 0 {
		s.notifyBridgeForDevice(ctx, ch.OutputDeviceID)
	}
}

// internalError logs the real error with request context and writes a
// generic message to the client. Use for every 5xx response so we don't
// leak GORM/driver strings (e.g. "UNIQUE constraint failed: users.username")
// that enable account or schema enumeration.
func (s *Server) internalError(w http.ResponseWriter, r *http.Request, op string, err error) {
	s.logger.ErrorContext(r.Context(), "webapi internal error", "op", op, "err", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
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

// StripAPIPrefix is a tiny helper for tests and middleware that need to
// know whether a URL belongs to this package.
func StripAPIPrefix(path string) (string, bool) {
	const prefix = "/api/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return path[len(prefix):], true
}
