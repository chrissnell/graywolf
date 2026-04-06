package configstore

import "time"

// KissInterface represents one row in kiss_interfaces. Each Server in
// pkg/kiss corresponds to one row. InterfaceType is "tcp"|"serial"|
// "bluetooth"; for serial/bluetooth the Device and BaudRate are used
// and ListenAddr may be empty.
type KissInterface struct {
	ID            uint32 `gorm:"primaryKey;autoIncrement"`
	Name          string `gorm:"not null;uniqueIndex"`
	InterfaceType string `gorm:"not null;default:'tcp'"` // tcp|serial|bluetooth
	ListenAddr    string // host:port for tcp
	Device        string // /dev/ttyUSB0 or bluetooth mac
	BaudRate      uint32 `gorm:"default:9600"`
	Channel       uint32 `gorm:"not null;default:1"` // default radio channel for this interface
	Broadcast     bool   `gorm:"not null;default:true"`
	Enabled       bool   `gorm:"not null;default:true"`
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// AgwConfig is a singleton (id=1) row describing the AGWPE listener.
type AgwConfig struct {
	ID         uint32 `gorm:"primaryKey;autoIncrement"`
	ListenAddr string `gorm:"not null;default:'0.0.0.0:8000'"`
	Callsigns  string `gorm:"not null;default:'N0CALL'"` // CSV; one per AGW port
	Enabled    bool   `gorm:"not null;default:false"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// TxTiming holds per-channel CSMA parameters. Mirrors
// txgovernor.ChannelTiming.
type TxTiming struct {
	ID        uint32 `gorm:"primaryKey;autoIncrement"`
	Channel   uint32 `gorm:"not null;uniqueIndex"`
	TxDelayMs uint32 `gorm:"not null;default:300"`
	TxTailMs  uint32 `gorm:"not null;default:100"`
	SlotMs    uint32 `gorm:"not null;default:100"`
	Persist   uint32 `gorm:"not null;default:63"`
	FullDup   bool   `gorm:"not null;default:false"`
	// Rate limits; 0 = unlimited.
	Rate1Min uint32 `gorm:"not null;default:0"`
	Rate5Min uint32 `gorm:"not null;default:0"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// DigipeaterConfig is a singleton (id=1) row with global digipeater
// settings.
type DigipeaterConfig struct {
	ID                   uint32 `gorm:"primaryKey;autoIncrement"`
	Enabled              bool   `gorm:"not null;default:false"`
	DedupeWindowSeconds  uint32 `gorm:"not null;default:30"`
	MyCall               string `gorm:"not null;default:'N0CALL'"` // local callsign used for preemptive digi
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// DigipeaterRule is one per-channel digipeater alias/rule. The digi
// engine walks rules in Priority ascending order looking for a match
// against an unconsumed path entry.
//
// Action enumeration:
//   "repeat"   — retransmit on ToChannel, consume this alias slot
//   "drop"     — match and suppress (filter-only rule)
//
// AliasType enumeration:
//   "widen"    — WIDEn-N style (Alias is the base e.g. "WIDE"; consumes 1 hop, decrements SSID)
//   "exact"    — exact callsign match (Alias is full "CALL[-SSID]"); e.g. the local callsign (preemptive)
//   "trace"    — TRACEn-N behaves like WIDEn-N but also inserts the local callsign before the alias
type DigipeaterRule struct {
	ID          uint32 `gorm:"primaryKey;autoIncrement"`
	FromChannel uint32 `gorm:"not null;index"`
	ToChannel   uint32 `gorm:"not null"`
	Alias       string `gorm:"not null"`
	AliasType   string `gorm:"not null;default:'widen'"` // widen|exact|trace
	MaxHops     uint32 `gorm:"not null;default:2"`       // maximum N-N accepted (e.g. WIDE2-2)
	Action      string `gorm:"not null;default:'repeat'"`
	Priority    uint32 `gorm:"not null;default:100"` // lower = evaluated first
	Enabled     bool   `gorm:"not null;default:true"`
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// IGateConfig is a singleton (id=1) row for the iGate.
type IGateConfig struct {
	ID              uint32 `gorm:"primaryKey;autoIncrement"`
	Enabled         bool   `gorm:"not null;default:false"`
	Server          string `gorm:"not null;default:'rotate.aprs2.net'"`
	Port            uint32 `gorm:"not null;default:14580"`
	Callsign        string `gorm:"not null;default:'N0CALL'"`
	Passcode        string `gorm:"not null;default:'-1'"` // string to tolerate "-1"
	ServerFilter    string // APRS-IS server-side filter expression
	SimulationMode  bool   `gorm:"not null;default:false"`
	GateRfToIs      bool   `gorm:"not null;default:true"`
	GateIsToRf      bool   `gorm:"not null;default:false"`
	RfChannel       uint32 `gorm:"not null;default:1"`          // channel used when gating IS->RF
	MaxMsgHops      uint32 `gorm:"not null;default:2"`          // WIDE hops for IS->RF messages
	SoftwareName    string `gorm:"not null;default:'graywolf'"` // APRS-IS login banner software name
	SoftwareVersion string `gorm:"not null;default:'0.1'"`      // APRS-IS login banner version
	TxChannel       uint32 `gorm:"not null;default:1"`          // radio channel for IS->RF submissions
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// IGateRfFilter is a per-channel allow/deny rule used to decide which
// RF-originated packets are forwarded to APRS-IS. Evaluation: lowest
// Priority first (ascending order); first match determines action.
type IGateRfFilter struct {
	ID        uint32 `gorm:"primaryKey;autoIncrement"`
	Channel   uint32 `gorm:"not null;index"`
	Type      string `gorm:"not null"` // callsign|prefix|message_dest|object
	Pattern   string `gorm:"not null"`
	Action    string `gorm:"not null;default:'allow'"` // allow|deny
	Priority  uint32 `gorm:"not null;default:100"`
	Enabled   bool   `gorm:"not null;default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}

// GPSConfig is a singleton (id=1) row for the GPS receiver.
type GPSConfig struct {
	ID         uint32 `gorm:"primaryKey;autoIncrement"`
	SourceType string `gorm:"not null;default:'none'"` // none|serial|gpsd
	Device     string // serial device path, e.g. /dev/ttyUSB0
	BaudRate   uint32 `gorm:"not null;default:4800"`
	GpsdHost   string `gorm:"not null;default:'localhost'"`
	GpsdPort   uint32 `gorm:"not null;default:2947"`
	Enabled    bool   `gorm:"not null;default:false"`
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Beacon is a scheduled beacon. Type selects the payload builder.
type Beacon struct {
	ID              uint32 `gorm:"primaryKey;autoIncrement"`
	Type            string `gorm:"not null;default:'position'"` // position|object|tracker|custom|igate
	Channel         uint32 `gorm:"not null;default:1"`
	Callsign        string `gorm:"not null"`
	Destination     string `gorm:"not null;default:'APGRWF'"`
	Path            string `gorm:"not null;default:'WIDE1-1'"`
	Latitude        float64
	Longitude       float64
	AltFt           float64 // altitude in feet for position reports
	Ambiguity       uint32  `gorm:"not null;default:0"` // 0..4 digits of position ambiguity
	SymbolTable     string  `gorm:"not null;default:'/'"`
	Symbol          string  `gorm:"not null;default:'-'"`
	Overlay         string  // alternate symbol table overlay character
	Compress        bool    `gorm:"not null;default:false"` // use compressed position encoding
	Messaging       bool    `gorm:"not null;default:false"` // '=' instead of '!' prefix
	Comment         string
	CommentCmd      string // shell command whose stdout is appended as comment
	CustomInfo      string // raw info field override for Type=="custom"
	ObjectName      string // for Type=="object"
	Power           uint32 `gorm:"not null;default:0"` // watts for PHG
	Height          uint32 `gorm:"not null;default:0"` // feet HAAT for PHG
	Gain            uint32 `gorm:"not null;default:0"` // dBi for PHG
	Dir             uint32 `gorm:"not null;default:0"` // antenna direction 0..8 for PHG
	Freq            string // frequency string for freq info
	Tone            string // CTCSS/DCS tone
	FreqOffset      string // repeater offset
	DelaySeconds    uint32 `gorm:"not null;default:30"`
	EverySeconds    uint32 `gorm:"not null;default:1800"`
	SlotSeconds     int32  `gorm:"not null;default:-1"`
	SmartBeacon     bool   `gorm:"not null;default:false"`
	SbFastSpeed     uint32 `gorm:"default:60"`
	SbSlowSpeed     uint32 `gorm:"default:5"`
	SbFastRate      uint32 `gorm:"default:60"`
	SbSlowRate      uint32 `gorm:"default:1800"`
	SbTurnAngle     uint32 `gorm:"default:30"`
	SbTurnSlope     uint32 `gorm:"default:255"`
	SbMinTurnTime   uint32 `gorm:"default:5"`
	Enabled         bool   `gorm:"not null;default:true"`
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

// PacketFilter is a reserved stub table for future per-channel packet
// filters (Phase 5/6).
type PacketFilter struct {
	ID        uint32 `gorm:"primaryKey;autoIncrement"`
	Channel   uint32 `gorm:"not null;index"`
	Name      string `gorm:"not null"`
	Expr      string `gorm:"not null"`
	Action    string `gorm:"not null;default:'allow'"`
	Enabled   bool   `gorm:"not null;default:true"`
	CreatedAt time.Time
	UpdatedAt time.Time
}
