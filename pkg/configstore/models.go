package configstore

import "time"

// AudioDevice describes a single audio input source feeding the modem.
// SourceType selects how the Rust modem opens the device:
//   - "soundcard": cpal device by name (DeviceName is cpal name)
//   - "flac":      file playback (DeviceName/SourcePath is file path)
//   - "stdin":     raw s16le on stdin
//   - "sdr_udp":   SDR UDP stream (later phases)
type AudioDevice struct {
	ID         uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name       string    `gorm:"not null" json:"name"`
	SourceType string    `gorm:"not null" json:"source_type"` // soundcard|flac|stdin|sdr_udp
	SourcePath string    `json:"device_path"`                 // cpal name or file path
	SampleRate uint32    `gorm:"not null;default:48000" json:"sample_rate"`
	Channels   uint32    `gorm:"not null;default:1" json:"channels"`
	Format     string    `gorm:"not null;default:'s16le'" json:"format"`
	CreatedAt  time.Time `json:"-"`
	UpdatedAt  time.Time `json:"-"`
}

// Channel is a logical radio channel tied to an audio device.
type Channel struct {
	ID            uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name          string    `gorm:"not null" json:"name"`
	AudioDeviceID uint32    `gorm:"not null;index" json:"audio_device_id"`
	AudioChannel  uint32    `gorm:"not null;default:0" json:"audio_channel"` // 0=left/mono, 1=right
	ModemType     string    `gorm:"not null;default:'afsk'" json:"modem_type"`
	BitRate       uint32    `gorm:"not null;default:1200" json:"bit_rate"`
	MarkFreq      uint32    `gorm:"not null;default:1200" json:"mark_freq"`
	SpaceFreq     uint32    `gorm:"not null;default:2200" json:"space_freq"`
	Profile       string    `gorm:"not null;default:'A'" json:"profile"`
	NumSlicers    uint32    `gorm:"not null;default:1" json:"num_slicers"`
	FixBits       string    `gorm:"not null;default:'none'" json:"fix_bits"` // none|single|double
	FX25Encode    bool      `gorm:"not null;default:false" json:"fx25_encode"`
	IL2PEncode    bool      `gorm:"column:il2p_encode;not null;default:false" json:"il2p_encode"`
	NumDecoders   uint32    `gorm:"not null;default:1" json:"num_decoders"`
	DecoderOffset int32     `gorm:"not null;default:0" json:"decoder_offset"`
	TxDelayMs     uint32    `gorm:"not null;default:300" json:"tx_delay_ms"`
	TxTailMs      uint32    `gorm:"not null;default:100" json:"tx_tail_ms"`
	CreatedAt     time.Time `json:"-"`
	UpdatedAt     time.Time `json:"-"`
}

// PttConfig holds push-to-talk configuration for a channel.
type PttConfig struct {
	ID         uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	ChannelID  uint32    `gorm:"not null;uniqueIndex" json:"channel_id"`
	Method     string    `gorm:"not null;default:'none'" json:"method"` // serial_rts|serial_dtr|gpio|cm108|none
	Device     string    `json:"device_path"`
	GpioPin    uint32    `json:"gpio_pin"`
	SlotTimeMs uint32    `gorm:"not null;default:10" json:"slot_time_ms"`
	Persist    uint32    `gorm:"not null;default:63" json:"persist"`
	DwaitMs    uint32    `gorm:"not null;default:0" json:"dwait_ms"`
	CreatedAt  time.Time `json:"-"`
	UpdatedAt  time.Time `json:"-"`
}
