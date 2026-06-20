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
	// GpioPin is the CM108 HID GPIO pin number (1-indexed, default 3).
	// cm108 method only. The `gpio` method uses `gpio_line` instead.
	GpioPin uint32 `json:"gpio_pin"`
	// PttMethod is the Android USB-PTT transport (PttMethod enum 1..4)
	// when Method=="android". Not used by desktop methods.
	PttMethod uint32 `json:"ptt_method"`
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
	// channel_id is the upsert key; 0 has no corresponding Channel row
	// (FK would fail anyway). Reject up front so the rekey branch can't
	// be tricked into a same-channel coalesce by a missing/zero body
	// field.
	if r.ChannelID == 0 {
		return fmt.Errorf("channel_id is required")
	}
	// android method requires ptt_method in 1..5 (spec Appendix B):
	//   1 = CP2102N_RTS, 2 = CM108_HID, 3 = AIOC_CDC_DTR, 4 = VOX,
	//   5 = DIGIRIG_TONE
	if r.Method == "android" {
		switch r.PttMethod {
		case 1, 2, 3, 4, 5:
			// valid
		default:
			return fmt.Errorf("android ptt method requires ptt_method in 1..5 (spec Appendix B), got %d", r.PttMethod)
		}
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
		PttMethod:  r.PttMethod,
	}
}

// ToUpdate maps an update request to a storage model with the URL's
// channel id forced into the result. Used on the same-channel branch
// of updatePttConfig (where body.channel_id == URL channel_id, or the
// body omitted channel_id and the URL fills it in). The cross-channel
// rekey branch calls ToModel directly so the body's channel_id wins.
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
			PttMethod:  m.PttMethod,
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
