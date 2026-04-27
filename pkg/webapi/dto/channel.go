package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// ChannelRequest is the body accepted by POST /api/channels and
// PUT /api/channels/{id}.
//
// InputDeviceID is a pointer (*uint32) to model the KISS-only channel
// case introduced in Phase 2: a null value means the channel is not
// audio-backed and will be serviced exclusively by a KISS-TNC
// interface. When null, ModemType / BitRate / etc. are accepted but
// unused by the modem subprocess (see
// modembridge/session.go pushConfiguration). When non-null, the
// device must exist and have direction=input; configstore enforces
// that at write time.
type ChannelRequest struct {
	Name           string  `json:"name"`
	InputDeviceID  *uint32 `json:"input_device_id"`
	InputChannel   uint32  `json:"input_channel"`
	OutputDeviceID uint32  `json:"output_device_id"`
	OutputChannel  uint32  `json:"output_channel"`
	ModemType      string  `json:"modem_type"`
	BitRate        uint32  `json:"bit_rate"`
	MarkFreq       uint32  `json:"mark_freq"`
	SpaceFreq      uint32  `json:"space_freq"`
	Profile        string  `json:"profile"`
	NumSlicers     uint32  `json:"num_slicers"`
	FixBits        string  `json:"fix_bits"`
	FX25Encode     bool    `json:"fx25_encode"`
	IL2PEncode     bool    `json:"il2p_encode"`
	NumDecoders    uint32  `json:"num_decoders"`
	DecoderOffset  int32   `json:"decoder_offset"`
}

// Validate ensures required fields are set. Deep validation (device
// existence, channel range) still runs inside configstore.
//
// InputDeviceID follows the Phase 2 nullable contract:
//   - nil  → KISS-only channel; OutputDeviceID must be 0 (no TX audio
//     without RX audio).
//   - non-nil → modem-backed channel; device existence + direction is
//     validated by configstore.validateChannel.
func (r ChannelRequest) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if r.InputDeviceID == nil {
		// KISS-only channel: no audio devices allowed.
		if r.OutputDeviceID != 0 {
			return fmt.Errorf("output_device_id must be 0 when input_device_id is null (KISS-only channel)")
		}
	} else if *r.InputDeviceID == 0 {
		// An explicit 0 from a client that didn't migrate to the
		// nullable shape yet is ambiguous — reject with a clear
		// error rather than silently treating it as "KISS-only"
		// (the client probably meant "missing, please default").
		return fmt.Errorf("input_device_id must be null or a valid device id, not 0")
	}
	if r.ModemType == "" {
		return fmt.Errorf("modem_type is required")
	}
	return nil
}

// ToModel maps a create request to a storage model.
func (r ChannelRequest) ToModel() configstore.Channel {
	return configstore.Channel{
		Name:           r.Name,
		InputDeviceID:  r.InputDeviceID,
		InputChannel:   r.InputChannel,
		OutputDeviceID: r.OutputDeviceID,
		OutputChannel:  r.OutputChannel,
		ModemType:      r.ModemType,
		BitRate:        r.BitRate,
		MarkFreq:       r.MarkFreq,
		SpaceFreq:      r.SpaceFreq,
		Profile:        r.Profile,
		NumSlicers:     r.NumSlicers,
		FixBits:        r.FixBits,
		FX25Encode:     r.FX25Encode,
		IL2PEncode:     r.IL2PEncode,
		NumDecoders:    r.NumDecoders,
		DecoderOffset:  r.DecoderOffset,
	}
}

// ToUpdate maps an update request to a storage model, preserving id.
func (r ChannelRequest) ToUpdate(id uint32) configstore.Channel {
	m := r.ToModel()
	m.ID = id
	return m
}

// ChannelResponse is the body returned by GET/POST/PUT for a channel.
//
// Backing is a computed, read-only object that tells the UI where a
// frame submitted on this channel will actually go (see design decision
// D7 in .context/2026-04-20-kiss-tcp-client-and-channel-backing.md).
// Empty in POST/PUT round-trips because the store model carries no
// backing state of its own; populated only by list/get handlers that
// have access to the running modembridge + kiss manager. Omitted from
// JSON when zero so create/update response bodies don't carry stale
// "unbound" placeholders.
type ChannelResponse struct {
	ID      uint32          `json:"id"`
	Backing *ChannelBacking `json:"backing,omitempty"`
	ChannelRequest
}

// ChannelBacking describes the runtime backing — modem and/or KISS-TNC
// interfaces — attached to a channel. Computed at request time from
// store + kissMgr.Status() + modembridge.SessionStatus().
//
// Summary is one of "modem", "kiss-tnc", or "unbound". Dual-backend is
// forbidden at the config layer (D3) so this is always a single value.
//
// Health is one of "live" (≥1 backend instance is up), "down" (backends
// exist but all are down), or "unbound" (no backend configured).
//
// Tx is the channel's TX-capability signal used by the beacon / iGate /
// digipeater picker predicate and by the server-side referrer validator.
// It is derived from the same underlying fields as Summary + Health but
// answers a different question: "can a frame submitted on this channel
// actually be transmitted?" See TxCapability's contract for the
// single-branch decision rule and the Reason invariant.
type ChannelBacking struct {
	Modem   ChannelModemBacking   `json:"modem"`
	KissTnc []ChannelKissTncEntry `json:"kiss_tnc"`
	Summary string                `json:"summary"`
	Health  string                `json:"health"`
	Tx      TxCapability          `json:"tx"`
}

// TxCapability reports whether a channel can currently transmit.
//
// Capable is true when the channel has at least one usable TX path:
//   - ≥ 1 TNC-mode KissInterface attached (the KISS path short-circuits
//     first, so a KISS-only channel with InputDeviceID == nil still
//     reports Capable=true, not "no input device configured"), OR
//   - a modem backing with both InputDeviceID != nil AND
//     OutputDeviceID != 0 (the zero-sentinel on OutputDeviceID marks
//     RX-only modem configs; those cannot TX even though
//     Backing.Summary is "modem").
//
// Reason is a short human-legible explanation of why Capable is false
// (e.g. "no input device configured", "no output device configured").
//
// Contract: Reason is the empty string if and only if Capable == true.
// Callers may rely on this invariant — don't overload Reason with a
// hint when Capable is true, and don't leave Reason empty when Capable
// is false. The reason strings are stable wire values (consumed by the
// UI picker's disabled-option secondary text and the server-side 400
// body on referrer writes), so treat them as API surface.
type TxCapability struct {
	Capable bool   `json:"capable"`
	Reason  string `json:"reason,omitempty"`
}

// TX-capability reason strings. Exported so tests and the validator
// callers can assert against them without re-stringing the literal.
const (
	TxReasonNoInputDevice  = "no input device configured"
	TxReasonNoOutputDevice = "no output device configured"
)

// ChannelModemBacking reports whether an audio modem currently serves
// this channel. Active is true when the channel has a bound input audio
// device and the modem subprocess is running. Reason is populated when
// Active is false; empty otherwise.
type ChannelModemBacking struct {
	Active bool   `json:"active"`
	Reason string `json:"reason,omitempty"`
}

// ChannelKissTncEntry is one TNC-mode KISS interface attached to the
// channel. AllowTxFromGovernor reflects KissInterface.AllowTxFromGovernor —
// Phase 3's opt-in flag gating governor-originated TX fan-out to this
// interface.
//
// State / LastError / RetryAtUnixMs are best-effort today: Phase 1 only
// supports server-listen interfaces, which expose a state of "listening"
// (or "stopped" when not running) with no error or retry timestamp.
// Phase 4 fills these in for tcp-client interfaces. Fields are
// pre-declared now so the JSON contract doesn't shift between phases.
type ChannelKissTncEntry struct {
	InterfaceID         uint32 `json:"interface_id"`
	InterfaceName       string `json:"interface_name"`
	AllowTxFromGovernor bool   `json:"allow_tx_from_governor"`
	State               string `json:"state"`
	LastError           string `json:"last_error,omitempty"`
	RetryAtUnixMs       int64  `json:"retry_at_unix_ms,omitempty"`
}

// Backing summary values.
const (
	ChannelBackingSummaryModem   = "modem"
	ChannelBackingSummaryKissTnc = "kiss-tnc"
	ChannelBackingSummaryUnbound = "unbound"
)

// Backing health values.
const (
	ChannelBackingHealthLive    = "live"
	ChannelBackingHealthDown    = "down"
	ChannelBackingHealthUnbound = "unbound"
)

// ChannelFromModel converts a storage model into a response DTO. The
// Backing field is left nil — list/get handlers populate it after the
// base mapping using the live kiss manager and modem bridge status.
//
// InputDeviceID is copied as-is (both sides are *uint32). A nil
// pointer round-trips as JSON null, which the TS client surfaces as
// `input_device_id: null` — the segmented type picker on the
// Channels page treats that as "KISS-TNC only".
func ChannelFromModel(m configstore.Channel) ChannelResponse {
	return ChannelResponse{
		ID: m.ID,
		ChannelRequest: ChannelRequest{
			Name:           m.Name,
			InputDeviceID:  m.InputDeviceID,
			InputChannel:   m.InputChannel,
			OutputDeviceID: m.OutputDeviceID,
			OutputChannel:  m.OutputChannel,
			ModemType:      m.ModemType,
			BitRate:        m.BitRate,
			MarkFreq:       m.MarkFreq,
			SpaceFreq:      m.SpaceFreq,
			Profile:        m.Profile,
			NumSlicers:     m.NumSlicers,
			FixBits:        m.FixBits,
			FX25Encode:     m.FX25Encode,
			IL2PEncode:     m.IL2PEncode,
			NumDecoders:    m.NumDecoders,
			DecoderOffset:  m.DecoderOffset,
		},
	}
}

// ChannelsFromModels maps a slice for list responses.
func ChannelsFromModels(ms []configstore.Channel) []ChannelResponse {
	out := make([]ChannelResponse, len(ms))
	for i, m := range ms {
		out[i] = ChannelFromModel(m)
	}
	return out
}
