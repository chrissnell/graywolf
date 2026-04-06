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

	// GPS — singleton get/update
	mux.HandleFunc("/api/gps", s.handleGps)

	// /api/igate — status + simulation in igate.go (RegisterIgate)
	// /api/packets — in packets.go (RegisterPackets)
	// /api/position — in position.go (RegisterPosition)
	mux.HandleFunc("/api/health", s.handleHealth)
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
		s.notifyBridgeForDevice(r.Context(), c.AudioDeviceID)
		writeJSON(w, http.StatusOK, c)
	case http.MethodDelete:
		if err := s.store.DeleteChannel(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
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

// /api/audio-devices/{id} or /api/audio-devices/available
func (s *Server) handleAudioDevice(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/audio-devices/")
	if rest == "available" {
		// TODO(phase6): audio device enumeration IPC round-trip
		writeJSON(w, http.StatusOK, []any{})
		return
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
		if err := s.store.DeleteAudioDevice(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
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
		writeJSON(w, http.StatusOK, b)
	case http.MethodDelete:
		if err := s.store.DeleteBeacon(id); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"status": "ok",
		"time":   time.Now().UTC().Format(time.RFC3339),
	})
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

// notifyBridgeForChannel looks up the channel's audio device and reconfigures it.
func (s *Server) notifyBridgeForChannel(ctx context.Context, channelID uint32) {
	if s.bridge == nil {
		return
	}
	ch, err := s.store.GetChannel(channelID)
	if err != nil {
		s.logger.Warn("bridge reconfigure: get channel", "channel_id", channelID, "err", err)
		return
	}
	s.notifyBridgeForDevice(ctx, ch.AudioDeviceID)
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
