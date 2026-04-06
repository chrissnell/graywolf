package igate

import (
	"context"
	"errors"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// IgateInput exposes IS->RF frames as an aprs.PacketInput. Consumers
// (e.g. an audit logger or secondary TX path) can drain it with
// RecvPacket; frames are also submitted directly through the TX
// governor by the iGate itself, so IgateInput is optional.
type IgateInput struct {
	ig *Igate
}

// NewIgateInput returns a PacketInput bound to ig.
func NewIgateInput(ig *Igate) *IgateInput {
	return &IgateInput{ig: ig}
}

// RecvPacket blocks until an IS->RF frame is available or ctx is done.
func (i *IgateInput) RecvPacket(ctx context.Context) (*aprs.InboundPacket, error) {
	if i == nil || i.ig == nil {
		return nil, errors.New("igate: nil input")
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case p, ok := <-i.ig.inputCh:
		if !ok {
			return nil, errors.New("igate: input channel closed")
		}
		return p, nil
	}
}

// Close drops the reference; the channel itself is owned by Igate.
func (i *IgateInput) Close() error { return nil }

// Compile-time assertion.
var _ aprs.PacketInput = (*IgateInput)(nil)
