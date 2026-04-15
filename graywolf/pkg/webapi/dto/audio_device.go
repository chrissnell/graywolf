package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
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
