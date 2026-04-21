package configstore

import (
	"strings"
	"time"

	"gorm.io/gorm"
)

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

// Channel is a logical radio channel optionally tied to an audio device.
//
// Foreign-key policy:
//   - InputDeviceID is a *pointer-typed soft FK* to AudioDevice.ID:
//     a nil value means "KISS-only channel — no modem, no audio".
//     When non-nil, the value must reference an existing input-direction
//     device (enforced at the application layer in validateChannel).
//     Phase 2 migrated the column from NOT NULL to NULL to allow
//     channels that are serviced only by a KISS TNC interface.
//     DeleteAudioDeviceChecked still walks both input and output
//     references to refuse / cascade a device delete that would
//     orphan channels.
//   - OutputDeviceID is a *soft* FK, not enforced by SQLite. The column
//     is a plain uint32 where 0 means "RX-only" (no output device).
//     SQLite FK constraints treat any non-NULL value as a reference, so
//     a stored 0 would fail the constraint, and making the column
//     nullable would ripple through DTOs and protobuf mappings for no
//     gain. The relation is validated at the application layer in
//     validateChannel, and DeleteAudioDeviceChecked walks both input
//     and output references.
//
// When InputDeviceID is nil, ModemType / BitRate / MarkFreq / SpaceFreq /
// Profile / NumSlicers / FixBits / FX25Encode / IL2PEncode / NumDecoders
// / DecoderOffset are stored unchanged but effectively unused: the modem
// subprocess is never told about the channel (see
// pkg/modembridge/session.go pushConfiguration, which skips nil-input
// channels). They round-trip through the UI so a future Convert flow
// can flip a channel back to modem-backed without losing the operator's
// last known values.
type Channel struct {
	ID             uint32       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name           string       `gorm:"not null" json:"name"`
	InputDeviceID  *uint32      `gorm:"index" json:"input_device_id"`
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
	GpioPin    uint32    `json:"gpio_pin"`                             // CM108-only: 1-indexed HID GPIO pin (default 3)
	GpioLine   uint32    `gorm:"not null;default:0" json:"gpio_line"`  // gpiochip method: 0-indexed line offset
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
//
// Mode selects the per-interface routing policy:
//   - KissModeModem (default): peer is an APRS app; frames it sends are
//     queued for RF transmission.
//   - KissModeTnc: peer is a hardware TNC supplying off-air RX; frames are
//     fanned out to digi/igate/messages/station cache, not auto-submitted
//     to TX. See .context/2026-04-19-kiss-modem-tnc-mode.md.
//
// TncIngressRateHz and TncIngressBurst configure the per-interface
// token-bucket ingress cap consumed in TNC mode (wired in Phase 3). The
// fields are stored and surfaced for every row regardless of mode so the
// operator's choice survives a mode flip.
type KissInterface struct {
	ID               uint32 `gorm:"primaryKey;autoIncrement" json:"id"`
	Name             string `gorm:"not null;uniqueIndex" json:"name"`
	InterfaceType    string `gorm:"not null;default:'tcp'" json:"type"` // tcp|tcp-client|serial|bluetooth
	ListenAddr       string `json:"listen_addr"`                        // host:port for tcp (server-listen)
	Device           string `json:"serial_device"`                      // /dev/ttyUSB0 or bluetooth mac
	BaudRate         uint32 `gorm:"default:9600" json:"baud_rate"`
	Channel          uint32 `gorm:"not null;default:1" json:"channel"` // default radio channel for this interface
	Broadcast        bool   `gorm:"not null;default:true" json:"broadcast"`
	Enabled          bool   `gorm:"not null;default:true" json:"enabled"`
	Mode             string `gorm:"not null;default:'modem'" json:"mode"`           // modem|tnc
	TncIngressRateHz uint32 `gorm:"not null;default:50" json:"tnc_ingress_rate_hz"` // token-bucket refill, frames/sec
	TncIngressBurst  uint32 `gorm:"not null;default:100" json:"tnc_ingress_burst"`  // token-bucket size
	// InterfaceType == "tcp-client" uses RemoteHost / RemotePort as the
	// dial target and ReconnectInitMs / ReconnectMaxMs to size the
	// supervisor's backoff schedule. ListenAddr is ignored on tcp-client
	// rows. Unused / zero on all other interface types; see Phase 4 in
	// .context/2026-04-20-kiss-tcp-client-and-channel-backing.md.
	RemoteHost      string `gorm:"column:remote_host;not null;default:''" json:"remote_host"`
	RemotePort      uint16 `gorm:"column:remote_port;not null;default:0" json:"remote_port"`
	ReconnectInitMs uint32 `gorm:"column:reconnect_init_ms;not null;default:1000" json:"reconnect_init_ms"`
	ReconnectMaxMs  uint32 `gorm:"column:reconnect_max_ms;not null;default:300000" json:"reconnect_max_ms"`
	// AllowTxFromGovernor: when true (and Mode == KissModeTnc), this
	// interface is registered as a KissTnc TX backend and the
	// dispatcher fan-outs governor-scheduled frames (beacon / digi /
	// iGate IS→RF / KISS / AGW submissions) for this channel to it.
	// Default false so existing TNC-mode rows that users configured
	// before Phase 3 do NOT silently start transmitting. Phase 4 sets
	// the DTO default to true for newly-created tcp-client rows only.
	// Modem-mode rows ignore this flag entirely (they TX via Submit,
	// they don't receive TX from the governor).
	AllowTxFromGovernor bool `gorm:"column:allow_tx_from_governor;not null;default:false" json:"allow_tx_from_governor"`
	// NeedsReconfig is set to true when a referential cascade (Phase 5)
	// nulls this row's Channel. Phase 3 merely declares the column so
	// the shape is stable before the cascade logic lands; no code reads
	// it yet.
	NeedsReconfig bool      `gorm:"column:needs_reconfig;not null;default:false" json:"needs_reconfig"`
	CreatedAt     time.Time `json:"-"`
	UpdatedAt     time.Time `json:"-"`
}

// KISS interface mode values. Stored lowercase and matched exactly — see
// ValidKissMode. The default for newly created rows is KissModeModem so
// existing behavior is preserved byte-for-byte.
const (
	KissModeModem = "modem"
	KissModeTnc   = "tnc"
)

// Defaults for KissInterface.TncIngressRateHz / TncIngressBurst. Kept in
// sync with the GORM struct-tag defaults on KissInterface. Go callers
// should reference these constants rather than hard-coding 50/100 so
// the two sides of the model can't drift.
const (
	DefaultTncIngressRateHz uint32 = 50
	DefaultTncIngressBurst  uint32 = 100
)

// ValidKissMode reports whether m is an accepted KissInterface.Mode
// value. The match is case-sensitive and the empty string is rejected:
// callers that want the "absent field" default to land on KissModeModem
// must substitute it themselves before calling this helper.
func ValidKissMode(m string) bool {
	return m == KissModeModem || m == KissModeTnc
}

// KISS interface transport types. Kept lowercase and matched exactly
// via ValidKissInterfaceType. "tcp" is the server-listen (inbound)
// transport — graywolf binds ListenAddr and accepts multiple clients.
// "tcp-client" is the outbound dial (Phase 4) — graywolf connects to a
// remote KISS TNC at RemoteHost:RemotePort and maintains a single
// supervised connection with exponential backoff + jitter.
const (
	KissTypeTCP       = "tcp"
	KissTypeTCPClient = "tcp-client"
	KissTypeSerial    = "serial"
	KissTypeBluetooth = "bluetooth"
)

// ValidKissInterfaceType reports whether t is an accepted
// KissInterface.InterfaceType value. "tcp-client" was added in Phase 4
// of the KISS TCP-client + channel-backing plan.
func ValidKissInterfaceType(t string) bool {
	switch t {
	case KissTypeTCP, KissTypeTCPClient, KissTypeSerial, KissTypeBluetooth:
		return true
	}
	return false
}

// U32Ptr returns a pointer to a copy of v. Small helper for call sites
// (tests, DTO mappers, fixtures) that need to set Channel.InputDeviceID
// — a *uint32 after the Phase 2 nullable migration — from a literal or
// a uint32 local. Keeps the common case a one-liner without the
// "declare local, take address" dance.
func U32Ptr(v uint32) *uint32 { return &v }

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
//
// Callsign and Passcode columns remain in the DB for forward-safety on
// downgrade, but are no longer read/written by application code.
// See .context/2026-04-21-centralized-station-callsign.md §D4.
type IGateConfig struct {
	ID              uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Enabled         bool      `gorm:"not null;default:false" json:"enabled"`
	Server          string    `gorm:"not null;default:'rotate.aprs2.net'" json:"server"`
	Port            uint32    `gorm:"not null;default:14580" json:"port"`
	ServerFilter    string    `json:"server_filter"` // APRS-IS server-side filter expression
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

// StationConfig is a singleton (id=1) row holding the station-wide
// APRS callsign. This is the single source of truth for the callsign
// used by the iGate (APRS-IS login + passcode), the digipeater (unless
// overridden), beacons (unless overridden), and APRS messaging. See
// .context/2026-04-21-centralized-station-callsign.md.
type StationConfig struct {
	ID        uint32    `gorm:"primaryKey;autoIncrement" json:"id"`
	Callsign  string    `gorm:"not null;default:''" json:"callsign"`
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
	ID           uint32  `gorm:"primaryKey;autoIncrement" json:"id"`
	Type         string  `gorm:"not null;default:'position'" json:"type"` // position|object|tracker|custom|igate
	Channel      uint32  `gorm:"not null;default:1" json:"channel"`
	Callsign     string  `gorm:"not null" json:"callsign"`
	Destination  string  `gorm:"not null;default:'APGRWO'" json:"destination"`
	Path         string  `gorm:"not null;default:'WIDE1-1'" json:"path"`
	UseGps       bool    `gorm:"column:use_gps;default:false" json:"use_gps"` // source lat/lon/alt from GPS cache instead of fixed fields
	Latitude     float64 `json:"latitude"`
	Longitude    float64 `json:"longitude"`
	AltFt        float64 `json:"alt_ft"` // altitude in feet for position reports
	Ambiguity    uint32  `gorm:"not null;default:0" json:"ambiguity"`
	SymbolTable  string  `gorm:"not null;default:'/'" json:"symbol_table"`
	Symbol       string  `gorm:"not null;default:'-'" json:"symbol"`
	Overlay      string  `json:"overlay"`                                 // alternate symbol table overlay character
	Compress     bool    `gorm:"not null;default:true" json:"compress"`   // use 13-byte base-91 compressed position encoding (APRS101 ch 9)
	Messaging    bool    `gorm:"not null;default:false" json:"messaging"` // '=' instead of '!' prefix
	Comment      string  `json:"comment"`
	CommentCmd   string  `json:"comment_cmd"`                      // shell command whose stdout is appended as comment
	CustomInfo   string  `json:"custom_info"`                      // raw info field override for Type=="custom"
	ObjectName   string  `json:"object_name"`                      // for Type=="object"
	Power        uint32  `gorm:"not null;default:0" json:"power"`  // watts for PHG
	Height       uint32  `gorm:"not null;default:0" json:"height"` // feet HAAT for PHG
	Gain         uint32  `gorm:"not null;default:0" json:"gain"`   // dBi for PHG
	Dir          uint32  `gorm:"not null;default:0" json:"dir"`    // antenna direction 0..8 for PHG
	Freq         string  `json:"freq"`                             // frequency string for freq info
	Tone         string  `json:"tone"`                             // CTCSS/DCS tone
	FreqOffset   string  `json:"freq_offset"`                      // repeater offset
	DelaySeconds uint32  `gorm:"not null;default:30" json:"delay_seconds"`
	EverySeconds uint32  `gorm:"not null;default:1800" json:"interval"`
	SlotSeconds  int32   `gorm:"not null;default:-1" json:"slot_seconds"`
	SmartBeacon  bool    `gorm:"not null;default:false" json:"smart_beacon"`
	// Deprecated: use the global configstore.SmartBeaconConfig instead.
	// This column is no longer read as of 2026-04-18 (the SmartBeacon
	// curve is now a global singleton, matching direwolf). The column
	// will be dropped in a future migration once all deployments have
	// moved to the global config. See
	// .context/2026-04-18-smart-beacon-implementation.md.
	SbFastSpeed uint32 `gorm:"default:60" json:"sb_fast_speed"`
	// Deprecated: use the global configstore.SmartBeaconConfig instead.
	// This column is no longer read as of 2026-04-18 (the SmartBeacon
	// curve is now a global singleton, matching direwolf). The column
	// will be dropped in a future migration once all deployments have
	// moved to the global config. See
	// .context/2026-04-18-smart-beacon-implementation.md.
	SbSlowSpeed uint32 `gorm:"default:5" json:"sb_slow_speed"`
	// Deprecated: use the global configstore.SmartBeaconConfig instead.
	// This column is no longer read as of 2026-04-18 (the SmartBeacon
	// curve is now a global singleton, matching direwolf). The column
	// will be dropped in a future migration once all deployments have
	// moved to the global config. See
	// .context/2026-04-18-smart-beacon-implementation.md.
	SbFastRate uint32 `gorm:"default:60" json:"sb_fast_rate"`
	// Deprecated: use the global configstore.SmartBeaconConfig instead.
	// This column is no longer read as of 2026-04-18 (the SmartBeacon
	// curve is now a global singleton, matching direwolf). The column
	// will be dropped in a future migration once all deployments have
	// moved to the global config. See
	// .context/2026-04-18-smart-beacon-implementation.md.
	SbSlowRate uint32 `gorm:"default:1800" json:"sb_slow_rate"`
	// Deprecated: use the global configstore.SmartBeaconConfig instead.
	// This column is no longer read as of 2026-04-18 (the SmartBeacon
	// curve is now a global singleton, matching direwolf). The column
	// will be dropped in a future migration once all deployments have
	// moved to the global config. See
	// .context/2026-04-18-smart-beacon-implementation.md.
	SbTurnAngle uint32 `gorm:"default:30" json:"sb_turn_angle"`
	// Deprecated: use the global configstore.SmartBeaconConfig instead.
	// This column is no longer read as of 2026-04-18 (the SmartBeacon
	// curve is now a global singleton, matching direwolf). The column
	// will be dropped in a future migration once all deployments have
	// moved to the global config. See
	// .context/2026-04-18-smart-beacon-implementation.md.
	SbTurnSlope uint32 `gorm:"default:255" json:"sb_turn_slope"`
	// Deprecated: use the global configstore.SmartBeaconConfig instead.
	// This column is no longer read as of 2026-04-18 (the SmartBeacon
	// curve is now a global singleton, matching direwolf). The column
	// will be dropped in a future migration once all deployments have
	// moved to the global config. See
	// .context/2026-04-18-smart-beacon-implementation.md.
	SbMinTurnTime uint32    `gorm:"default:5" json:"sb_min_turn_time"`
	SendToAPRSIS  bool      `gorm:"column:send_to_aprs_is;not null;default:false" json:"send_to_aprs_is"`
	Enabled       bool      `gorm:"not null;default:true" json:"enabled"`
	CreatedAt     time.Time `json:"-"`
	UpdatedAt     time.Time `json:"-"`
}

// SmartBeaconConfig is a singleton (id=1) row holding the global
// SmartBeacon curve parameters applied to every beacon with
// SmartBeacon=true. Mirrors direwolf's single SMARTBEACON directive:
// the curve is global, not per-beacon. No integer defaults are declared
// in gorm tags — defaults live in pkg/beacon.DefaultSmartBeacon() (the
// single source of truth) and are surfaced to callers via the DTO layer
// when no row exists. GetSmartBeaconConfig returning (nil, nil) signals
// "no row yet — apply defaults."
type SmartBeaconConfig struct {
	ID          uint32    `gorm:"primaryKey;autoIncrement" json:"-"`
	Enabled     bool      `gorm:"not null" json:"enabled"`
	FastSpeedKt uint32    `gorm:"not null" json:"fast_speed"`
	FastRateSec uint32    `gorm:"not null" json:"fast_rate"`
	SlowSpeedKt uint32    `gorm:"not null" json:"slow_speed"`
	SlowRateSec uint32    `gorm:"not null" json:"slow_rate"`
	MinTurnDeg  uint32    `gorm:"not null" json:"min_turn_angle"`
	TurnSlope   uint32    `gorm:"not null" json:"turn_slope"`
	MinTurnSec  uint32    `gorm:"not null" json:"min_turn_time"`
	CreatedAt   time.Time `json:"-"`
	UpdatedAt   time.Time `json:"-"`
}

// PositionLogConfig controls the optional persistent position history
// database. Disabled by default to protect SD-card-based systems.
type PositionLogConfig struct {
	ID      uint32 `gorm:"primaryKey" json:"id"`
	Enabled bool   `gorm:"not null;default:false" json:"enabled"`
	DBPath  string `gorm:"not null;default:'./graywolf-history.db'" json:"db_path"`
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

// Message is one persisted APRS text message, DM or tactical, in either
// direction. Columns cover the full lifecycle: receipt metadata, state
// transitions (SentAt/AckedAt/AckState/Attempts), retry scheduling
// (NextRetryAt + FailureReason), ack/reply-ack correlation (MsgID +
// ReplyAckID), and thread identity ((ThreadKind, ThreadKey) — "dm"
// uses peer callsign, "tactical" uses the tactical label).
//
// Lifecycle columns set by the repository:
//
//   - Insert: CreatedAt, ReceivedAt (inbound), Direction, FromCall,
//     ToCall, OurCall, ThreadKind, ThreadKey, PeerCall, Text, Unread,
//     etc. The repository derives ThreadKey + PeerCall at insert and
//     writes them directly — callers only need to set ThreadKind and
//     the direction-dependent raw callsigns.
//   - Send pipeline (Phase 3): QueuedAt, SentAt, AckState, Attempts,
//     NextRetryAt, FailureReason.
//   - Router (Phase 2): AckedAt, AckState, ReceivedByCall (for tactical
//     reply-ack correlation).
//
// ThreadKind is one of: "dm" (1:1) or "tactical" (group broadcast via
// tactical callsign). See the APRS messages feature plan for the full
// design.
type Message struct {
	ID             uint64         `gorm:"primaryKey;autoIncrement" json:"id"`
	Direction      string         `gorm:"not null;index:idx_msg_direction_unread,priority:1" json:"direction"` // "in" | "out"
	OurCall        string         `gorm:"size:9;not null;index:idx_msg_peer,priority:1;index:idx_msg_to_time,priority:1" json:"our_call"`
	PeerCall       string         `gorm:"size:9;not null;index:idx_msg_peer,priority:2;index:idx_msg_peer_time" json:"peer_call"`
	FromCall       string         `gorm:"size:9;not null;index:idx_msg_from_time,priority:1;index:idx_msg_msgid_from,priority:2" json:"from_call"`
	ToCall         string         `gorm:"size:9;not null;index:idx_msg_to_time,priority:2" json:"to_call"`
	Text           string         `gorm:"size:200;not null" json:"text"`
	MsgID          string         `gorm:"size:5;index:idx_msg_msgid_from,priority:1" json:"msg_id"`
	CreatedAt      time.Time      `gorm:"not null;index:idx_msg_peer,priority:3;index:idx_msg_from_time,priority:2;index:idx_msg_to_time,priority:3;index:idx_msg_peer_time,priority:2;index:idx_msg_thread,priority:3" json:"created_at"`
	UpdatedAt      time.Time      `gorm:"not null" json:"updated_at"`
	ReceivedAt     *time.Time     `json:"received_at,omitempty"`
	SentAt         *time.Time     `json:"sent_at,omitempty"`
	AckedAt        *time.Time     `json:"acked_at,omitempty"`
	AckState       string         `gorm:"size:16;not null;default:'none'" json:"ack_state"` // none | acked | rejected | broadcast
	Source         string         `gorm:"size:4;not null;default:''" json:"source"`         // rf | is (string form of aprs.Direction)
	Channel        uint32         `gorm:"not null;default:0" json:"channel"`
	Path           string         `gorm:"size:64" json:"path"`                      // display path, e.g. "W1ABC*,WIDE1-1*"
	Via            string         `gorm:"size:64" json:"via"`                       // last used digipeater
	RawTNC2        string         `gorm:"column:raw_tnc2;size:512" json:"raw_tnc2"` // archival raw text
	Unread         bool           `gorm:"not null;default:false;index:idx_msg_direction_unread,priority:2" json:"unread"`
	Attempts       uint32         `gorm:"not null;default:0" json:"attempts"`
	NextRetryAt    *time.Time     `json:"next_retry_at,omitempty"`
	FailureReason  string         `gorm:"size:128" json:"failure_reason"`
	ReplyAckID     string         `gorm:"size:5" json:"reply_ack_id"` // inbound: APRS11 reply-ack id we observed
	IsAck          bool           `gorm:"not null;default:false" json:"is_ack"`
	IsRej          bool           `gorm:"not null;default:false" json:"is_rej"`
	IsBulletin     bool           `gorm:"not null;default:false" json:"is_bulletin"`
	IsNWS          bool           `gorm:"column:is_nws;not null;default:false" json:"is_nws"`
	PreferIS       bool           `gorm:"column:prefer_is;not null;default:false" json:"prefer_is"`
	DeletedAt      gorm.DeletedAt `gorm:"index" json:"-"`
	ThreadKind     string         `gorm:"size:10;not null;default:'dm';index:idx_msg_thread,priority:1" json:"thread_kind"` // dm | tactical
	ThreadKey      string         `gorm:"size:9;not null;default:'';index:idx_msg_thread,priority:2" json:"thread_key"`     // peer callsign for dm, tactical label for tactical
	ReceivedByCall string         `gorm:"size:9" json:"received_by_call"`                                                   // tactical outbound: first acker's call
	// Kind classifies the message body so the UI can render specialized
	// affordances (e.g. an Accept button for tactical invites) without
	// having to re-parse the wire text. Defaults to "text"; "invite"
	// marks a `!GW1 INVITE <TAC>` DM. The CHECK constraint pins the
	// enum at the SQL layer as a backstop against accidental writes of
	// other values from SQL shells or future migrations. No index — the
	// column is never a query predicate, only a display tag.
	Kind string `gorm:"size:10;not null;default:'text';check:kind IN ('text','invite')" json:"kind"`
	// InviteTactical is the tactical callsign referenced by an invite
	// message. Empty when Kind != "invite". Size 9 mirrors ThreadKey /
	// TacticalCallsign.Callsign.
	InviteTactical string `gorm:"size:9;not null;default:''" json:"invite_tactical"`
	// InviteAcceptedAt records when the local operator accepted this
	// invite. Audit-only: UI rendering of "Joined" keys off the live
	// TacticalSet cache, not this column, so first-paint is race-free
	// on refresh. Nil until accept. No index.
	InviteAcceptedAt *time.Time `json:"invite_accepted_at,omitempty"`
}

// TableName pins the messages table name to the plural lower-case form
// GORM would infer. Explicit so the migration-list raw SQL stays
// obviously in sync with the model.
func (Message) TableName() string { return "messages" }

// MessageCounter is a singleton (id=1) holding the next msgid to
// allocate. NextID rolls 1..999; allocation skips values currently
// held by outstanding outbound DM rows (see pkg/messages/store.go
// AllocateMsgID). Separate from MessagePreferences so bumping the
// counter does not touch the preferences row.
type MessageCounter struct {
	ID        uint32    `gorm:"primaryKey;autoIncrement" json:"-"`
	NextID    uint32    `gorm:"not null;default:1" json:"next_id"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

// MessagePreferences is a singleton (id=1) holding operator-level
// messaging preferences. Seeded at migrate-time with defaults if no row
// exists. See plan Phase 3 for semantics.
type MessagePreferences struct {
	ID               uint32    `gorm:"primaryKey;autoIncrement" json:"-"`
	FallbackPolicy   string    `gorm:"size:16;not null;default:'is_fallback'" json:"fallback_policy"` // rf_only | is_fallback | is_only | both
	DefaultPath      string    `gorm:"size:64;not null;default:'WIDE1-1,WIDE2-1'" json:"default_path"`
	RetryMaxAttempts uint32    `gorm:"not null;default:4" json:"retry_max_attempts"`
	RetentionDays    uint32    `gorm:"not null;default:0" json:"retention_days"` // 0 = forever
	CreatedAt        time.Time `json:"-"`
	UpdatedAt        time.Time `json:"-"`
}

// TacticalCallsign is one monitored tactical addressee label. Operators
// register these to participate in group threads keyed by the label.
// Callsign is normalized to uppercase via BeforeSave so any path in/out
// is safe. See plan "Group chat via tactical callsigns" section.
type TacticalCallsign struct {
	ID       uint32 `gorm:"primaryKey;autoIncrement" json:"id"`
	Callsign string `gorm:"size:9;not null;uniqueIndex" json:"callsign"` // 1-9 [A-Z0-9-], uppercase
	Alias    string `gorm:"size:64" json:"alias"`                        // optional free-text
	// Enabled: column does not declare default:true on purpose. The
	// handler-level default ("Monitor now" toggle) runs before the
	// insert, and a GORM default:true would silently override a caller
	// passing false (GORM treats Go-zero values as "use the DB
	// default"), which is hostile to the common "create disabled" path.
	Enabled   bool      `gorm:"not null" json:"enabled"`
	CreatedAt time.Time `json:"-"`
	UpdatedAt time.Time `json:"-"`
}

// BeforeSave normalizes Callsign to uppercase and trims whitespace
// before insert or update. Ensures the router's case-sensitive exact
// match against the cached set always sees a canonical value regardless
// of how a handler constructed the row.
func (t *TacticalCallsign) BeforeSave(_ *gorm.DB) error {
	t.Callsign = strings.ToUpper(strings.TrimSpace(t.Callsign))
	return nil
}
