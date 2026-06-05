package igate

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// ErrNotEnabled is the sentinel returned by adapters that wrap a
// runtime-toggleable iGate (the IGateLineSender adapter passed to
// messages.Service, the simulation toggle closure registered with
// webapi.RegisterIgate, etc.) when the operator has the iGate disabled.
// Webapi handlers map this to 503 "igate not available" instead of a
// generic 500 so a deliberately-off iGate does not surface as an
// internal error in operator dashboards.
var ErrNotEnabled = errors.New("igate not enabled")

// IgateOutput adapts the iGate's RF->IS gating to the aprs.PacketOutput
// interface so it can be wired into the decoder's fanout alongside
// LogOutput and the packet log sink. The inner *Igate is held in an
// atomic pointer so it can be swapped at runtime when the operator
// toggles the iGate enable flag.
type IgateOutput struct {
	ig atomic.Pointer[Igate]
}

// NewIgateOutput returns a PacketOutput bound to ig. ig may be nil; the
// inner pointer can be replaced later via SetIgate.
func NewIgateOutput(ig *Igate) *IgateOutput {
	o := &IgateOutput{}
	if ig != nil {
		o.ig.Store(ig)
	}
	return o
}

// SetIgate swaps the inner *Igate. Pass nil to disable forwarding (used
// when the operator turns the iGate off at runtime).
func (o *IgateOutput) SetIgate(ig *Igate) {
	if o == nil {
		return
	}
	o.ig.Store(ig)
}

// SendPacket feeds a decoded RF packet into the iGate for possible
// forwarding to APRS-IS. Always returns nil — gating errors are logged
// internally and counted in metrics; they are not caller-visible.
func (o *IgateOutput) SendPacket(_ context.Context, pkt *aprs.DecodedAPRSPacket) error {
	if o == nil {
		return nil
	}
	ig := o.ig.Load()
	if ig == nil {
		return nil
	}
	_ = ig.gateRFToIS(pkt)
	return nil
}

// GateClientTx runs the same RF→IS gate pipeline as SendPacket and
// returns the outcome reason so the caller can log per-packet
// visibility. Used by the kiss-modem gate hook in pkg/app to surface
// which branch fired for a connected KISS client's transmission. When
// the iGate is disabled or the output is uninitialized, returns the
// sentinel reasons "no-output" / "igate-disabled" rather than a real
// gating outcome.
func (o *IgateOutput) GateClientTx(_ context.Context, pkt *aprs.DecodedAPRSPacket) GateReason {
	if o == nil {
		return GateReason("no-output")
	}
	ig := o.ig.Load()
	if ig == nil {
		return GateReason("igate-disabled")
	}
	return ig.gateRFToIS(pkt)
}

// Close is a no-op; the iGate itself owns its lifecycle.
func (o *IgateOutput) Close() error { return nil }

// Compile-time assertion.
var _ aprs.PacketOutput = (*IgateOutput)(nil)
