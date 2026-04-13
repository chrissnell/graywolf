package webapi

import (
	"context"
	"net/http"
	"strings"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// registerAudioDevices installs /api/audio-devices and its subpaths.
func (s *Server) registerAudioDevices(mux *http.ServeMux) {
	mux.HandleFunc("/api/audio-devices", s.handleAudioDevicesCollection)
	mux.HandleFunc("/api/audio-devices/", s.handleAudioDevicesItem)
}

// GET/POST /api/audio-devices
func (s *Server) handleAudioDevicesCollection(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		handleList[configstore.AudioDevice](s, w, r, "list audio devices",
			s.store.ListAudioDevices, dto.AudioDeviceFromModel)
	case http.MethodPost:
		handleCreate[dto.AudioDeviceRequest](s, w, r, "create audio device",
			func(ctx context.Context, req dto.AudioDeviceRequest) (configstore.AudioDevice, error) {
				m := req.ToModel()
				return m, s.store.CreateAudioDevice(ctx, &m)
			},
			dto.AudioDeviceFromModel)
	default:
		methodNotAllowed(w)
	}
}

// /api/audio-devices/{id} plus the /available, /levels, /scan-levels,
// /{id}/test-tone, /{id}/gain subpaths.
func (s *Server) handleAudioDevicesItem(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/api/audio-devices/")
	switch rest {
	case "available":
		s.handleAudioDevicesAvailable(w, r)
		return
	case "scan-levels":
		s.handleAudioDevicesScanLevels(w, r)
		return
	case "levels":
		s.handleAudioDevicesLevels(w, r)
		return
	}

	// /{id}/test-tone or /{id}/gain
	if parts := strings.SplitN(rest, "/", 2); len(parts) == 2 {
		switch parts[1] {
		case "test-tone":
			if r.Method != http.MethodPost {
				methodNotAllowed(w)
				return
			}
			s.handleTestTone(w, r, parts[0])
			return
		case "gain":
			if r.Method != http.MethodPut {
				methodNotAllowed(w)
				return
			}
			s.handleSetGain(w, r, parts[0])
			return
		}
	}

	id, err := parseID(rest)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	switch r.Method {
	case http.MethodGet:
		handleGet[*configstore.AudioDevice](s, w, r, id,
			s.store.GetAudioDevice,
			func(d *configstore.AudioDevice) dto.AudioDeviceResponse {
				return dto.AudioDeviceFromModel(*d)
			})
	case http.MethodPut:
		handleUpdate[dto.AudioDeviceRequest](s, w, r, "update audio device", id,
			func(ctx context.Context, id uint32, req dto.AudioDeviceRequest) (configstore.AudioDevice, error) {
				m := req.ToUpdate(id)
				if err := s.store.UpdateAudioDevice(ctx, &m); err != nil {
					return configstore.AudioDevice{}, err
				}
				s.notifyBridgeReload(ctx)
				return m, nil
			},
			dto.AudioDeviceFromModel)
	case http.MethodDelete:
		s.handleAudioDeviceDelete(w, r, id)
	default:
		methodNotAllowed(w)
	}
}

// handleAudioDeviceDelete is bespoke because it has the cascade-query
// parameter and a 409 outcome, which the generic helper can't model.
func (s *Server) handleAudioDeviceDelete(w http.ResponseWriter, r *http.Request, id uint32) {
	cascade := r.URL.Query().Get("cascade") == "true"
	deleted, refs, err := s.store.DeleteAudioDeviceChecked(r.Context(), id, cascade)
	if err != nil {
		s.internalError(w, r, "delete audio device", err)
		return
	}
	if len(refs) > 0 {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":    "device is referenced by channels",
			"channels": refs,
		})
		return
	}
	s.notifyBridgeReload(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{"deleted": deleted})
}

// GET /api/audio-devices/available — ask the modem what devices exist.
func (s *Server) handleAudioDevicesAvailable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
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
}

// POST /api/audio-devices/scan-levels — ask the modem to scan input levels.
func (s *Server) handleAudioDevicesScanLevels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
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
}

// GET /api/audio-devices/levels — per-device audio levels for meters.
func (s *Server) handleAudioDevicesLevels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
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
		badRequest(w, "invalid id")
		return
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "bridge not available"})
		return
	}
	dev, err := s.store.GetAudioDevice(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	if dev.Direction != "output" {
		badRequest(w, "test tone only supported on output devices")
		return
	}
	deviceName := audioDeviceName(dev)
	if err := s.bridge.PlayTestTone(r.Context(), id, deviceName, dev.SampleRate, dev.Channels); err != nil {
		s.internalError(w, r, "play test tone", err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

// PUT /api/audio-devices/{id}/gain — set software gain for a device.
func (s *Server) handleSetGain(w http.ResponseWriter, r *http.Request, idStr string) {
	id, err := parseID(idStr)
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	type gainBody struct {
		GainDB float32 `json:"gain_db"`
	}
	body, err := decodeJSON[gainBody](r)
	if err != nil {
		badRequest(w, "invalid request body")
		return
	}
	if body.GainDB < -60 || body.GainDB > 12 {
		badRequest(w, "gain_db must be between -60 and +12")
		return
	}
	dev, err := s.store.GetAudioDevice(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "device not found"})
		return
	}
	dev.GainDB = body.GainDB
	if err := s.store.UpdateAudioDevice(r.Context(), dev); err != nil {
		s.internalError(w, r, "update audio device gain", err)
		return
	}
	// Live update to modem — no full reconfig needed.
	if s.bridge != nil {
		if err := s.bridge.SetDeviceGain(id, body.GainDB); err != nil {
			s.logger.Warn("set device gain", "device_id", id, "err", err)
		}
	}
	writeJSON(w, http.StatusOK, dto.AudioDeviceFromModel(*dev))
}

// audioDeviceName picks the cpal device name from an AudioDevice.
func audioDeviceName(d *configstore.AudioDevice) string {
	if d.SourcePath != "" {
		return d.SourcePath
	}
	return d.Name
}
