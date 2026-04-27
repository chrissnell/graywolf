// Package txbackend contains the per-channel TX dispatcher that replaces
// the single modem Sender function graywolf used before Phase 3 of the
// KISS TCP-client / channel-backing plan.
//
// Architectural summary:
//
//   Governor.Submit → Dispatcher.Send(tf) → Registry.Load() →
//     ByChannel[tf.Channel] → fanout to every Backend (Modem or
//     KissTnc) → errors.Join(...) → nil if any backend accepted.
//
// Registry is an immutable snapshot published via atomic.Pointer[T].
// A single watcher goroutine (owned by the Dispatcher) rebuilds the
// snapshot off the hot path in response to config-change events
// (notifyBridgeReload + KISS manager add/remove/mode-flip events) and
// publishes with one atomic.Store. Dispatcher.Send does one
// atomic.Load per frame — no locking on the hot path.
//
// Connection health is deliberately NOT part of the registry. A
// KissTnc backend whose supervisor is in backoff stays registered;
// the Submit call to kiss.Manager returns ErrBackendDown and the
// Dispatcher records that outcome per-instance. This separates config
// (which backends exist) from runtime (which backends are up) and
// keeps hot-path snapshot swaps rare.
//
// Fanout semantics: a channel may have multiple KissTnc instances
// (e.g. two remote TNCs peering the same logical channel). All
// instances receive every frame; per-instance outcomes are recorded
// independently via metrics, and Dispatcher.Send returns nil iff at
// least one instance accepted. Governor.Sent++ is incremented once
// per frame (it is a submission counter, not an airtime counter);
// per-backend emissions live in the dispatcher's own counter.
//
// Shutdown order (see pkg/app/wiring.go Stop path):
//
//  1. Governor.Drain(ctx)       — stop accepting new Submits.
//  2. Dispatcher.StopAccepting()— refuse new Send calls.
//  3. Backends Close(ctx)       — parallel (errgroup).
//  4. kiss.Manager.Stop()       — cancels per-instance supervisors and queues.
//  5. modembridge.Stop()        — kills the Rust subprocess.
//
// Every long-running goroutine in this package (just the watcher)
// exits on ctx cancellation; tests use goleak.VerifyNone(t) at teardown.
package txbackend

import "errors"

// ErrNoBackend is returned by Dispatcher.Send when the channel has no
// registered backend. The submission is dropped and the per-channel
// graywolf_tx_no_backend_total counter increments.
var ErrNoBackend = errors.New("txbackend: no backend registered for channel")

// ErrStopped is returned by Dispatcher.Send after StopAccepting has
// been called. Terminal — callers (Governor) must drop the frame and
// cease submitting.
var ErrStopped = errors.New("txbackend: dispatcher stopped")

// ErrBackendBusy is returned by a Backend.Submit when the backend's
// internal bounded queue cannot accept the frame without blocking. The
// Dispatcher records this as outcome=backend_busy for per-instance
// visibility. Non-terminal — the backend remains healthy.
var ErrBackendBusy = errors.New("txbackend: backend busy")

// ErrBackendDown is returned by a Backend.Submit when the backend is
// known-disconnected (e.g. KISS tcp-client supervisor in backoff). The
// Dispatcher records this as outcome=backend_down. Non-terminal — a
// later frame may succeed after reconnect.
var ErrBackendDown = errors.New("txbackend: backend down")
