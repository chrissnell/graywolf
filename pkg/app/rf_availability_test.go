package app

import (
	"context"
	"testing"

	"github.com/chrissnell/graywolf/pkg/app/txbackend"
	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// fakeBackend lets us populate the dispatcher snapshot with a backend
// of an arbitrary Name(). Submit / Close / AttachedChannels are
// inert — only Name() and InstanceID() are exercised by the adapter.
type fakeBackend struct {
	name     string
	channels []uint32
}

func (f fakeBackend) Submit(context.Context, *pb.TransmitFrame) error { return nil }
func (f fakeBackend) Name() string                                    { return f.name }
func (f fakeBackend) InstanceID() string                              { return f.name + "-0" }
func (f fakeBackend) AttachedChannels() []uint32                      { return f.channels }
func (f fakeBackend) Close(context.Context) error                     { return nil }

// TestRFAvailabilityAdapter_KissOnlyChannel pins the issue #81 fix:
// the messages sender must see RF as available for a KISS-only channel
// even when the modem subprocess is not running.
func TestRFAvailabilityAdapter_KissOnlyChannel(t *testing.T) {
	reg := txbackend.NewRegistry()
	snap := &txbackend.Snapshot{
		ByChannel: map[uint32][]txbackend.Backend{
			1: {fakeBackend{name: txbackend.BackendNameKiss, channels: []uint32{1}}},
		},
		CsmaSkip: map[uint32]bool{1: true},
	}
	reg.Publish(snap)

	a := rfAvailabilityAdapter{bridge: nil, reg: reg}
	if !a.IsRunningForChannel(1) {
		t.Fatal("KISS-only channel must report available with no modem bridge")
	}
	if a.IsRunningForChannel(2) {
		t.Fatal("channel with no backend must report unavailable")
	}
}

// TestRFAvailabilityAdapter_ModemOnlyChannel verifies the legacy
// modem-only path still gates on bridge.IsRunning(): if the bridge is
// nil (or down) and only a modem backend is registered, the channel
// is unavailable so the IS fallback can fire immediately.
func TestRFAvailabilityAdapter_ModemOnlyChannel(t *testing.T) {
	reg := txbackend.NewRegistry()
	snap := &txbackend.Snapshot{
		ByChannel: map[uint32][]txbackend.Backend{
			1: {fakeBackend{name: txbackend.BackendNameModem, channels: []uint32{1}}},
		},
	}
	reg.Publish(snap)

	a := rfAvailabilityAdapter{bridge: nil, reg: reg}
	if a.IsRunningForChannel(1) {
		t.Fatal("modem-only channel with nil bridge must report unavailable")
	}
}

// TestRFAvailabilityAdapter_NoRegistry covers the construction order
// where the dispatcher hasn't been wired yet (or test rigs that pass
// nil): the adapter must not panic and must report unavailable.
func TestRFAvailabilityAdapter_NoRegistry(t *testing.T) {
	a := rfAvailabilityAdapter{bridge: nil, reg: nil}
	if a.IsRunningForChannel(1) {
		t.Fatal("nil registry must report unavailable")
	}
}

