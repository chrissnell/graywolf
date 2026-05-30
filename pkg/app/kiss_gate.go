package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

// decodeAprsForGate parses an AX.25 frame as APRS and tags the result
// with the originating channel + DirectionRF so the iGate's RF→IS gate
// can apply its filter chain (NOGATE / RFONLY / TCPIP markers, operator
// filter rules) as if the packet had been heard off the air. Returns
// nil when f is nil, not a UI frame, or carries an info field that
// doesn't parse as APRS (e.g. AX.25 connected-mode payloads, NET/ROM,
// non-APRS experiments) — those legitimately don't belong on APRS-IS
// even if the operator's KISS client happens to submit them.
//
// Split out from kissClientTxGateToIs so tests can assert the parse +
// tag work directly (Channel + Direction) without standing up a real
// *Igate.
func decodeAprsForGate(channel uint32, f *ax25.Frame) *aprs.DecodedAPRSPacket {
	if f == nil || !f.IsUI() {
		return nil
	}
	pkt, err := aprs.Parse(f)
	if err != nil || pkt == nil {
		return nil
	}
	pkt.Channel = int(channel)
	pkt.Direction = aprs.DirectionRF
	return pkt
}

// kissClientTxGateToIs is the per-interface OnClientTxAccepted hook
// the kiss.Manager invokes for each KISS frame accepted by Sink.Submit
// on an interface that has GateTxToIs set. The hook offers the parsed
// APRS packet to the iGate's RF→IS path, bypassing the messages
// router / Actions classifier / station cache / digipeater — those
// surfaces exist to handle heard traffic, and a frame the operator is
// transmitting is not heard traffic. The iGate's filter chain
// (NOGATE / RFONLY / TCPIP path markers + operator filter rules) is
// applied unchanged inside IgateOutput.SendPacket.
//
// Blocking contract: runs on the kiss.Server per-connection read
// goroutine. IgateOutput.SendPacket → Igate.gateRFToIS → client.WriteLine
// writes to the APRS-IS TCP socket with a 10s write deadline, so a
// stalled iGate connection can delay the next KISS frame by up to
// that long. Acceptable for the single-station target (the existing
// aprsQueue→fanout path has the same bound) but worth re-evaluating
// if a higher-volume call site ever lands here.
//
// ifaceID is unused today but reserved for future per-interface
// metrics labeling so the API doesn't need a breaking change later.
func (a *App) kissClientTxGateToIs(ctx context.Context, ifaceID, channel uint32, f *ax25.Frame) {
	_ = ifaceID
	if a == nil || a.igateOut == nil {
		return
	}
	pkt := decodeAprsForGate(channel, f)
	if pkt == nil {
		return
	}
	_ = a.igateOut.SendPacket(ctx, pkt)
}
