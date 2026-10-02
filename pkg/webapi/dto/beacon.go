package dto

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
)

// BeaconRequest is the body accepted by POST /api/beacons and
// PUT /api/beacons/{id}.
//
// Callsign is a per-beacon callsign override (see centralized
// station-callsign plan, D2/D3). The request DTO uses *string so the
// three meaningful states are expressible independently:
//
//   - nil         → field omitted; on PUT, leave the stored value
//     unchanged. On POST, treated the same as "".
//   - ""          → inherit from StationConfig at transmit time.
//   - non-empty   → explicit override (e.g. a vanity or tactical call).
//
// The response DTO carries Callsign as plain string — an empty value
// in the response means "inherits from station callsign".
type BeaconRequest struct {
	Type           string  `json:"type"`
	Channel        uint32  `json:"channel"`
	Callsign       *string `json:"callsign"`
	Destination    string  `json:"destination"`
	Path           string  `json:"path"`
	UseGps         bool    `json:"use_gps"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	AltFt          float64 `json:"alt_ft"`
	Ambiguity      uint32  `json:"ambiguity"`
	SymbolTable    string  `json:"symbol_table"`
	Symbol         string  `json:"symbol"`
	Overlay        string  `json:"overlay"`
	PositionFormat string  `json:"position_format"`
	Messaging      bool    `json:"messaging"`
	Comment        string  `json:"comment"`
	CommentCmd     string  `json:"comment_cmd"`
	CustomInfo     string  `json:"custom_info"`
	ObjectName     string  `json:"object_name"`
	Power          uint32  `json:"power"`
	Height         uint32  `json:"height"`
	Gain           uint32  `json:"gain"`
	Dir            uint32  `json:"dir"`
	Freq           string  `json:"freq"`
	Tone           string  `json:"tone"`
	ToneFreq       string  `json:"tone_freq"`
	FreqOffset     string  `json:"freq_offset"`
	DelaySeconds   uint32  `json:"delay_seconds"`
	EverySeconds   uint32  `json:"interval"`
	SlotSeconds    int32   `json:"slot_seconds"`
	SmartBeacon    bool    `json:"smart_beacon"`
	SbFastSpeed    uint32  `json:"sb_fast_speed"`
	SbSlowSpeed    uint32  `json:"sb_slow_speed"`
	SbFastRate     uint32  `json:"sb_fast_rate"`
	SbSlowRate     uint32  `json:"sb_slow_rate"`
	SbTurnAngle    uint32  `json:"sb_turn_angle"`
	SbTurnSlope    uint32  `json:"sb_turn_slope"`
	SbMinTurnTime  uint32  `json:"sb_min_turn_time"`
	SendPath       string  `json:"send_path" enums:"rf,both,is_only" example:"rf"`
	Enabled        bool    `json:"enabled"`
}

// Validate rejects configurations that would cause the scheduler to
// skip transmission at send time. Position/igate beacons must either
// source coordinates from the GPS cache or carry non-zero fixed
// coordinates. The Callsign override field is no longer validated here
// — empty / nil mean "inherit from StationConfig", which is now the
// canonical source of truth.
//
// position_format and ambiguity are also validated against APRS101
// constraints: ambiguity must be 0..4; only uncompressed and mic_e
// carry ambiguity bytes, so compressed must keep ambiguity at zero.
//
// QSY (Freq/Tone/ToneFreq/FreqOffset) is restricted to type=="position"
// beacons with no callsign override — QSY describes where to reach the
// operator's own station, so it must not be attachable to a beacon
// transmitting objects, trackers, igate status, or someone else's
// callsign. pkg/beacon/builder.go re-checks this same restriction as a
// defense-in-depth guard against hand-edited DB rows.
func (r BeaconRequest) Validate() error {
	switch r.SendPath {
	case "", "rf", "both", "is_only":
		// "" normalizes to rf in normalizedSendPath
	default:
		return fmt.Errorf("send_path must be one of rf, both, is_only (got %q)", r.SendPath)
	}
	// Reject addresses the beacon loader cannot parse, so an invalid
	// callsign/destination/path is surfaced here at save time instead of
	// silently dropping the beacon from the scheduler at load. APRS SSIDs
	// must be 0-15; this also catches bad characters and over-length calls.
	// An empty/nil callsign override means "inherit the station callsign",
	// which is validated by the station config, so skip it here.
	if r.Callsign != nil {
		if c := strings.TrimSpace(*r.Callsign); c != "" {
			if _, err := ax25.ParseAddress(c); err != nil {
				return fmt.Errorf("callsign %q is not a valid APRS address (SSID must be 0-15): %w", c, err)
			}
		}
	}
	if d := strings.TrimSpace(r.Destination); d != "" {
		if _, err := ax25.ParseAddress(d); err != nil {
			return fmt.Errorf("destination %q is not a valid APRS address: %w", d, err)
		}
	}
	for _, p := range strings.Split(r.Path, ",") {
		if p = strings.TrimSpace(p); p != "" {
			if _, err := ax25.ParseAddress(p); err != nil {
				return fmt.Errorf("path element %q is not a valid APRS address: %w", p, err)
			}
		}
	}
	switch r.Type {
	case "position", "igate":
		if !r.UseGps && r.Latitude == 0 && r.Longitude == 0 {
			return fmt.Errorf("latitude/longitude required when use_gps is false")
		}
	}
	if r.Type == "position" || r.Type == "tracker" || r.Type == "igate" {
		switch r.PositionFormat {
		case "", "compressed":
			if r.Ambiguity != 0 {
				return fmt.Errorf("ambiguity must be 0 when position_format is compressed")
			}
		case "uncompressed", "mic_e":
			// fall through to ambiguity range check below
		default:
			return fmt.Errorf("position_format must be one of compressed, uncompressed, mic_e (got %q)", r.PositionFormat)
		}
		if r.Ambiguity > 4 {
			return fmt.Errorf("ambiguity must be 0..4 (got %d)", r.Ambiguity)
		}
	}
	if err := r.validateQSY(); err != nil {
		return err
	}
	return nil
}

// validateQSY enforces the QSY eligibility rule (type=="position", no
// callsign override) plus the field-level shape of Freq/Tone/ToneFreq/
// FreqOffset when QSY is configured.
func (r BeaconRequest) validateQSY() error {
	hasQSY := r.Freq != "" || r.Tone != "" || r.ToneFreq != "" || r.FreqOffset != ""
	if !hasQSY {
		return nil
	}
	hasOverride := r.Callsign != nil && strings.TrimSpace(*r.Callsign) != ""
	if r.Type != "position" || hasOverride {
		return fmt.Errorf("QSY fields require type=position with no callsign override (got type=%q, override=%v)", r.Type, hasOverride)
	}
	if r.Freq != "" {
		if f, err := strconv.ParseFloat(r.Freq, 64); err != nil || f <= 0 {
			return fmt.Errorf("freq must be a positive number in MHz (got %q)", r.Freq)
		}
	}
	switch r.Tone {
	case "", "ctcss", "dcs":
	default:
		return fmt.Errorf("tone must be one of \"\", ctcss, dcs (got %q)", r.Tone)
	}
	if r.Tone != "" && r.ToneFreq == "" {
		return fmt.Errorf("tone_freq is required when tone is set")
	}
	if r.FreqOffset != "" {
		if _, err := strconv.ParseFloat(r.FreqOffset, 64); err != nil {
			return fmt.Errorf("freq_offset must be a number in MHz (got %q)", r.FreqOffset)
		}
	}
	return nil
}

// normalizedSendPath returns the send_path value to persist. Empty
// (older client / unset form) becomes "rf" so the column never holds a
// surprise value. Validate() rejects unknown non-empty values up front.
func (r BeaconRequest) normalizedSendPath() string {
	switch r.SendPath {
	case "rf", "both", "is_only":
		return r.SendPath
	default:
		return "rf"
	}
}

// normalizedFormat returns the position_format value to persist:
// empty or unknown becomes "compressed" so the DB column never holds a
// surprise string. Validate() rejects unknown values up front so this
// helper only papers over the empty-string default the form may emit
// before client-side defaults bind.
func (r BeaconRequest) normalizedFormat() string {
	switch r.PositionFormat {
	case "compressed", "uncompressed", "mic_e":
		return r.PositionFormat
	default:
		return "compressed"
	}
}

// callsignValue resolves the request's pointer callsign into the
// persisted string value. nil becomes empty (inherit) for POST; for
// PUT, the handler uses ApplyToUpdate which preserves the existing
// value when the pointer is nil.
func (r BeaconRequest) callsignValue() string {
	if r.Callsign == nil {
		return ""
	}
	return *r.Callsign
}

func (r BeaconRequest) ToModel() configstore.Beacon {
	return configstore.Beacon{
		Type:           r.Type,
		Channel:        r.Channel,
		Callsign:       r.callsignValue(),
		Destination:    r.Destination,
		Path:           r.Path,
		UseGps:         r.UseGps,
		Latitude:       r.Latitude,
		Longitude:      r.Longitude,
		AltFt:          r.AltFt,
		Ambiguity:      r.Ambiguity,
		SymbolTable:    r.SymbolTable,
		Symbol:         r.Symbol,
		Overlay:        r.Overlay,
		PositionFormat: r.normalizedFormat(),
		Messaging:      r.Messaging,
		Comment:        r.Comment,
		CommentCmd:     r.CommentCmd,
		CustomInfo:     r.CustomInfo,
		ObjectName:     r.ObjectName,
		Power:          r.Power,
		Height:         r.Height,
		Gain:           r.Gain,
		Dir:            r.Dir,
		Freq:           r.Freq,
		Tone:           r.Tone,
		ToneFreq:       r.ToneFreq,
		FreqOffset:     r.FreqOffset,
		DelaySeconds:   r.DelaySeconds,
		EverySeconds:   r.EverySeconds,
		SlotSeconds:    r.SlotSeconds,
		SmartBeacon:    r.SmartBeacon,
		SbFastSpeed:    r.SbFastSpeed,
		SbSlowSpeed:    r.SbSlowSpeed,
		SbFastRate:     r.SbFastRate,
		SbSlowRate:     r.SbSlowRate,
		SbTurnAngle:    r.SbTurnAngle,
		SbTurnSlope:    r.SbTurnSlope,
		SbMinTurnTime:  r.SbMinTurnTime,
		SendPath:       r.normalizedSendPath(),
		Enabled:        r.Enabled,
	}
}

func (r BeaconRequest) ToUpdate(id uint32) configstore.Beacon {
	m := r.ToModel()
	m.ID = id
	return m
}

// ApplyToUpdate merges the request onto an existing stored beacon,
// honouring pointer-nil = "leave unchanged" on the Callsign override
// field. All other fields are overwritten with the request value
// (replace-style PUT).
func (r BeaconRequest) ApplyToUpdate(id uint32, existing configstore.Beacon) configstore.Beacon {
	callsign := existing.Callsign
	if r.Callsign != nil {
		callsign = *r.Callsign
	}
	return configstore.Beacon{
		ID:             id,
		Type:           r.Type,
		Channel:        r.Channel,
		Callsign:       callsign,
		Destination:    r.Destination,
		Path:           r.Path,
		UseGps:         r.UseGps,
		Latitude:       r.Latitude,
		Longitude:      r.Longitude,
		AltFt:          r.AltFt,
		Ambiguity:      r.Ambiguity,
		SymbolTable:    r.SymbolTable,
		Symbol:         r.Symbol,
		Overlay:        r.Overlay,
		PositionFormat: r.normalizedFormat(),
		Messaging:      r.Messaging,
		Comment:        r.Comment,
		CommentCmd:     r.CommentCmd,
		CustomInfo:     r.CustomInfo,
		ObjectName:     r.ObjectName,
		Power:          r.Power,
		Height:         r.Height,
		Gain:           r.Gain,
		Dir:            r.Dir,
		Freq:           r.Freq,
		Tone:           r.Tone,
		ToneFreq:       r.ToneFreq,
		FreqOffset:     r.FreqOffset,
		DelaySeconds:   r.DelaySeconds,
		EverySeconds:   r.EverySeconds,
		SlotSeconds:    r.SlotSeconds,
		SmartBeacon:    r.SmartBeacon,
		SbFastSpeed:    r.SbFastSpeed,
		SbSlowSpeed:    r.SbSlowSpeed,
		SbFastRate:     r.SbFastRate,
		SbSlowRate:     r.SbSlowRate,
		SbTurnAngle:    r.SbTurnAngle,
		SbTurnSlope:    r.SbTurnSlope,
		SbMinTurnTime:  r.SbMinTurnTime,
		SendPath:       r.normalizedSendPath(),
		Enabled:        r.Enabled,
	}
}

// BeaconResponse is the body returned by GET/POST/PUT for a beacon.
// Callsign is the stored value — empty means "inherit from station
// callsign" at transmit time.
type BeaconResponse struct {
	ID             uint32  `json:"id"`
	Type           string  `json:"type"`
	Channel        uint32  `json:"channel"`
	Callsign       string  `json:"callsign"`
	Destination    string  `json:"destination"`
	Path           string  `json:"path"`
	UseGps         bool    `json:"use_gps"`
	Latitude       float64 `json:"latitude"`
	Longitude      float64 `json:"longitude"`
	AltFt          float64 `json:"alt_ft"`
	Ambiguity      uint32  `json:"ambiguity"`
	SymbolTable    string  `json:"symbol_table"`
	Symbol         string  `json:"symbol"`
	Overlay        string  `json:"overlay"`
	PositionFormat string  `json:"position_format"`
	Messaging      bool    `json:"messaging"`
	Comment        string  `json:"comment"`
	CommentCmd     string  `json:"comment_cmd"`
	CustomInfo     string  `json:"custom_info"`
	ObjectName     string  `json:"object_name"`
	Power          uint32  `json:"power"`
	Height         uint32  `json:"height"`
	Gain           uint32  `json:"gain"`
	Dir            uint32  `json:"dir"`
	Freq           string  `json:"freq"`
	Tone           string  `json:"tone"`
	ToneFreq       string  `json:"tone_freq"`
	FreqOffset     string  `json:"freq_offset"`
	DelaySeconds   uint32  `json:"delay_seconds"`
	EverySeconds   uint32  `json:"interval"`
	SlotSeconds    int32   `json:"slot_seconds"`
	SmartBeacon    bool    `json:"smart_beacon"`
	SbFastSpeed    uint32  `json:"sb_fast_speed"`
	SbSlowSpeed    uint32  `json:"sb_slow_speed"`
	SbFastRate     uint32  `json:"sb_fast_rate"`
	SbSlowRate     uint32  `json:"sb_slow_rate"`
	SbTurnAngle    uint32  `json:"sb_turn_angle"`
	SbTurnSlope    uint32  `json:"sb_turn_slope"`
	SbMinTurnTime  uint32  `json:"sb_min_turn_time"`
	SendPath       string  `json:"send_path" enums:"rf,both,is_only" example:"rf"`
	Enabled        bool    `json:"enabled"`
}

func BeaconFromModel(m configstore.Beacon) BeaconResponse {
	return BeaconResponse{
		ID:             m.ID,
		Type:           m.Type,
		Channel:        m.Channel,
		Callsign:       m.Callsign,
		Destination:    m.Destination,
		Path:           m.Path,
		UseGps:         m.UseGps,
		Latitude:       m.Latitude,
		Longitude:      m.Longitude,
		AltFt:          m.AltFt,
		Ambiguity:      m.Ambiguity,
		SymbolTable:    m.SymbolTable,
		Symbol:         m.Symbol,
		Overlay:        m.Overlay,
		PositionFormat: m.PositionFormat,
		Messaging:      m.Messaging,
		Comment:        m.Comment,
		CommentCmd:     m.CommentCmd,
		CustomInfo:     m.CustomInfo,
		ObjectName:     m.ObjectName,
		Power:          m.Power,
		Height:         m.Height,
		Gain:           m.Gain,
		Dir:            m.Dir,
		Freq:           m.Freq,
		Tone:           m.Tone,
		ToneFreq:       m.ToneFreq,
		FreqOffset:     m.FreqOffset,
		DelaySeconds:   m.DelaySeconds,
		EverySeconds:   m.EverySeconds,
		SlotSeconds:    m.SlotSeconds,
		SmartBeacon:    m.SmartBeacon,
		SbFastSpeed:    m.SbFastSpeed,
		SbSlowSpeed:    m.SbSlowSpeed,
		SbFastRate:     m.SbFastRate,
		SbSlowRate:     m.SbSlowRate,
		SbTurnAngle:    m.SbTurnAngle,
		SbTurnSlope:    m.SbTurnSlope,
		SbMinTurnTime:  m.SbMinTurnTime,
		SendPath:       m.SendPath,
		Enabled:        m.Enabled,
	}
}

func BeaconsFromModels(ms []configstore.Beacon) []BeaconResponse {
	out := make([]BeaconResponse, len(ms))
	for i, m := range ms {
		out[i] = BeaconFromModel(m)
	}
	return out
}

// BeaconSendResponse is the body returned by POST /api/beacons/{id}/send
// when a one-shot transmission has been handed to the beacon scheduler.
type BeaconSendResponse struct {
	Status string `json:"status"` // "sent"
}
