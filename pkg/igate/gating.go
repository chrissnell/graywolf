package igate

import (
	"github.com/chrissnell/graywolf/pkg/aprs"
)

// LocalOriginRing abstracts the (source, msg_id) lookup the iGate
// needs from pkg/messages.LocalTxRing. Pkg/igate MUST NOT import
// pkg/messages directly — this narrow interface is the contract.
//
// *messages.LocalTxRing satisfies this trivially via its
// Contains(source, msgID) method.
type LocalOriginRing interface {
	Contains(source, msgID string) bool
}

// shouldSuppressLocalMessage reports whether pkt looks like an RF
// re-heard copy of a message we originated. When ig.cfg.LocalOrigin
// is non-nil and ig.cfg.SuppressLocalMessageReGate is true, a hit in
// the ring short-circuits RF→IS gating so the operator's own outbound
// doesn't get re-uploaded to APRS-IS via the digipeater repeat.
//
// Non-message packets and packets without a MessageID always return
// false (no suppression) — the ring is keyed on (source, msg_id)
// and both parts must be present for a meaningful match.
func (ig *Igate) shouldSuppressLocalMessage(pkt *aprs.DecodedAPRSPacket) bool {
	if ig == nil || ig.cfg.LocalOrigin == nil {
		return false
	}
	if !ig.cfg.SuppressLocalMessageReGate {
		return false
	}
	if pkt == nil || pkt.Message == nil {
		return false
	}
	if pkt.Message.MessageID == "" {
		return false
	}
	return ig.cfg.LocalOrigin.Contains(pkt.Source, pkt.Message.MessageID)
}
