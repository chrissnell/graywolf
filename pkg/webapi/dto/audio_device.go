package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/modembridge"
)

// AudioDeviceRequest is the body accepted by POST /api/audio-devices
// and PUT /api/audio-devices/{id}.
type AudioDeviceRequest struct {
	Name       string  `json:"name"`
	Direction  string  `json:"direction"`
	SourceType string  `json:"source_type"`
	DevicePath string  `json:"device_path"`
	SampleRate uint32  `json:"sample_rate"`
	Channels   uint32  `json:"channels"`
	Format     string  `json:"format"`
	GainDB     float32 `json:"gain_db"`
}

// Validate ensures required fields are set and gain is in range.
func (r AudioDeviceRequest) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if r.Direction != "input" && r.Direction != "output" {
		return fmt.Errorf("direction must be 'input' or 'output'")
	}
	if r.SourceType == "" {
		return fmt.Errorf("source_type is required")
	}
	if r.GainDB < -60 || r.GainDB > 12 {
		return fmt.Errorf("gain_db must be between -60 and +12")
	}
	return nil
}

func (r AudioDeviceRequest) ToModel() configstore.AudioDevice {
	gain := r.GainDB
	// Default output devices to -12 dB so they don't overdrive the radio
	if r.Direction == "output" && gain == 0 {
		gain = -12
	}
	return configstore.AudioDevice{
		Name:       r.Name,
		Direction:  r.Direction,
		SourceType: r.SourceType,
		SourcePath: r.DevicePath,
		SampleRate: r.SampleRate,
		Channels:   1, // always mono; Rust auto-negotiates if device requires stereo
		Format:     r.Format,
		GainDB:     gain,
	}
}

func (r AudioDeviceRequest) ToUpdate(id uint32) configstore.AudioDevice {
	m := r.ToModel()
	m.ID = id
	return m
}

// AudioDeviceResponse is the body returned by GET/POST/PUT for a device.
type AudioDeviceResponse struct {
	ID uint32 `json:"id"`
	AudioDeviceRequest
}

func AudioDeviceFromModel(m configstore.AudioDevice) AudioDeviceResponse {
	return AudioDeviceResponse{
		ID: m.ID,
		AudioDeviceRequest: AudioDeviceRequest{
			Name:       m.Name,
			Direction:  m.Direction,
			SourceType: m.SourceType,
			DevicePath: m.SourcePath,
			SampleRate: m.SampleRate,
			Channels:   m.Channels,
			Format:     m.Format,
			GainDB:     m.GainDB,
		},
	}
}

func AudioDevicesFromModels(ms []configstore.AudioDevice) []AudioDeviceResponse {
	out := make([]AudioDeviceResponse, len(ms))
	for i, m := range ms {
		out[i] = AudioDeviceFromModel(m)
	}
	return out
}

// AudioDeviceDeleteResponse is the body returned by
// DELETE /api/audio-devices/{id} on success. Deleted lists the
// channels that were removed alongside the device when cascade was
// requested; empty when the device had no referencing channels.
type AudioDeviceDeleteResponse struct {
	Deleted []ChannelResponse `json:"deleted"`
}

// AudioDeviceDeleteConflict is the body returned by
// DELETE /api/audio-devices/{id} with a 409 when the device is
// referenced by one or more channels and the caller did not request
// cascade deletion. The channels slice lists the referencing channels
// so the UI can surface them in the confirm dialog.
//
// Wire shape matches the pre-typed `map[string]any{"error": ..., "channels": ...}`
// literal previously emitted by the handler — byte-identical when the
// slice fields are populated the same way.
type AudioDeviceDeleteConflict struct {
	Error    string            `json:"error"`
	Channels []ChannelResponse `json:"channels"`
}

// AudioDeviceSetGainRequest is the body for PUT /api/audio-devices/{id}/gain.
type AudioDeviceSetGainRequest struct {
	GainDB float32 `json:"gain_db"`
}

// Validate enforces the same gain window as AudioDeviceRequest so the
// live-update path can't install a value the create/update path would
// reject.
func (r AudioDeviceSetGainRequest) Validate() error {
	if r.GainDB < -60 || r.GainDB > 12 {
		return fmt.Errorf("gain_db must be between -60 and +12")
	}
	return nil
}

// TestToneResponse is the body returned by POST /api/audio-devices/{id}/test-tone
// on success. Preserves the pre-typed `{"status":"ok"}` wire shape.
type TestToneResponse struct {
	Status string `json:"status"`
}

// AudioDeviceLevelsResponse is the body returned by
// GET /api/audio-devices/levels — a map from device id to the latest
// cached peak/rms/clipping measurement. Swag cannot render a keyed
// map[uint32]*T directly in a Swagger 2.0 definition, so the response
// is documented as {object} and the TypeScript client represents it as
// Record<string, modembridge.DeviceLevel> (JSON object keys are always
// strings on the wire).
type AudioDeviceLevelsResponse map[uint32]*modembridge.DeviceLevel
