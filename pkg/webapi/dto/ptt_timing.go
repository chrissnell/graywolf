package dto

import "github.com/chrissnell/graywolf/pkg/configstore"

// PttTimingRequest is the body accepted by PUT /api/ptt-timing. It carries
// the global PTT keying timing applied to every transmitting channel.
type PttTimingRequest struct {
	TxDelayMs uint32 `json:"tx_delay_ms"`
	TxTailMs  uint32 `json:"tx_tail_ms"`
}

func (r PttTimingRequest) Validate() error { return nil }

// ApplyToModel merges the request onto the existing stored singleton,
// consistent with the replace-style PUT pattern used elsewhere in webapi.
func (r PttTimingRequest) ApplyToModel(existing configstore.PttTiming) configstore.PttTiming {
	existing.TxDelayMs = r.TxDelayMs
	existing.TxTailMs = r.TxTailMs
	return existing
}

type PttTimingResponse struct {
	ID        uint32 `json:"id"`
	TxDelayMs uint32 `json:"tx_delay_ms"`
	TxTailMs  uint32 `json:"tx_tail_ms"`
}

func PttTimingFromModel(m configstore.PttTiming) PttTimingResponse {
	return PttTimingResponse{
		ID:        m.ID,
		TxDelayMs: m.TxDelayMs,
		TxTailMs:  m.TxTailMs,
	}
}
