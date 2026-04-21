package txbackend

import (
	"context"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

// Backend is one TX sink attached to a channel. Implementations:
//
//   - ModemBackend: wraps modembridge.Bridge.SendTransmitFrame. One per
//     graywolf instance; attached to every channel with a bound input
//     audio device.
//   - KissTncBackend: wraps kiss.Manager.TransmitOnChannel for a single
//     KissInterface row with Mode=tnc AND AllowTxFromGovernor=true.
//
// Submit is expected to be fast and non-blocking on the hot path:
// ModemBackend forwards over an in-process IPC channel; KissTncBackend
// does a non-blocking enqueue into a per-instance bounded queue.
// Slow work (socket write, subprocess IPC ack) happens on a separate
// goroutine owned by the backend's implementation.
//
// Close is called during shutdown (step 3 in the package godoc). It
// must be idempotent and respect ctx cancellation. No-op is valid for
// backends whose underlying transport owns its own lifecycle (the
// ModemBackend is a thin wrapper and returns nil immediately).
type Backend interface {
	// Submit hands tf to the backend's transport. Returns ErrBackendBusy
	// if the backend's internal queue is momentarily full, ErrBackendDown
	// if the transport is known-disconnected, or an opaque transport
	// error otherwise. Must not block on slow I/O (writes run on a
	// separate goroutine).
	Submit(ctx context.Context, tf *pb.TransmitFrame) error
	// Name identifies the backend kind for metrics: "modem" or "kiss".
	Name() string
	// InstanceID is a per-backend string that uniquely identifies this
	// instance across the process. For ModemBackend this is "modem";
	// for KissTncBackend it is "kiss-<interfaceID>". Used as the
	// `instance` label on dispatcher metrics.
	InstanceID() string
	// AttachedChannels returns the channel IDs this backend serves.
	// Read once during snapshot rebuild; the backend must not mutate
	// its attached set after being handed to the dispatcher.
	AttachedChannels() []uint32
	// Close releases any resources owned by this backend (bounded
	// queue drain, goroutine shutdown). Idempotent. Must return when
	// ctx is cancelled even if resources remain. Most backends delegate
	// to upstream lifecycle objects (kiss.Manager, modembridge) whose
	// own shutdown is sequenced by the wiring layer.
	Close(ctx context.Context) error
}
