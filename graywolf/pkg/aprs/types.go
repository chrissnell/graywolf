package aprs

import (
	"context"
	"time"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// PacketType is the high-level classification of an APRS packet,
// suitable for metrics and log filtering.
type PacketType string

const (
	PacketUnknown      PacketType = "unknown"
	PacketPosition     PacketType = "position"
	PacketMessage      PacketType = "message"
	PacketTelemetry    PacketType = "telemetry"
	PacketWeather      PacketType = "weather"
	PacketObject       PacketType = "object"
	PacketItem         PacketType = "item"
	PacketMicE         PacketType = "mic-e"
	PacketStatus       PacketType = "status"
	PacketCapabilities PacketType = "capabilities"
	PacketDF           PacketType = "df-report"
	PacketQuery        PacketType = "query"
	PacketThirdParty   PacketType = "third-party"
)

// Position is the decoded geographic location carried by a packet. Not
// every packet type has one (messages and telemetry do not).
type Position struct {
	Latitude   float64 // decimal degrees, positive north
	Longitude  float64 // decimal degrees, positive east
	Ambiguity  int     // 0..4, digits of ambiguity introduced by spaces
	Altitude   float64 // meters (0 if none reported)
	HasAlt     bool
	Speed      float64 // knots
	Course     int     // degrees true (0..359)
	HasCourse  bool
	Symbol     Symbol
	Compressed bool
	Timestamp  *time.Time // nil if positionless or no embedded time
	LocalTime  bool       // true if the timestamp was the '/' local-time form (APRS101 ch 6)
	PHG        *PHG       // decoded Power/Height/Gain/Directivity extension (APRS101 ch 7), nil if not present
	DAODatum   byte       // DAO datum byte (APRS101 DAO extension), 0 if not present
}

// Symbol is the APRS map symbol: table+code (e.g. '/' + '>' = car).
type Symbol struct {
	Table byte
	Code  byte
}

// Message is a directed-addressee message (addressee, text, id/ack/rej).
type Message struct {
	Addressee  string // 1..9 chars, space-padded in packet
	Text       string
	MessageID  string // optional identifier used for ACK/REJ correlation
	ReplyAck   string // piggybacked reply-ack id (aprs11/replyacks), empty if absent
	HasReplyAck bool  // true if a reply-ack trailer was present (ack may still be "")
	IsAck      bool
	IsRej      bool
	IsBulletin bool // addressee starts with BLN
	IsNWS      bool // NWS-originated
}

// TelemetryMeta carries PARM/UNIT/EQNS/BITS metadata messages (APRS101
// ch 13). These arrive as messages addressed to the telemetering
// station itself and are required to scale raw analog channels.
type TelemetryMeta struct {
	Kind        string // "parm", "unit", "eqns", or "bits"
	Parm        [13]string
	Unit        [13]string
	Eqns        [5][3]float64 // a, b, c coefficients per analog channel
	Bits        uint8         // BITS. sense-bits bitmap (active-high per bit)
	ProjectName string        // BITS. project title
}

// Telemetry is an APRS telemetry packet (T# uncompressed or base-91
// compressed form). Values are raw (unscaled) analog and digital
// channels; calibration coefficients live in parameter/equation/unit
// packets that are out-of-scope for Phase 3 decoding.
type Telemetry struct {
	Seq        int // 0..999, -1 if absent
	Analog     [5]float64
	AnalogHas  [5]bool // true for channels actually reported (distinguishes 0 from missing)
	Digital    uint8   // bits 0..7 (only lower 8)
	HasDigital bool
	Comment    string // trailing free-form
}

// Weather carries the APRS weather report fields (APRS101 ch 12).
// Unreported fields leave the corresponding Has* flag false.
type Weather struct {
	WindDirection  int // degrees true
	HasWindDir     bool
	WindSpeed      float64 // mph (1-minute sustained)
	HasWindSpeed   bool
	WindGust       float64 // mph (5-minute peak)
	HasWindGust    bool
	Temperature    float64 // degrees F
	HasTemp        bool
	Rain1Hour      float64 // hundredths of an inch
	HasRain1h      bool
	Rain24Hour     float64
	HasRain24h     bool
	RainSinceMid   float64
	HasRainMid     bool
	Humidity       int // percent (0..100)
	HasHumidity    bool
	Pressure       float64 // tenths of millibar (e.g. 10132 = 1013.2)
	HasPressure    bool
	Luminosity     int // watts/m^2
	HasLuminosity  bool
	Snowfall24h    float64 // inches (via 's' after 'g')
	HasSnow        bool
	RawRainCounter int // raw rain counter ('#' field)
	HasRawRain     bool
	SoftwareType   string // one-letter software code (e.g. 'w', 'x', 'd')
	WeatherUnitTag string // 2..4 ASCII letters identifying the unit/model
}

// Object is an APRS object report (packet prefix ';'). Name is 9 chars
// exactly (space-padded). Live == false means "killed".
type Object struct {
	Name      string
	Live      bool
	Timestamp *time.Time
	Position  *Position
	Comment   string
}

// Item is an APRS item report (packet prefix ')'). Name is 3..9 chars
// terminated by '!' (live) or '_' (killed).
type Item struct {
	Name     string
	Live     bool
	Position *Position
	Comment  string
}

// MicE is a decoded Mic-E (' or `) position report.
type MicE struct {
	Position     Position
	MessageCode  int // 0..7 index into the standard Mic-E message table
	MessageText  string
	Manufacturer string // e.g. "Kenwood TH-D74", "" if unknown
	Status       string // trailing status text
}

// Capabilities is a <...> station capabilities advertisement (IGATE, etc.)
type Capabilities struct {
	Entries map[string]string // key → value (value empty for flag entries)
}

// DirectionFinding is a bearing/number/quality tuple attached to a
// position report (the "/BRG/NRQ" appendix).
type DirectionFinding struct {
	Bearing int // degrees true
	Number  int // 0..9 station count
	Range   int // miles
	Quality int // 0..9
}

// DecodedAPRSPacket is the canonical decoded form that flows through
// graywolf's PacketOutput pipeline.
type DecodedAPRSPacket struct {
	Raw           []byte // original AX.25 frame bytes
	Source        string // callsign-SSID
	Dest          string
	Path          []string
	Type          PacketType
	Position      *Position
	Message       *Message
	Weather       *Weather
	Telemetry     *Telemetry
	Object        *Object
	Item          *Item
	MicE          *MicE
	Caps          *Capabilities
	DF            *DirectionFinding
	TelemetryMeta *TelemetryMeta     // PARM/UNIT/EQNS/BITS metadata (APRS101 ch 13)
	ThirdParty    *DecodedAPRSPacket // recursively-decoded inner packet for '}' traffic (APRS101 ch 20)
	Status        string             // for '>' status reports
	Comment       string             // residual free-form text after structured fields
	Timestamp     time.Time
	Channel       int
	Quality       int // modem-reported quality (0..100) if available
}

// FromAX25 populates the Source/Dest/Path fields of a DecodedAPRSPacket
// from an AX.25 frame. Helper for parser entry points.
func (p *DecodedAPRSPacket) FromAX25(f *ax25.Frame) {
	if f == nil {
		return
	}
	p.Source = f.Source.String()
	p.Dest = f.Dest.String()
	p.Path = make([]string, 0, len(f.Path))
	for _, a := range f.Path {
		p.Path = append(p.Path, a.String())
	}
}

// DedupKey returns a string suitable as a map key for APRS-level
// deduplication: the key is (source + info bytes), ignoring the
// AX.25 path and destination. This is the key the iGate uses for
// RF->IS duplicate suppression, where two identical payloads from
// the same source arriving via different geographic paths should be
// gated once (the first arrival) rather than once per path.
//
// Returns an empty string if the packet has no recoverable info
// field; callers should treat an empty key as "do not dedup".
//
// Distinct from ax25.Frame.DedupKey (which works at the frame layer
// with dest+source+info) and ax25.Frame.PathDedupKey (which the
// digipeater uses with source+dest+path+info). Those two operate on
// encoded frames; this one operates on a decoded APRS packet after
// the packet has been parsed out of the frame.
func (p *DecodedAPRSPacket) DedupKey() string {
	if p == nil {
		return ""
	}
	info := p.dedupInfoBytes()
	if len(info) == 0 {
		return ""
	}
	return p.Source + "\x00" + string(info)
}

// dedupInfoBytes recovers the AX.25 info field for dedup keying.
// Prefer the original Raw frame's info (lossless and byte-accurate);
// return nil if no Raw frame is available since an empty key
// disables dedup rather than silently conflating packets.
func (p *DecodedAPRSPacket) dedupInfoBytes() []byte {
	if len(p.Raw) == 0 {
		return nil
	}
	f, err := ax25.Decode(p.Raw)
	if err != nil || len(f.Info) == 0 {
		return nil
	}
	return f.Info
}

// PacketOutput is the pluggable sink for decoded packets (log, KISS
// rebroadcast, iGate, gRPC, ...). Implementations must be safe for
// concurrent use.
type PacketOutput interface {
	SendPacket(ctx context.Context, pkt *DecodedAPRSPacket) error
	Close() error
}

// InboundPacket is a request from an external source to transmit an
// AX.25 frame on a specific channel.
type InboundPacket struct {
	Raw     []byte
	Source  string
	Channel int
}

// PacketInput is the pluggable source of external TX requests (KISS,
// AGW, APRS-IS, gRPC, ...).
type PacketInput interface {
	RecvPacket(ctx context.Context) (*InboundPacket, error)
	Close() error
}
