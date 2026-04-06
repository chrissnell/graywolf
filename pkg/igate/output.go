package igate

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// IgateOutput adapts the iGate's RF->IS gating to the aprs.PacketOutput
// interface so it can be wired into the decoder's fanout alongside
// LogOutput and the packet log sink.
type IgateOutput struct {
	ig *Igate
}

// NewIgateOutput returns a PacketOutput bound to ig.
func NewIgateOutput(ig *Igate) *IgateOutput {
	return &IgateOutput{ig: ig}
}

// SendPacket feeds a decoded RF packet into the iGate for possible
// forwarding to APRS-IS. Always returns nil — gating errors are logged
// internally and counted in metrics; they are not caller-visible.
func (o *IgateOutput) SendPacket(_ context.Context, pkt *aprs.DecodedAPRSPacket) error {
	if o == nil || o.ig == nil {
		return nil
	}
	o.ig.gateRFToIS(pkt)
	return nil
}

// Close is a no-op; the iGate itself owns its lifecycle.
func (o *IgateOutput) Close() error { return nil }

// Compile-time assertion.
var _ aprs.PacketOutput = (*IgateOutput)(nil)
