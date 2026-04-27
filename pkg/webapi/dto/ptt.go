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
	// GpioPin is the CM108 HID GPIO pin number (1-indexed, default 3). Not used
	// by the `gpio` method, which references `gpio_line` instead to avoid
	// indexing ambiguity between CM108 pin numbers and gpiochip line offsets.
	GpioPin uint32 `json:"gpio_pin"`
	// GpioLine is the gpiochip v2 line offset (0-indexed) used by the `gpio`
	// method. Ignored for every other method.
	GpioLine   uint32 `json:"gpio_line"`
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
		GpioLine:   r.GpioLine,
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
			GpioLine:   m.GpioLine,
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

// TestRigctldRequest is the body accepted by POST /api/ptt/test-rigctld.
// The handler opens a short-lived TCP connection to the given rigctld
// endpoint and sends a non-disruptive `t` (get_ptt) probe.
type TestRigctldRequest struct {
	Host string `json:"host"`
	Port uint16 `json:"port"`
}

// TestRigctldResponse reports the outcome of a rigctld probe. OK is the
// single source of truth — clients must not infer success from HTTP
// status. Message is a human-readable diagnostic; LatencyMs is populated
// only on success and measures wall-clock from dial start to RPRT 0.
type TestRigctldResponse struct {
	OK        bool   `json:"ok"`
	Message   string `json:"message"`
	LatencyMs int64  `json:"latency_ms"`
}
