package modembridge

// ChannelStats holds per-channel statistics sourced from StatusUpdate messages.
type ChannelStats struct {
	Channel         uint32  `json:"channel"`
	RxFrames        uint64  `json:"rx_frames"`
	RxBadFCS        uint64  `json:"rx_bad_fcs"`
	TxFrames        uint64  `json:"tx_frames"`
	DcdTransitions  uint64  `json:"dcd_transitions"`
	AudioLevelMark  float32 `json:"audio_level_mark"`
	AudioLevelSpace float32 `json:"audio_level_space"`
	AudioLevelPeak  float32 `json:"audio_level_peak"`
	DcdState        bool    `json:"dcd_state"`
}

// DeviceLevel holds the latest per-device audio level from the modem.
type DeviceLevel struct {
	DeviceID uint32  `json:"device_id"`
	PeakDBFS float32 `json:"peak_dbfs"`
	RmsDBFS  float32 `json:"rms_dbfs"`
	Clipping bool    `json:"clipping"`
}

// AvailableDevice describes an audio device discovered by cpal enumeration.
// Field names match the frontend's expected shape.
type AvailableDevice struct {
	Name        string   `json:"name"`
	Description string   `json:"description"` // human-friendly name (e.g. USB product string)
	Path        string   `json:"path"`        // cpal device name (used as device_path)
	SampleRates []uint32 `json:"sample_rates"`
	Channels    []uint32 `json:"channels"`
	HostAPI     string   `json:"host_api"`
	IsDefault   bool     `json:"is_default"`
	IsInput     bool     `json:"is_input"`
}

// InputLevel holds the level scan result for a single input device.
type InputLevel struct {
	Name      string  `json:"name"`
	PeakDBFS  float32 `json:"peak_dbfs"`
	HasSignal bool    `json:"has_signal"`
	Error     string  `json:"error,omitempty"`
}
