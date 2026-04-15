package dto

import (
	"fmt"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// ChannelRequest is the body accepted by POST /api/channels and
// PUT /api/channels/{id}.
type ChannelRequest struct {
	Name           string `json:"name"`
	InputDeviceID  uint32 `json:"input_device_id"`
	InputChannel   uint32 `json:"input_channel"`
	OutputDeviceID uint32 `json:"output_device_id"`
	OutputChannel  uint32 `json:"output_channel"`
	ModemType      string `json:"modem_type"`
	BitRate        uint32 `json:"bit_rate"`
	MarkFreq       uint32 `json:"mark_freq"`
	SpaceFreq      uint32 `json:"space_freq"`
	Profile        string `json:"profile"`
	NumSlicers     uint32 `json:"num_slicers"`
	FixBits        string `json:"fix_bits"`
	FX25Encode     bool   `json:"fx25_encode"`
	IL2PEncode     bool   `json:"il2p_encode"`
	NumDecoders    uint32 `json:"num_decoders"`
	DecoderOffset  int32  `json:"decoder_offset"`
}

// Validate ensures required fields are set. Deep validation (device
// existence, channel range) still runs inside configstore.
func (r ChannelRequest) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("name is required")
	}
	if r.InputDeviceID == 0 {
		return fmt.Errorf("input_device_id is required")
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
type ChannelResponse struct {
	ID uint32 `json:"id"`
	ChannelRequest
}

// ChannelFromModel converts a storage model into a response DTO.
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
