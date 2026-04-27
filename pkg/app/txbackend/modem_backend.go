package txbackend

import (
	"context"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// ModemSender is the minimal surface the ModemBackend needs from
// modembridge.Bridge. Injection point keeps the package testable.
type ModemSender interface {
	SendTransmitFrame(*pb.TransmitFrame) error
}

// ModemBackend is the single TX backend that routes governor-scheduled
// frames into the Rust modem subprocess via modembridge. One instance
// per graywolf process; attached to every channel whose configstore
// row has a non-nil InputDeviceID.
type ModemBackend struct {
	sender   ModemSender
	channels []uint32
}

// NewModemBackend constructs a backend servicing the given channel IDs.
// channels should be the complete set of channels with a bound input
// audio device at snapshot-build time; a new backend is constructed
// on each registry rebuild so the membership is always current.
func NewModemBackend(sender ModemSender, channels []uint32) *ModemBackend {
	// Defensive copy so the caller's slice can be reused.
	chs := make([]uint32, len(channels))
	copy(chs, channels)
	return &ModemBackend{sender: sender, channels: chs}
}

// Submit forwards tf to the modembridge. Errors from the bridge are
// returned verbatim — the dispatcher classifies them as OutcomeErr.
// Busy/Down outcomes don't apply to modembridge: it has a single IPC
// socket and no bounded queue; a dead subprocess surfaces as an IPC
// error which is recorded as a generic err.
func (m *ModemBackend) Submit(_ context.Context, tf *pb.TransmitFrame) error {
	return m.sender.SendTransmitFrame(tf)
}

// Name returns the metric label for this backend kind.
func (m *ModemBackend) Name() string { return "modem" }

// InstanceID returns a process-wide-unique identifier for this
// backend. There is only ever one modem backend, so the literal
// "modem" is fine — per-channel labelling already lives in the
// `channel` metric label.
func (m *ModemBackend) InstanceID() string { return "modem" }

// AttachedChannels returns the channel IDs this backend serves.
func (m *ModemBackend) AttachedChannels() []uint32 { return m.channels }

// Close is a no-op. modembridge owns its own subprocess lifecycle and
// is torn down by the wiring layer's bridgeComponent.stop.
func (m *ModemBackend) Close(context.Context) error { return nil }
