package dto

import (
	"fmt"

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
	Type          string  `json:"type"`
	Channel       uint32  `json:"channel"`
	Callsign      *string `json:"callsign"`
	Destination   string  `json:"destination"`
	Path          string  `json:"path"`
	UseGps        bool    `json:"use_gps"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	AltFt         float64 `json:"alt_ft"`
	Ambiguity     uint32  `json:"ambiguity"`
	SymbolTable   string  `json:"symbol_table"`
	Symbol        string  `json:"symbol"`
	Overlay       string  `json:"overlay"`
	Compress      bool    `json:"compress"`
	Messaging     bool    `json:"messaging"`
	Comment       string  `json:"comment"`
	CommentCmd    string  `json:"comment_cmd"`
	CustomInfo    string  `json:"custom_info"`
	ObjectName    string  `json:"object_name"`
	Power         uint32  `json:"power"`
	Height        uint32  `json:"height"`
	Gain          uint32  `json:"gain"`
	Dir           uint32  `json:"dir"`
	Freq          string  `json:"freq"`
	Tone          string  `json:"tone"`
	FreqOffset    string  `json:"freq_offset"`
	DelaySeconds  uint32  `json:"delay_seconds"`
	EverySeconds  uint32  `json:"interval"`
	SlotSeconds   int32   `json:"slot_seconds"`
	SmartBeacon   bool    `json:"smart_beacon"`
	SbFastSpeed   uint32  `json:"sb_fast_speed"`
	SbSlowSpeed   uint32  `json:"sb_slow_speed"`
	SbFastRate    uint32  `json:"sb_fast_rate"`
	SbSlowRate    uint32  `json:"sb_slow_rate"`
	SbTurnAngle   uint32  `json:"sb_turn_angle"`
	SbTurnSlope   uint32  `json:"sb_turn_slope"`
	SbMinTurnTime uint32  `json:"sb_min_turn_time"`
	SendToAPRSIS  bool    `json:"send_to_aprs_is"`
	Enabled       bool    `json:"enabled"`
}

// Validate rejects configurations that would cause the scheduler to
// skip transmission at send time. Position/igate beacons must either
// source coordinates from the GPS cache or carry non-zero fixed
// coordinates. The Callsign override field is no longer validated here
// — empty / nil mean "inherit from StationConfig", which is now the
// canonical source of truth.
func (r BeaconRequest) Validate() error {
	switch r.Type {
	case "position", "igate":
		if !r.UseGps && r.Latitude == 0 && r.Longitude == 0 {
			return fmt.Errorf("latitude/longitude required when use_gps is false")
		}
	}
	return nil
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
		Type:          r.Type,
		Channel:       r.Channel,
		Callsign:      r.callsignValue(),
		Destination:   r.Destination,
		Path:          r.Path,
		UseGps:        r.UseGps,
		Latitude:      r.Latitude,
		Longitude:     r.Longitude,
		AltFt:         r.AltFt,
		Ambiguity:     r.Ambiguity,
		SymbolTable:   r.SymbolTable,
		Symbol:        r.Symbol,
		Overlay:       r.Overlay,
		Compress:      r.Compress,
		Messaging:     r.Messaging,
		Comment:       r.Comment,
		CommentCmd:    r.CommentCmd,
		CustomInfo:    r.CustomInfo,
		ObjectName:    r.ObjectName,
		Power:         r.Power,
		Height:        r.Height,
		Gain:          r.Gain,
		Dir:           r.Dir,
		Freq:          r.Freq,
		Tone:          r.Tone,
		FreqOffset:    r.FreqOffset,
		DelaySeconds:  r.DelaySeconds,
		EverySeconds:  r.EverySeconds,
		SlotSeconds:   r.SlotSeconds,
		SmartBeacon:   r.SmartBeacon,
		SbFastSpeed:   r.SbFastSpeed,
		SbSlowSpeed:   r.SbSlowSpeed,
		SbFastRate:    r.SbFastRate,
		SbSlowRate:    r.SbSlowRate,
		SbTurnAngle:   r.SbTurnAngle,
		SbTurnSlope:   r.SbTurnSlope,
		SbMinTurnTime: r.SbMinTurnTime,
		SendToAPRSIS:  r.SendToAPRSIS,
		Enabled:       r.Enabled,
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
		ID:            id,
		Type:          r.Type,
		Channel:       r.Channel,
		Callsign:      callsign,
		Destination:   r.Destination,
		Path:          r.Path,
		UseGps:        r.UseGps,
		Latitude:      r.Latitude,
		Longitude:     r.Longitude,
		AltFt:         r.AltFt,
		Ambiguity:     r.Ambiguity,
		SymbolTable:   r.SymbolTable,
		Symbol:        r.Symbol,
		Overlay:       r.Overlay,
		Compress:      r.Compress,
		Messaging:     r.Messaging,
		Comment:       r.Comment,
		CommentCmd:    r.CommentCmd,
		CustomInfo:    r.CustomInfo,
		ObjectName:    r.ObjectName,
		Power:         r.Power,
		Height:        r.Height,
		Gain:          r.Gain,
		Dir:           r.Dir,
		Freq:          r.Freq,
		Tone:          r.Tone,
		FreqOffset:    r.FreqOffset,
		DelaySeconds:  r.DelaySeconds,
		EverySeconds:  r.EverySeconds,
		SlotSeconds:   r.SlotSeconds,
		SmartBeacon:   r.SmartBeacon,
		SbFastSpeed:   r.SbFastSpeed,
		SbSlowSpeed:   r.SbSlowSpeed,
		SbFastRate:    r.SbFastRate,
		SbSlowRate:    r.SbSlowRate,
		SbTurnAngle:   r.SbTurnAngle,
		SbTurnSlope:   r.SbTurnSlope,
		SbMinTurnTime: r.SbMinTurnTime,
		SendToAPRSIS:  r.SendToAPRSIS,
		Enabled:       r.Enabled,
	}
}

// BeaconResponse is the body returned by GET/POST/PUT for a beacon.
// Callsign is the stored value — empty means "inherit from station
// callsign" at transmit time.
type BeaconResponse struct {
	ID            uint32  `json:"id"`
	Type          string  `json:"type"`
	Channel       uint32  `json:"channel"`
	Callsign      string  `json:"callsign"`
	Destination   string  `json:"destination"`
	Path          string  `json:"path"`
	UseGps        bool    `json:"use_gps"`
	Latitude      float64 `json:"latitude"`
	Longitude     float64 `json:"longitude"`
	AltFt         float64 `json:"alt_ft"`
	Ambiguity     uint32  `json:"ambiguity"`
	SymbolTable   string  `json:"symbol_table"`
	Symbol        string  `json:"symbol"`
	Overlay       string  `json:"overlay"`
	Compress      bool    `json:"compress"`
	Messaging     bool    `json:"messaging"`
	Comment       string  `json:"comment"`
	CommentCmd    string  `json:"comment_cmd"`
	CustomInfo    string  `json:"custom_info"`
	ObjectName    string  `json:"object_name"`
	Power         uint32  `json:"power"`
	Height        uint32  `json:"height"`
	Gain          uint32  `json:"gain"`
	Dir           uint32  `json:"dir"`
	Freq          string  `json:"freq"`
	Tone          string  `json:"tone"`
	FreqOffset    string  `json:"freq_offset"`
	DelaySeconds  uint32  `json:"delay_seconds"`
	EverySeconds  uint32  `json:"interval"`
	SlotSeconds   int32   `json:"slot_seconds"`
	SmartBeacon   bool    `json:"smart_beacon"`
	SbFastSpeed   uint32  `json:"sb_fast_speed"`
	SbSlowSpeed   uint32  `json:"sb_slow_speed"`
	SbFastRate    uint32  `json:"sb_fast_rate"`
	SbSlowRate    uint32  `json:"sb_slow_rate"`
	SbTurnAngle   uint32  `json:"sb_turn_angle"`
	SbTurnSlope   uint32  `json:"sb_turn_slope"`
	SbMinTurnTime uint32  `json:"sb_min_turn_time"`
	SendToAPRSIS  bool    `json:"send_to_aprs_is"`
	Enabled       bool    `json:"enabled"`
}

func BeaconFromModel(m configstore.Beacon) BeaconResponse {
	return BeaconResponse{
		ID:            m.ID,
		Type:          m.Type,
		Channel:       m.Channel,
		Callsign:      m.Callsign,
		Destination:   m.Destination,
		Path:          m.Path,
		UseGps:        m.UseGps,
		Latitude:      m.Latitude,
		Longitude:     m.Longitude,
		AltFt:         m.AltFt,
		Ambiguity:     m.Ambiguity,
		SymbolTable:   m.SymbolTable,
		Symbol:        m.Symbol,
		Overlay:       m.Overlay,
		Compress:      m.Compress,
		Messaging:     m.Messaging,
		Comment:       m.Comment,
		CommentCmd:    m.CommentCmd,
		CustomInfo:    m.CustomInfo,
		ObjectName:    m.ObjectName,
		Power:         m.Power,
		Height:        m.Height,
		Gain:          m.Gain,
		Dir:           m.Dir,
		Freq:          m.Freq,
		Tone:          m.Tone,
		FreqOffset:    m.FreqOffset,
		DelaySeconds:  m.DelaySeconds,
		EverySeconds:  m.EverySeconds,
		SlotSeconds:   m.SlotSeconds,
		SmartBeacon:   m.SmartBeacon,
		SbFastSpeed:   m.SbFastSpeed,
		SbSlowSpeed:   m.SbSlowSpeed,
		SbFastRate:    m.SbFastRate,
		SbSlowRate:    m.SbSlowRate,
		SbTurnAngle:   m.SbTurnAngle,
		SbTurnSlope:   m.SbTurnSlope,
		SbMinTurnTime: m.SbMinTurnTime,
		SendToAPRSIS:  m.SendToAPRSIS,
		Enabled:       m.Enabled,
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
