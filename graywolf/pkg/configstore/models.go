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
	Direction  string    `gorm:"not null;default:'input'" json:"direction"` // input|output
	SourceType string    `gorm:"not null" json:"source_type"`               // soundcard|flac|stdin|sdr_udp
	SourcePath string    `json:"device_path"`                               // cpal name or file path
	SampleRate uint32    `gorm:"not null;default:48000" json:"sample_rate"`
	Channels   uint32    `gorm:"not null;default:1" json:"channels"`
	Format     string    `gorm:"not null;default:'s16le'" json:"format"`
	GainDB     float32   `gorm:"not null;default:0" json:"gain_db"` // software gain: -60 to +12 dB, 0 = unity
	CreatedAt  time.Time `json:"-"`
	UpdatedAt  time.Time `json:"-"`
}

// Channel is a logical radio channel tied to an audio device.
//
// Foreign-key policy:
//   - InputDeviceID is a hard FK to AudioDevice.ID with OnDelete:RESTRICT:
//     channels must have an input device, so deleting a device that
//     still has referencing channels fails at the SQL layer unless the
//     caller goes through DeleteAudioDeviceChecked(cascade=true), which
//     removes the channels first. The RESTRICT constraint is the
//     backstop if anything tries to delete an audio device by other
//     means.
//   - OutputDeviceID is a *soft* FK, not enforced by SQLite. The column
//     is a plain uint32 where 0 means "RX-only" (no output device).
//     SQLite FK constraints treat any non-NULL value as a reference, so
//     a stored 0 would fail the constraint, and making the column
//     nullable would ripple through DTOs and protobuf mappings for no
//     gain. The relation is validated at the application layer in
//     validateChannel, and DeleteAudioDeviceChecked walks both input
//     and output references.
type Channel struct {
	ID             uint32       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name           string       `gorm:"not null" json:"name"`
	InputDeviceID  uint32       `gorm:"not null;index" json:"input_device_id"`
	InputDevice    *AudioDevice `gorm:"foreignKey:InputDeviceID;references:ID;constraint:OnDelete:RESTRICT,OnUpdate:RESTRICT" json:"-"`
	InputChannel   uint32       `gorm:"not null;default:0" json:"input_channel"`          // 0=left/mono, 1=right
	OutputDeviceID uint32       `gorm:"not null;default:0;index" json:"output_device_id"` // 0=RX-only; soft FK, see type comment
	OutputChannel  uint32       `gorm:"not null;default:0" json:"output_channel"`
	ModemType      string       `gorm:"not null;default:'afsk'" json:"modem_type"`
	BitRate        uint32       `gorm:"not null;default:1200" json:"bit_rate"`
	MarkFreq       uint32       `gorm:"not null;default:1200" json:"mark_freq"`
	SpaceFreq      uint32       `gorm:"not null;default:2200" json:"space_freq"`
	Profile        string       `gorm:"not null;default:'A'" json:"profile"`
	NumSlicers     uint32       `gorm:"not null;default:1" json:"num_slicers"`
	FixBits        string       `gorm:"not null;default:'none'" json:"fix_bits"` // none|single|double
	FX25Encode     bool         `gorm:"not null;default:false" json:"fx25_encode"`
	IL2PEncode     bool         `gorm:"column:il2p_encode;not null;default:false" json:"il2p_encode"`
	NumDecoders    uint32       `gorm:"not null;default:1" json:"num_decoders"`
	DecoderOffset  int32        `gorm:"not null;default:0" json:"decoder_offset"`
	TxDelayMs      uint32       `gorm:"not null;default:300" json:"tx_delay_ms"`
	TxTailMs       uint32       `gorm:"not null;default:100" json:"tx_tail_ms"`
	CreatedAt      time.Time    `json:"-"`
	UpdatedAt      time.Time    `json:"-"`
}

// PttConfig holds push-to-talk configuration for a channel. ChannelID
// is a hard FK to Channel.ID with OnDelete:CASCADE: PTT settings have
// no meaning without the channel they belong to, and the uniqueIndex
// on ChannelID guarantees one row per channel.
type PttConfig struct {
	ID         uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	ChannelID  uint32    `gorm:"not null;uniqueIndex" json:"channel_id"`
	Channel    *Channel  `gorm:"foreignKey:ChannelID;references:ID;constraint:OnDelete:CASCADE,OnUpdate:CASCADE" json:"-"`
	Method     string    `gorm:"not null;default:'none'" json:"method"` // serial_rts|serial_dtr|gpio|cm108|none
	Device     string    `json:"device_path"`
	GpioPin    uint32    `json:"gpio_pin"`
	Invert     bool      `gorm:"not null;default:false" json:"invert"` // reverse polarity for rigs wired backwards
	SlotTimeMs uint32    `gorm:"not null;default:10" json:"slot_time_ms"`
	Persist    uint32    `gorm:"not null;default:63" json:"persist"`
	DwaitMs    uint32    `gorm:"not null;default:0" json:"dwait_ms"`
	CreatedAt  time.Time `json:"-"`
	UpdatedAt  time.Time `json:"-"`
}

// KissInterface represents one row in kiss_interfaces. Each Server in
// pkg/kiss corresponds to one row. InterfaceType is "tcp"|"serial"|
// "bluetooth"; for serial/bluetooth the Device and BaudRate are used
// and ListenAddr may be empty.
type KissInterface struct {
	ID            uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Name          string    `gorm:"not null;uniqueIndex" json:"name"`
	InterfaceType string    `gorm:"not null;default:'tcp'" json:"type"` // tcp|serial|bluetooth
	ListenAddr    string    `json:"listen_addr"`                        // host:port for tcp
	Device        string    `json:"serial_device"`                      // /dev/ttyUSB0 or bluetooth mac
	BaudRate      uint32    `gorm:"default:9600" json:"baud_rate"`
	Channel       uint32    `gorm:"not null;default:1" json:"channel"` // default radio channel for this interface
	Broadcast     bool      `gorm:"not null;default:true" json:"broadcast"`
	Enabled       bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt     time.Time `json:"-"`
	UpdatedAt     time.Time `json:"-"`
}

// AgwConfig is a singleton (id=1) row describing the AGWPE listener.
type AgwConfig struct {
	ID         uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	ListenAddr string    `gorm:"not null;default:'0.0.0.0:8000'" json:"listen_addr"`
	Callsigns  string    `gorm:"not null;default:'N0CALL'" json:"callsigns"` // CSV; one per AGW port
	Enabled    bool      `gorm:"not null;default:false" json:"enabled"`
	CreatedAt  time.Time `json:"-"`
	UpdatedAt  time.Time `json:"-"`
}

// TxTiming holds per-channel CSMA parameters. Mirrors
// txgovernor.ChannelTiming.
type TxTiming struct {
	ID        uint32 `gorm:"primaryKey;autoIncrement" json:"id"`
	Channel   uint32 `gorm:"not null;uniqueIndex" json:"channel"`
	TxDelayMs uint32 `gorm:"not null;default:300" json:"tx_delay_ms"`
	TxTailMs  uint32 `gorm:"not null;default:100" json:"tx_tail_ms"`
	SlotMs    uint32 `gorm:"not null;default:100" json:"slot_ms"`
	Persist   uint32 `gorm:"not null;default:63" json:"persist"`
	FullDup   bool   `gorm:"not null;default:false" json:"full_dup"`
	// Rate limits; 0 = unlimited.
	Rate1Min  uint32    `gorm:"not null;default:0" json:"rate_1min"`
	Rate5Min  uint32    `gorm:"not null;default:0" json:"rate_5min"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

// DigipeaterConfig is a singleton (id=1) row with global digipeater
// settings.
type DigipeaterConfig struct {
	ID                  uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Enabled             bool      `gorm:"not null;default:false" json:"enabled"`
	DedupeWindowSeconds uint32    `gorm:"not null;default:30" json:"dedupe_window_seconds"`
	MyCall              string    `gorm:"not null;default:'N0CALL'" json:"my_call"` // local callsign used for preemptive digi
	CreatedAt           time.Time `json:"-"`
	UpdatedAt           time.Time `json:"-"`
}

// DigipeaterRule is one per-channel digipeater alias/rule. The digi
// engine walks rules in Priority ascending order looking for a match
// against an unconsumed path entry.
//
// Action enumeration:
//
//	"repeat"   — retransmit on ToChannel, consume this alias slot
//	"drop"     — match and suppress (filter-only rule)
//
// AliasType enumeration:
//
//	"widen"    — WIDEn-N style (Alias is the base e.g. "WIDE"; consumes 1 hop, decrements SSID)
//	"exact"    — exact callsign match (Alias is full "CALL[-SSID]"); e.g. the local callsign (preemptive)
//	"trace"    — TRACEn-N behaves like WIDEn-N but also inserts the local callsign before the alias
type DigipeaterRule struct {
	ID          uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	FromChannel uint32    `gorm:"not null;index" json:"from_channel"`
	ToChannel   uint32    `gorm:"not null" json:"to_channel"`
	Alias       string    `gorm:"not null" json:"alias"`
	AliasType   string    `gorm:"not null;default:'widen'" json:"alias_type"` // widen|exact|trace
	MaxHops     uint32    `gorm:"not null;default:2" json:"max_hops"`         // maximum N-N accepted (e.g. WIDE2-2)
	Action      string    `gorm:"not null;default:'repeat'" json:"action"`
	Priority    uint32    `gorm:"not null;default:100" json:"priority"` // lower = evaluated first
	Enabled     bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt   time.Time `json:"-"`
	UpdatedAt   time.Time `json:"-"`
}

// IGateConfig is a singleton (id=1) row for the iGate.
type IGateConfig struct {
	ID              uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Enabled         bool      `gorm:"not null;default:false" json:"enabled"`
	Server          string    `gorm:"not null;default:'rotate.aprs2.net'" json:"server"`
	Port            uint32    `gorm:"not null;default:14580" json:"port"`
	Callsign        string    `gorm:"not null;default:'N0CALL'" json:"callsign"`
	Passcode        string    `gorm:"not null;default:'-1'" json:"passcode"` // string to tolerate "-1"
	ServerFilter    string    `json:"server_filter"`                         // APRS-IS server-side filter expression
	SimulationMode  bool      `gorm:"not null;default:false" json:"simulation_mode"`
	GateRfToIs      bool      `gorm:"not null;default:true" json:"gate_rf_to_is"`
	GateIsToRf      bool      `gorm:"not null;default:false" json:"gate_is_to_rf"`
	RfChannel       uint32    `gorm:"not null;default:1" json:"rf_channel"`             // channel used when gating IS->RF
	MaxMsgHops      uint32    `gorm:"not null;default:2" json:"max_msg_hops"`           // WIDE hops for IS->RF messages
	SoftwareName    string    `gorm:"not null;default:'graywolf'" json:"software_name"` // APRS-IS login banner software name
	SoftwareVersion string    `gorm:"not null;default:'0.1'" json:"software_version"`   // APRS-IS login banner version
	TxChannel       uint32    `gorm:"not null;default:1" json:"tx_channel"`             // radio channel for IS->RF submissions
	CreatedAt       time.Time `json:"-"`
	UpdatedAt       time.Time `json:"-"`
}

// IGateRfFilter is a per-channel allow/deny rule used to decide which
// RF-originated packets are forwarded to APRS-IS. Evaluation: lowest
// Priority first (ascending order); first match determines action.
type IGateRfFilter struct {
	ID        uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Channel   uint32    `gorm:"not null;index" json:"channel"`
	Type      string    `gorm:"not null" json:"type"` // callsign|prefix|message_dest|object
	Pattern   string    `gorm:"not null" json:"pattern"`
	Action    string    `gorm:"not null;default:'allow'" json:"action"` // allow|deny
	Priority  uint32    `gorm:"not null;default:100" json:"priority"`
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

// GPSConfig is a singleton (id=1) row for the GPS receiver.
type GPSConfig struct {
	ID         uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	SourceType string    `gorm:"not null;default:'none'" json:"source"` // none|serial|gpsd
	Device     string    `json:"serial_port"`                           // serial device path, e.g. /dev/ttyUSB0
	BaudRate   uint32    `gorm:"not null;default:4800" json:"baud_rate"`
	GpsdHost   string    `gorm:"not null;default:'localhost'" json:"gpsd_host"`
	GpsdPort   uint32    `gorm:"not null;default:2947" json:"gpsd_port"`
	Enabled    bool      `gorm:"not null;default:false" json:"enabled"`
	CreatedAt  time.Time `json:"-"`
	UpdatedAt  time.Time `json:"-"`
}

// Beacon is a scheduled beacon. Type selects the payload builder.
type Beacon struct {
	ID            uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Type          string    `gorm:"not null;default:'position'" json:"type"` // position|object|tracker|custom|igate
	Channel       uint32    `gorm:"not null;default:1" json:"channel"`
	Callsign      string    `gorm:"not null" json:"callsign"`
	Destination   string    `gorm:"not null;default:'APGRWF'" json:"destination"`
	Path          string    `gorm:"not null;default:'WIDE1-1'" json:"path"`
	UseGps        bool      `gorm:"column:use_gps;default:false" json:"use_gps"` // source lat/lon/alt from GPS cache instead of fixed fields
	Latitude      float64   `json:"latitude"`
	Longitude     float64   `json:"longitude"`
	AltFt         float64   `json:"alt_ft"` // altitude in feet for position reports
	Ambiguity     uint32    `gorm:"not null;default:0" json:"ambiguity"`
	SymbolTable   string    `gorm:"not null;default:'/'" json:"symbol_table"`
	Symbol        string    `gorm:"not null;default:'-'" json:"symbol"`
	Overlay       string    `json:"overlay"`                                 // alternate symbol table overlay character
	Compress      bool      `gorm:"not null;default:true" json:"compress"`   // use 13-byte base-91 compressed position encoding (APRS101 ch 9)
	Messaging     bool      `gorm:"not null;default:false" json:"messaging"` // '=' instead of '!' prefix
	Comment       string    `json:"comment"`
	CommentCmd    string    `json:"comment_cmd"`                      // shell command whose stdout is appended as comment
	CustomInfo    string    `json:"custom_info"`                      // raw info field override for Type=="custom"
	ObjectName    string    `json:"object_name"`                      // for Type=="object"
	Power         uint32    `gorm:"not null;default:0" json:"power"`  // watts for PHG
	Height        uint32    `gorm:"not null;default:0" json:"height"` // feet HAAT for PHG
	Gain          uint32    `gorm:"not null;default:0" json:"gain"`   // dBi for PHG
	Dir           uint32    `gorm:"not null;default:0" json:"dir"`    // antenna direction 0..8 for PHG
	Freq          string    `json:"freq"`                             // frequency string for freq info
	Tone          string    `json:"tone"`                             // CTCSS/DCS tone
	FreqOffset    string    `json:"freq_offset"`                      // repeater offset
	DelaySeconds  uint32    `gorm:"not null;default:30" json:"delay_seconds"`
	EverySeconds  uint32    `gorm:"not null;default:1800" json:"interval"`
	SlotSeconds   int32     `gorm:"not null;default:-1" json:"slot_seconds"`
	SmartBeacon   bool      `gorm:"not null;default:false" json:"smart_beacon"`
	SbFastSpeed   uint32    `gorm:"default:60" json:"sb_fast_speed"`
	SbSlowSpeed   uint32    `gorm:"default:5" json:"sb_slow_speed"`
	SbFastRate    uint32    `gorm:"default:60" json:"sb_fast_rate"`
	SbSlowRate    uint32    `gorm:"default:1800" json:"sb_slow_rate"`
	SbTurnAngle   uint32    `gorm:"default:30" json:"sb_turn_angle"`
	SbTurnSlope   uint32    `gorm:"default:255" json:"sb_turn_slope"`
	SbMinTurnTime uint32    `gorm:"default:5" json:"sb_min_turn_time"`
	SendToAPRSIS  bool      `gorm:"column:send_to_aprs_is;not null;default:false" json:"send_to_aprs_is"`
	Enabled       bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt     time.Time `json:"-"`
	UpdatedAt     time.Time `json:"-"`
}

// PacketFilter is a reserved stub table for future per-channel packet
// filters (Phase 5/6).
type PacketFilter struct {
	ID        uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Channel   uint32    `gorm:"not null;index" json:"channel"`
	Name      string    `gorm:"not null" json:"name"`
	Expr      string    `gorm:"not null" json:"expr"`
	Action    string    `gorm:"not null;default:'allow'" json:"action"`
	Enabled   bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}
