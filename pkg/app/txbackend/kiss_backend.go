package txbackend

import (
	"context"
	"fmt"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// KissInstanceSender is the minimal surface a KissTncBackend needs
// from the per-interface queue owned by kiss.Manager. The concrete
// implementation lives in pkg/kiss (instanceTxQueue); the indirection
// keeps txbackend free of circular dependencies on kiss internals and
// makes test stubs trivial.
type KissInstanceSender interface {
	// Enqueue does a non-blocking send of (frame, frameID) onto the
	// per-instance bounded queue. Returns ErrBackendBusy when the
	// queue is full, ErrBackendDown when the writer goroutine has
	// stopped, or an opaque transport error otherwise.
	Enqueue(frame []byte, frameID uint64) error
}

// KissTncBackend is the per-interface TX backend for a KissInterface
// row with Mode=tnc and AllowTxFromGovernor=true. One instance per
// eligible interface; a channel that has two such interfaces attached
// gets two independent KissTncBackend instances in the snapshot so
// the dispatcher fans out to both.
type KissTncBackend struct {
	sender      KissInstanceSender
	channel     uint32 // single channel per KissInterface row
	interfaceID uint32
}

// NewKissTncBackend constructs a backend for one KissInterface.
// interfaceID is the DB row ID used to build the instance label and
// to correlate with kiss.Manager.Status(). channel is the single
// channel this interface services.
func NewKissTncBackend(sender KissInstanceSender, interfaceID, channel uint32) *KissTncBackend {
	return &KissTncBackend{sender: sender, channel: channel, interfaceID: interfaceID}
}

// Submit hands off to the per-instance tx queue. The queue's writer
// goroutine performs the actual socket write; Submit never blocks on
// slow peers.
func (k *KissTncBackend) Submit(_ context.Context, tf *pb.TransmitFrame) error {
	// tf.Data is the raw AX.25 frame (FCS appended by encode). The
	// instance queue wraps it in KISS framing and writes to every
	// connected client (server-listen mode) or the single dialed
	// peer (tcp-client mode, Phase 4).
	return k.sender.Enqueue(tf.Data, tf.FrameId)
}

// Name returns the metric label for this backend kind.
func (k *KissTncBackend) Name() string { return "kiss" }

// InstanceID returns the per-interface identifier used as the
// `instance` metric label so operators can attribute drops to a
// specific KissInterface row.
func (k *KissTncBackend) InstanceID() string {
	return fmt.Sprintf("kiss-%d", k.interfaceID)
}

// AttachedChannels returns the single channel this interface serves.
func (k *KissTncBackend) AttachedChannels() []uint32 { return []uint32{k.channel} }

// Close is a no-op at the txbackend layer. The per-instance queue's
// writer goroutine is owned by kiss.Manager, which cancels it via
// the ctx passed to Manager.Start when the wiring layer's kissComponent
// stop runs. Duplicating the teardown here would race Manager.Stop.
func (k *KissTncBackend) Close(context.Context) error { return nil }
