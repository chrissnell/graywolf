package configstore

import "time"

// AudioDevice describes a single audio input source feeding the modem.
// SourceType selects how the Rust modem opens the device:
//   - "soundcard": cpal device by name (DeviceName is cpal name)
//   - "flac":      file playback (DeviceName/SourcePath is file path)
//   - "stdin":     raw s16le on stdin
//   - "sdr_udp":   SDR UDP stream (later phases)
type AudioDevice struct {
	ID         uint32 `gorm:"primaryKey;autoIncrement"`
	Name       string `gorm:"not null"`
	SourceType string `gorm:"not null"` // soundcard|flac|stdin|sdr_udp
	SourcePath string // cpal name or file path
	SampleRate uint32 `gorm:"not null;default:48000"`
	Channels   uint32 `gorm:"not null;default:1"`
	Format     string `gorm:"not null;default:'s16le'"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Channel is a logical radio channel tied to an audio device.
type Channel struct {
	ID            uint32 `gorm:"primaryKey;autoIncrement"`
	Name          string `gorm:"not null"`
	AudioDeviceID uint32 `gorm:"not null;index"`
	AudioChannel  uint32 `gorm:"not null;default:0"` // 0=left/mono, 1=right
	ModemType     string `gorm:"not null;default:'afsk'"`
	BitRate       uint32 `gorm:"not null;default:1200"`
	MarkFreq      uint32 `gorm:"not null;default:1200"`
	SpaceFreq     uint32 `gorm:"not null;default:2200"`
	Profile       string `gorm:"not null;default:'A'"`
	NumSlicers    uint32 `gorm:"not null;default:1"`
	FixBits       string `gorm:"not null;default:'none'"` // none|single|double
	TxDelayMs     uint32 `gorm:"not null;default:300"`
	TxTailMs      uint32 `gorm:"not null;default:100"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// PttConfig holds push-to-talk configuration for a channel.
type PttConfig struct {
	ID         uint32 `gorm:"primaryKey;autoIncrement"`
	ChannelID  uint32 `gorm:"not null;uniqueIndex"`
	Method     string `gorm:"not null;default:'none'"` // serial_rts|serial_dtr|gpio|cm108|none
	Device     string
	GpioPin    uint32
	SlotTimeMs uint32 `gorm:"not null;default:10"`
	Persist    uint32 `gorm:"not null;default:63"`
	DwaitMs    uint32 `gorm:"not null;default:0"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// WebAuth is a bcrypt-hashed credential for the future web UI.
type WebAuth struct {
	Username   string `gorm:"primaryKey"`
	BcryptHash string `gorm:"not null"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// WebSession is a bearer token issued after a successful login.
type WebSession struct {
	Token     string `gorm:"primaryKey"`
	Username  string `gorm:"not null;index"`
	ExpiresAt time.Time `gorm:"not null;index"`
	CreatedAt time.Time
}
