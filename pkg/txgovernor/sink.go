package txgovernor

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

// TxSink is implemented by anything that accepts AX.25 frames for
// transmit scheduling. The canonical implementation is *Governor;
// callers that need only to submit frames should depend on this
// interface rather than the concrete type so tests can inject a fake
// without reaching for a package-local interface copy.
//
// Every production call site funnels through txgovernor.Governor.
// Previously each caller (kiss, agw, beacon, igate) declared its own
// TxSink + SubmitSource types duplicating this signature, which meant
// a change to the governor's Submit contract could silently drift
// away from the duplicates until a compile error finally surfaced in
// the adapter layer. Depending on this type eliminates that risk.
type TxSink interface {
	Submit(ctx context.Context, channel uint32, frame *ax25.Frame, src SubmitSource) error
}

// Compile-time assertion that *Governor implements TxSink.
var _ TxSink = (*Governor)(nil)

// TxHookRegistry is the narrow hook-registration interface. Callers
// that only need to register/unregister a TxHook should depend on
// this interface rather than the concrete *Governor so tests can
// inject a fake. The returned unregister closure is idempotent.
type TxHookRegistry interface {
	AddTxHook(h TxHook) (id uint64, unregister func())
}

// Compile-time assertion that *Governor implements TxHookRegistry.
var _ TxHookRegistry = (*Governor)(nil)
