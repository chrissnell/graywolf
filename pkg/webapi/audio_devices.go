package webapi

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// registerAudioDevices installs the /api/audio-devices route tree on
// mux using Go 1.22+ method-scoped patterns. Sub-resource routes
// (/available, /scan-levels, /levels, /{id}/test-tone, /{id}/gain) are
// registered as method-scoped patterns so mux precedence correctly
// prefers literal segments over the {id} wildcard.
//
// Operation IDs used in the swag annotation blocks below are frozen
// against the constants in pkg/webapi/docs/op_ids.go. The
// `make docs-lint` target enforces the correspondence.
func (s *Server) registerAudioDevices(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/audio-devices", s.listAudioDevices)
	mux.HandleFunc("POST /api/audio-devices", s.createAudioDevice)
	mux.HandleFunc("GET /api/audio-devices/available", s.listAvailableAudioDevices)
	mux.HandleFunc("POST /api/audio-devices/scan-levels", s.scanAudioDeviceLevels)
	mux.HandleFunc("GET /api/audio-devices/levels", s.getAudioDeviceLevels)
	mux.HandleFunc("POST /api/audio-devices/{id}/test-tone", s.playTestTone)
	mux.HandleFunc("PUT /api/audio-devices/{id}/gain", s.setAudioDeviceGain)
	mux.HandleFunc("GET /api/audio-devices/{id}", s.getAudioDevice)
	mux.HandleFunc("PUT /api/audio-devices/{id}", s.updateAudioDevice)
	mux.HandleFunc("DELETE /api/audio-devices/{id}", s.deleteAudioDevice)
}

// listAudioDevices returns every configured audio device.
//
// @Summary  List audio devices
// @Tags     audio-devices
// @ID       listAudioDevices
// @Produce  json
// @Success  200 {array}  dto.AudioDeviceResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices [get]
func (s *Server) listAudioDevices(w http.ResponseWriter, r *http.Request) {
	handleList[configstore.AudioDevice](s, w, r, "list audio devices",
		s.store.ListAudioDevices, dto.AudioDeviceFromModel)
}

// createAudioDevice creates a new audio device from the request body
// and returns the persisted record (with its assigned id) on success.
//
// @Summary  Create audio device
// @Tags     audio-devices
// @ID       createAudioDevice
// @Accept   json
// @Produce  json
// @Param    body body     dto.AudioDeviceRequest true "Audio device definition"
// @Success  201  {object} dto.AudioDeviceResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices [post]
func (s *Server) createAudioDevice(w http.ResponseWriter, r *http.Request) {
	handleCreate[dto.AudioDeviceRequest](s, w, r, "create audio device",
		func(ctx context.Context, req dto.AudioDeviceRequest) (configstore.AudioDevice, error) {
			m := req.ToModel()
			return m, s.store.CreateAudioDevice(ctx, &m)
		},
		dto.AudioDeviceFromModel)
}

// getAudioDevice returns the audio device with the given id.
//
// @Summary  Get audio device
// @Tags     audio-devices
// @ID       getAudioDevice
// @Produce  json
// @Param    id  path     int true "Audio device id"
// @Success  200 {object} dto.AudioDeviceResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices/{id} [get]
func (s *Server) getAudioDevice(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	handleGet[*configstore.AudioDevice](s, w, r, "get audio device", id,
		s.store.GetAudioDevice,
		func(d *configstore.AudioDevice) dto.AudioDeviceResponse {
			return dto.AudioDeviceFromModel(*d)
		})
}

// updateAudioDevice replaces the audio device with the given id using
// the request body and returns the persisted record.
//
// @Summary  Update audio device
// @Tags     audio-devices
// @ID       updateAudioDevice
// @Accept   json
// @Produce  json
// @Param    id   path     int                    true "Audio device id"
// @Param    body body     dto.AudioDeviceRequest true "Audio device definition"
// @Success  200  {object} dto.AudioDeviceResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices/{id} [put]
func (s *Server) updateAudioDevice(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
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
}

// deleteAudioDevice removes the audio device with the given id.
// Bespoke because it has the cascade-query parameter and a 409 outcome,
// which the generic helper can't model.
//
// @Summary  Delete audio device
// @Tags     audio-devices
// @ID       deleteAudioDevice
// @Produce  json
// @Param    id      path  int  true  "Audio device id"
// @Param    cascade query bool false "Cascade-delete referencing channels"
// @Success  200 {object} dto.AudioDeviceDeleteResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  409 {object} dto.AudioDeviceDeleteConflict
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices/{id} [delete]
func (s *Server) deleteAudioDevice(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	cascade := r.URL.Query().Get("cascade") == "true"
	deleted, refs, err := s.store.DeleteAudioDeviceChecked(r.Context(), id, cascade)
	if err != nil {
		s.internalError(w, r, "delete audio device", err)
		return
	}
	if len(refs) > 0 {
		// Wire shape is preserved: the previous `map[string]any{"error": ...,
		// "channels": ...}` literal serialized the store's Channel models
		// directly; Channel's JSON tags and ChannelResponse's DTO layout
		// are field-for-field identical (name, input_device_id, …), with
		// the gorm-only CreatedAt/UpdatedAt tagged `json:"-"` on both
		// sides. Running refs through ChannelsFromModels therefore emits
		// the same bytes while giving the OpenAPI spec (and the
		// generated TypeScript client) a named type instead of
		// `{ [key: string]: unknown }`.
		writeJSON(w, http.StatusConflict, dto.AudioDeviceDeleteConflict{
			Error:    "device is referenced by channels",
			Channels: dto.ChannelsFromModels(refs),
		})
		return
	}
	s.notifyBridgeReload(r.Context())
	// Preserve wire shape: nil → null, non-empty → array. The store
	// returns a nil slice when no channels were referenced, which must
	// serialize as `"deleted":null` exactly as before.
	var respDeleted []dto.ChannelResponse
	if deleted != nil {
		respDeleted = dto.ChannelsFromModels(deleted)
	}
	writeJSON(w, http.StatusOK, dto.AudioDeviceDeleteResponse{Deleted: respDeleted})
}

// listAvailableAudioDevices asks the modem bridge to enumerate the
// audio devices visible to cpal on the host. Used by the "Detect
// Devices" button in the UI.
//
// When the bridge is not wired (early startup or headless test mode)
// this returns 200 with an empty list so the UI shows "no devices"
// instead of a generic error. A real IPC failure against a running
// bridge is surfaced as 500 so the UI can distinguish "service not
// ready" from "service broken" — the previous "always 200 []" behavior
// hid genuine failures from operators.
//
// @Summary  List available audio devices
// @Tags     audio-devices
// @ID       listAvailableAudioDevices
// @Produce  json
// @Success  200 {array}  modembridge.AvailableDevice
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices/available [get]
func (s *Server) listAvailableAudioDevices(w http.ResponseWriter, r *http.Request) {
	if s.bridge == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	devices, err := s.bridge.EnumerateAudioDevices(r.Context())
	if err != nil {
		s.internalError(w, r, "enumerate audio devices", err)
		return
	}
	writeJSON(w, http.StatusOK, devices)
}

// scanAudioDeviceLevels asks the modem bridge to briefly open each
// input device and return peak levels. Used by the "Scan Levels"
// button in the UI.
//
// Bridge-absent vs bridge-error distinction matches
// listAvailableAudioDevices: nil bridge → 200 []; live bridge returning
// an IPC error → 500 via internalError.
//
// @Summary  Scan audio device input levels
// @Tags     audio-devices
// @ID       scanAudioDeviceLevels
// @Produce  json
// @Success  200 {array}  modembridge.InputLevel
// @Failure  500 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices/scan-levels [post]
func (s *Server) scanAudioDeviceLevels(w http.ResponseWriter, r *http.Request) {
	if s.bridge == nil {
		writeJSON(w, http.StatusOK, []any{})
		return
	}
	levels, err := s.bridge.ScanInputLevels(r.Context())
	if err != nil {
		s.internalError(w, r, "scan input levels", err)
		return
	}
	writeJSON(w, http.StatusOK, levels)
}

// getAudioDeviceLevels returns the latest cached peak/rms/clipping
// measurement for every device, keyed by device id. The payload is
// sourced from an in-process cache that the bridge updates on every
// audio frame, so this handler is lock-free — there's no IPC round-trip
// and therefore no bridge-error path to distinguish.
//
// @Summary  Get audio device levels
// @Tags     audio-devices
// @ID       getAudioDeviceLevels
// @Produce  json
// @Success  200 {object} dto.AudioDeviceLevelsResponse
// @Security CookieAuth
// @Router   /audio-devices/levels [get]
func (s *Server) getAudioDeviceLevels(w http.ResponseWriter, r *http.Request) {
	if s.bridge == nil {
		writeJSON(w, http.StatusOK, dto.AudioDeviceLevelsResponse{})
		return
	}
	writeJSON(w, http.StatusOK, s.bridge.GetAllDeviceLevels())
}

// playTestTone plays a 1 kHz test tone on the specified output device
// for a fixed duration. Fails with 400 if the device is an input
// device, 404 if the device id is unknown, 503 if the bridge is not
// wired, and 500 if the bridge IPC call fails.
//
// @Summary  Play test tone on audio device
// @Tags     audio-devices
// @ID       playTestTone
// @Produce  json
// @Param    id  path     int true "Audio device id"
// @Success  200 {object} dto.TestToneResponse
// @Failure  400 {object} webtypes.ErrorResponse
// @Failure  404 {object} webtypes.ErrorResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Failure  503 {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices/{id}/test-tone [post]
func (s *Server) playTestTone(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	if s.bridge == nil {
		writeJSON(w, http.StatusServiceUnavailable, webtypes.ErrorResponse{Error: "bridge not available"})
		return
	}
	dev, err := s.store.GetAudioDevice(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, webtypes.ErrorResponse{Error: "device not found"})
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
	writeJSON(w, http.StatusOK, dto.TestToneResponse{Status: "ok"})
}

// setAudioDeviceGain updates the software gain for a device and pushes
// the new value live to the modem (so the operator hears the change
// immediately without a full reconfig). The persisted value is used by
// the modem on its next restart.
//
// Gain range matches AudioDeviceRequest: -60 dB .. +12 dB. A nil
// bridge is not an error — the store is still updated and the live
// push is skipped; the value takes effect at next modem start.
//
// @Summary  Set audio device gain
// @Tags     audio-devices
// @ID       setAudioDeviceGain
// @Accept   json
// @Produce  json
// @Param    id   path     int                           true "Audio device id"
// @Param    body body     dto.AudioDeviceSetGainRequest true "Gain setting"
// @Success  200  {object} dto.AudioDeviceResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  404  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Security CookieAuth
// @Router   /audio-devices/{id}/gain [put]
func (s *Server) setAudioDeviceGain(w http.ResponseWriter, r *http.Request) {
	id, err := parseID(r.PathValue("id"))
	if err != nil {
		badRequest(w, "invalid id")
		return
	}
	body, err := decodeJSON[dto.AudioDeviceSetGainRequest](r)
	if err != nil {
		badRequest(w, "invalid request body")
		return
	}
	if err := body.Validate(); err != nil {
		badRequest(w, err.Error())
		return
	}
	dev, err := s.store.GetAudioDevice(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, webtypes.ErrorResponse{Error: "device not found"})
		return
	}
	dev.GainDB = body.GainDB
	if err := s.store.UpdateAudioDevice(r.Context(), dev); err != nil {
		s.internalError(w, r, "update audio device gain", err)
		return
	}
	// Live update to modem — no full reconfig needed. Failure here is
	// not fatal: the persisted value still wins on next modem start, so
	// we log and continue with a 200 rather than confusing the caller
	// with a 500 after a successful store write.
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
