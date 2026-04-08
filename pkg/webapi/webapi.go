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
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/modembridge"
)

// Server routes /api/* requests. It does not own the underlying
// listener; cmd/graywolf composes it into its main mux.
type Server struct {
	store        *configstore.Store
	bridge       *modembridge.Bridge
	kissManager  *kiss.Manager
	kissCtx      context.Context // long-lived context for KISS server goroutines
	logger       *slog.Logger
	startedAt    time.Time
	igateStatusFn func() IgateStatus
	gpsReload    chan struct{} // signalled when GPS config changes
	beaconReload chan struct{} // signalled when beacon config changes
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
	// Channels — full CRUD + stats subpath
	mux.HandleFunc("/api/channels", s.handleChannels)
	mux.HandleFunc("/api/channels/", s.handleChannelsSubpath)

	// Audio devices — full CRUD + available enumeration
	mux.HandleFunc("/api/audio-devices", s.handleAudioDevices)
	mux.HandleFunc("/api/audio-devices/", s.handleAudioDevice)

	// Beacons — full CRUD
	mux.HandleFunc("/api/beacons", s.handleBeacons)
	mux.HandleFunc("/api/beacons/", s.handleBeacon)

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

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		channels, err := s.store.ListChannels()
		if err != nil {
			s.logger.Warn("list channels", "err", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, channels)
	case http.MethodPost:
		var c configstore.Channel
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.CreateChannel(&c); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, c)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleChannelsSubpath routes /api/channels/{id} and /api/channels/{id}/stats
func (s *Server) handleChannelsSubpath(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/channels/")
	parts := strings.SplitN(path, "/", 2)

	// /api/channels/{id}/stats
	if len(parts) == 2 && parts[1] == "stats" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleChannelStats(w, r, parts[0])
		return
	}

	// /api/channels/{id} — GET, PUT, DELETE
	id, err := parseID(parts[0])
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		c, err := s.store.GetChannel(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, c)
	case http.MethodPut:
		var c configstore.Channel
		if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		c.ID = id
		if err := s.store.UpdateChannel(&c); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyBridgeForDevice(r.Context(), c.InputDeviceID)
		if c.OutputDeviceID != 0 {
			s.notifyBridgeForDevice(r.Context(), c.OutputDeviceID)
		}
		writeJSON(w, http.StatusOK, c)
	case http.MethodDelete:
		// Look up device IDs before deleting so we can notify the bridge.
		ch, _ := s.store.GetChannel(id)
		if err := s.store.DeleteChannel(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if ch != nil {
			s.notifyBridgeForDevice(r.Context(), ch.InputDeviceID)
			if ch.OutputDeviceID != 0 {
				s.notifyBridgeForDevice(r.Context(), ch.OutputDeviceID)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /api/channels/{id}/stats
func (s *Server) handleChannelStats(w http.ResponseWriter, _ *http.Request, idStr string) {
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		http.Error(w, "invalid channel id", http.StatusBadRequest)
		return
	}
	if s.bridge == nil {
		http.Error(w, "bridge not available", http.StatusServiceUnavailable)
		return
	}
	stats, ok := s.bridge.GetChannelStats(uint32(id))
	if !ok {
		http.Error(w, "no stats for channel", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

// /api/audio-devices — list + create
func (s *Server) handleAudioDevices(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		devices, err := s.store.ListAudioDevices()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, devices)
	case http.MethodPost:
		var d configstore.AudioDevice
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.CreateAudioDevice(&d); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusCreated, d)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /api/audio-devices/{id} or /api/audio-devices/available or /api/audio-devices/levels
func (s *Server) handleAudioDevice(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/audio-devices/")
	if rest == "available" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.bridge == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		devices, err := s.bridge.EnumerateAudioDevices(r.Context())
		if err != nil {
			s.logger.Warn("enumerate audio devices", "err", err)
			// Return empty list rather than error — bridge may not be running yet.
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusOK, devices)
		return
	}
	if rest == "scan-levels" {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.bridge == nil {
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		levels, err := s.bridge.ScanInputLevels(r.Context())
		if err != nil {
			s.logger.Warn("scan input levels", "err", err)
			writeJSON(w, http.StatusOK, []any{})
			return
		}
		writeJSON(w, http.StatusOK, levels)
		return
	}
	if rest == "levels" {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		s.handleDeviceLevels(w)
		return
	}

	// /api/audio-devices/{id}/test-tone or /api/audio-devices/{id}/gain
	if parts := strings.SplitN(rest, "/", 2); len(parts) == 2 {
		switch parts[1] {
		case "test-tone":
			if r.Method != http.MethodPost {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleTestTone(w, r, parts[0])
			return
		case "gain":
			if r.Method != http.MethodPut {
				http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
				return
			}
			s.handleSetGain(w, r, parts[0])
			return
		}
	}

	id, err := parseID(rest)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		d, err := s.store.GetAudioDevice(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, d)
	case http.MethodPut:
		var d configstore.AudioDevice
		if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		d.ID = id
		if err := s.store.UpdateAudioDevice(&d); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.notifyBridgeForDevice(r.Context(), id)
		writeJSON(w, http.StatusOK, d)
	case http.MethodDelete:
		cascade := r.URL.Query().Get("cascade") == "true"
		deps, err := s.store.ChannelsForDevice(id)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if len(deps) > 0 && !cascade {
			names := make([]string, len(deps))
			for i, ch := range deps {
				names[i] = ch.Name
			}
			writeJSON(w, http.StatusConflict, map[string]any{
				"error":    "device is referenced by channels",
				"channels": names,
			})
			return
		}
		if len(deps) > 0 {
			if _, err := s.store.DeleteAudioDeviceCascade(id); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		} else {
			if err := s.store.DeleteAudioDevice(id); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
		s.notifyBridgeForDevice(r.Context(), id)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// GET /api/audio-devices/levels — per-device audio levels for meters.
func (s *Server) handleDeviceLevels(w http.ResponseWriter) {
	if s.bridge == nil {
		writeJSON(w, http.StatusOK, map[string]any{})
		return
	}
	writeJSON(w, http.StatusOK, s.bridge.GetAllDeviceLevels())
}

// POST /api/audio-devices/{id}/test-tone — play a test tone on an output device.
func (s *Server) handleTestTone(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := parseID(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "bridge not available"})
		return
	}
	dev, err := s.store.GetAudioDevice(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if dev.Direction != "output" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "test tone only supported on output devices"})
		return
	}
	deviceName := audioDeviceName(dev)
	if err := s.bridge.PlayTestTone(r.Context(), id, deviceName, dev.SampleRate, dev.Channels); err != nil {
		s.logger.Warn("test tone failed", "device_id", id, "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// PUT /api/audio-devices/{id}/gain — set software gain for a device.
func (s *Server) handleSetGain(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := parseID(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	var body struct {
		GainDB float32 `json:"gain_db"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if body.GainDB < -60 || body.GainDB > 12 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "gain_db must be between -60 and +12"})
		return
	}
	dev, err := s.store.GetAudioDevice(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	dev.GainDB = body.GainDB
	if err := s.store.UpdateAudioDevice(dev); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	// Live update to modem — no full reconfig needed.
	if s.bridge != nil {
		if err := s.bridge.SetDeviceGain(id, body.GainDB); err != nil {
			s.logger.Warn("set device gain", "device_id", id, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, dev)
}

// audioDeviceName picks the cpal device name from an AudioDevice.
func audioDeviceName(d *configstore.AudioDevice) string {
	if d.SourcePath != "" {
		return d.SourcePath
	}
	return d.Name
}

// /api/beacons — list + create
func (s *Server) handleBeacons(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		beacons, err := s.store.ListBeacons()
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, beacons)
	case http.MethodPost:
		var b configstore.Beacon
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		if err := s.store.CreateBeacon(&b); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.signalBeaconReload()
		writeJSON(w, http.StatusCreated, b)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// /api/beacons/{id} — get, update, delete
func (s *Server) handleBeacon(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(strings.TrimPrefix(r.URL.Path, "/api/beacons/"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}
	switch r.Method {
	case http.MethodGet:
		b, err := s.store.GetBeacon(id)
		if err != nil {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
			return
		}
		writeJSON(w, http.StatusOK, b)
	case http.MethodPut:
		var b configstore.Beacon
		if err := json.NewDecoder(r.Body).Decode(&b); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
			return
		}
		b.ID = id
		if err := s.store.UpdateBeacon(&b); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.signalBeaconReload()
		writeJSON(w, http.StatusOK, b)
	case http.MethodDelete:
		if err := s.store.DeleteBeacon(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		s.signalBeaconReload()
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// signalBeaconReload performs a non-blocking send on the beacon reload
// channel; coalesces if a previous signal is still buffered.
func (s *Server) signalBeaconReload() {
	if s.beaconReload == nil {
		return
	}
	select {
	case s.beaconReload <- struct{}{}:
	default:
	}
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

// SetIgateStatusFn installs the function used by /api/status to report igate counters.
func (s *Server) SetIgateStatusFn(fn func() IgateStatus) {
	s.igateStatusFn = fn
}

// StatusDTO is the JSON shape returned by GET /api/status.
type StatusDTO struct {
	UptimeSeconds int64                            `json:"uptime_seconds"`
	Channels      []StatusChannel                  `json:"channels"`
	Igate         *IgateStatus                     `json:"igate,omitempty"`
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
	channels, err := s.store.ListChannels()
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
	ch, err := s.store.GetChannel(channelID)
	if err != nil {
		s.logger.Warn("bridge reconfigure: get channel", "channel_id", channelID, "err", err)
		return
	}
	s.notifyBridgeForDevice(ctx, ch.InputDeviceID)
	if ch.OutputDeviceID != 0 {
		s.notifyBridgeForDevice(ctx, ch.OutputDeviceID)
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

// StripAPIPrefix is a tiny helper for tests and middleware that need to
// know whether a URL belongs to this package.
func StripAPIPrefix(path string) (string, bool) {
	const prefix = "/api/"
	if !strings.HasPrefix(path, prefix) {
		return "", false
	}
	return path[len(prefix):], true
}
