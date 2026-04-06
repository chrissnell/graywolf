// Package webapi is graywolf's REST management API.
package webapi

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/modembridge"
)

// Server routes /api/* requests. It does not own the underlying
// listener; cmd/graywolf composes it into its main mux.
type Server struct {
	store  *configstore.Store
	bridge *modembridge.Bridge
	logger *slog.Logger
}

// Config bundles the dependencies for NewServer.
type Config struct {
	Store  *configstore.Store
	Bridge *modembridge.Bridge
	Logger *slog.Logger
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
	return &Server{
		store:  cfg.Store,
		bridge: cfg.Bridge,
		logger: logger.With("component", "webapi"),
	}, nil
}

// RegisterRoutes installs the /api/* handlers on mux.
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/channels", s.handleChannels)
	mux.HandleFunc("/api/channels/", s.handleChannelsSubpath)
	mux.HandleFunc("/api/beacons", s.handleBeacons)
	mux.HandleFunc("/api/beacons/", s.handleBeacons)
	mux.HandleFunc("/api/audio-devices", s.handleAudioDevices)
	mux.HandleFunc("/api/ptt", s.stub("ptt"))
	mux.HandleFunc("/api/kiss-interfaces", s.stub("kiss-interfaces"))
	mux.HandleFunc("/api/agw", s.stub("agw"))
	mux.HandleFunc("/api/tx-timing", s.stub("tx-timing"))
	mux.HandleFunc("/api/digipeater", s.stub("digipeater"))
	// /api/igate — real handler in igate.go (RegisterIgate)
	// /api/packets — real handler in packets.go (RegisterPackets)
	mux.HandleFunc("/api/health", s.handleHealth)
}

// ChannelDTO is the JSON shape returned by GET /api/channels.
type ChannelDTO struct {
	ID            uint32 `json:"id"`
	Name          string `json:"name"`
	AudioDeviceID uint32 `json:"audio_device_id"`
	AudioChannel  uint32 `json:"audio_channel"`
	ModemType     string `json:"modem_type"`
	BitRate       uint32 `json:"bit_rate"`
	MarkFreq      uint32 `json:"mark_freq"`
	SpaceFreq     uint32 `json:"space_freq"`
	Profile       string `json:"profile"`
	TxDelayMs     uint32 `json:"tx_delay_ms"`
	TxTailMs      uint32 `json:"tx_tail_ms"`
}

// BeaconDTO is the JSON shape returned by GET /api/beacons.
type BeaconDTO struct {
	ID        uint32    `json:"id"`
	Channel   uint32    `json:"channel"`
	Callsign  string    `json:"callsign"`
	Path      string    `json:"path"`
	Interval  string    `json:"interval"`
	Text      string    `json:"text"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func (s *Server) handleChannels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	channels, err := s.store.ListChannels()
	if err != nil {
		s.logger.Warn("list channels", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	out := make([]ChannelDTO, 0, len(channels))
	for _, c := range channels {
		out = append(out, ChannelDTO{
			ID:            c.ID,
			Name:          c.Name,
			AudioDeviceID: c.AudioDeviceID,
			AudioChannel:  c.AudioChannel,
			ModemType:     c.ModemType,
			BitRate:       c.BitRate,
			MarkFreq:      c.MarkFreq,
			SpaceFreq:     c.SpaceFreq,
			Profile:       c.Profile,
			TxDelayMs:     c.TxDelayMs,
			TxTailMs:      c.TxTailMs,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// handleChannelsSubpath routes /api/channels/{id}/stats
func (s *Server) handleChannelsSubpath(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/channels/")
	parts := strings.SplitN(path, "/", 2)
	if len(parts) == 2 && parts[1] == "stats" {
		s.handleChannelStats(w, r, parts[0])
		return
	}
	s.handleChannels(w, r)
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

// GET /api/audio-devices
func (s *Server) handleAudioDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	devices, err := s.store.ListAudioDevices()
	if err != nil {
		http.Error(w, "list devices: "+err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

func (s *Server) handleBeacons(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, []BeaconDTO{})
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *Server) stub(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusNotImplemented, map[string]string{
			"error":    "not implemented",
			"endpoint": name,
			"phase":    "pending Phase 6",
		})
	}
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
