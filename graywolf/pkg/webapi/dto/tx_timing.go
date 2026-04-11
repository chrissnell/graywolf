package dto

import "github.com/chrissnell/graywolf/pkg/configstore"

// TxTimingRequest is the body accepted by POST /api/tx-timing and
// PUT /api/tx-timing/{channel}.
type TxTimingRequest struct {
	Channel   uint32 `json:"channel"`
	TxDelayMs uint32 `json:"tx_delay_ms"`
	TxTailMs  uint32 `json:"tx_tail_ms"`
	SlotMs    uint32 `json:"slot_ms"`
	Persist   uint32 `json:"persist"`
	FullDup   bool   `json:"full_dup"`
	Rate1Min  uint32 `json:"rate_1min"`
	Rate5Min  uint32 `json:"rate_5min"`
}

func (r TxTimingRequest) Validate() error { return nil }

func (r TxTimingRequest) ToModel() configstore.TxTiming {
	return configstore.TxTiming{
		Channel:   r.Channel,
		TxDelayMs: r.TxDelayMs,
		TxTailMs:  r.TxTailMs,
		SlotMs:    r.SlotMs,
		Persist:   r.Persist,
		FullDup:   r.FullDup,
		Rate1Min:  r.Rate1Min,
		Rate5Min:  r.Rate5Min,
	}
}

// ToUpdate maps an update request to a storage model, pinning the
// channel from the URL instead of the body.
func (r TxTimingRequest) ToUpdate(channel uint32) configstore.TxTiming {
	m := r.ToModel()
	m.Channel = channel
	return m
}

type TxTimingResponse struct {
	ID uint32 `json:"id"`
	TxTimingRequest
}

func TxTimingFromModel(m configstore.TxTiming) TxTimingResponse {
	return TxTimingResponse{
		ID: m.ID,
		TxTimingRequest: TxTimingRequest{
			Channel:   m.Channel,
			TxDelayMs: m.TxDelayMs,
			TxTailMs:  m.TxTailMs,
			SlotMs:    m.SlotMs,
			Persist:   m.Persist,
			FullDup:   m.FullDup,
			Rate1Min:  m.Rate1Min,
			Rate5Min:  m.Rate5Min,
		},
	}
}

func TxTimingsFromModels(ms []configstore.TxTiming) []TxTimingResponse {
	out := make([]TxTimingResponse, len(ms))
	for i, m := range ms {
		out[i] = TxTimingFromModel(m)
	}
	return out
}
