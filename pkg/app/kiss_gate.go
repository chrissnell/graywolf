package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
)

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
// Non-blocking by contract: runs on the kiss.Server per-connection
// read goroutine. The iGate's SendPacket is also non-blocking (it
// hands off to the iGate's internal channel and returns) so this
// inherits the right semantics.
//
// ifaceID is unused today but reserved for future per-interface
// metrics labeling so the API doesn't need a breaking change later.
func (a *App) kissClientTxGateToIs(ctx context.Context, ifaceID, channel uint32, f *ax25.Frame) {
	_ = ifaceID
	if a == nil || a.igateOut == nil || f == nil {
		return
	}
	if !f.IsUI() {
		return
	}
	pkt, err := aprs.Parse(f)
	if err != nil || pkt == nil {
		return
	}
	pkt.Channel = int(channel)
	pkt.Direction = aprs.DirectionRF
	_ = a.igateOut.SendPacket(ctx, pkt)
}
