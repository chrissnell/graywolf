package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// PttRequest is the body accepted by POST /api/ptt and
// PUT /api/ptt/{channel}. The store upserts by channel_id.
type PttRequest struct {
	ChannelID  uint32 `json:"channel_id"`
	Method     string `json:"method"`
	DevicePath string `json:"device_path"`
	GpioPin    uint32 `json:"gpio_pin"`
	Invert     bool   `json:"invert"`
	SlotTimeMs uint32 `json:"slot_time_ms"`
	Persist    uint32 `json:"persist"`
	DwaitMs    uint32 `json:"dwait_ms"`
}

func (r PttRequest) Validate() error {
	if r.Method == "" {
		return fmt.Errorf("method is required")
	}
	return nil
}

func (r PttRequest) ToModel() configstore.PttConfig {
	return configstore.PttConfig{
		ChannelID:  r.ChannelID,
		Method:     r.Method,
		Device:     r.DevicePath,
		GpioPin:    r.GpioPin,
		Invert:     r.Invert,
		SlotTimeMs: r.SlotTimeMs,
		Persist:    r.Persist,
		DwaitMs:    r.DwaitMs,
	}
}

// ToUpdate maps an update request to a storage model, pinning the
// channel id from the URL instead of the body so path-wins semantics
// match the current handler.
func (r PttRequest) ToUpdate(channelID uint32) configstore.PttConfig {
	m := r.ToModel()
	m.ChannelID = channelID
	return m
}

// PttResponse is the body returned by GET/POST/PUT for a PTT config.
type PttResponse struct {
	ID uint32 `json:"id"`
	PttRequest
}

func PttFromModel(m configstore.PttConfig) PttResponse {
	return PttResponse{
		ID: m.ID,
		PttRequest: PttRequest{
			ChannelID:  m.ChannelID,
			Method:     m.Method,
			DevicePath: m.Device,
			GpioPin:    m.GpioPin,
			Invert:     m.Invert,
			SlotTimeMs: m.SlotTimeMs,
			Persist:    m.Persist,
			DwaitMs:    m.DwaitMs,
		},
	}
}

func PttsFromModels(ms []configstore.PttConfig) []PttResponse {
	out := make([]PttResponse, len(ms))
	for i, m := range ms {
		out[i] = PttFromModel(m)
	}
	return out
}
