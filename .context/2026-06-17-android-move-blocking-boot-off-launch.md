# Design: move the blocking backend boot off the launch path (graywolf#315)

Status: implemented on `feature/android-async-backend-boot`.

This is Design v2 -- it supersedes the original by folding in two
stop-during-boot corrections. The problem statement, constraints, and the
split are unchanged from v1; Corrections 1 and 2 are the additions.

## Problem

`GraywolfService` shares the app's one main/UI thread with `MainActivity`
(no `android:process` on the `<service>`). `Service.onCreate` runs on that
main thread and, after the fast `startForeground`, performs two synchronous
blocking waits:

- `bootModem()` -> `ModemBridge.modemAwaitReady(10_000)`
  (`GraywolfService.kt`, `bootModem` at ~:151) -- up to 10s
- `bootGoChild()` -> `GoLauncher.startAndAwaitReady(10_000)` = `gate.wait(10_000)`
  (`GoLauncher.kt:57`) -- up to 10s

If the modem cdylib and the Go child don't both reach readiness inside
Android's ANR window (~5s), the main thread is pinned, the WebView can't
paint (the white screen), and the OS kills the app, which drops the
foreground-service notification -- exactly the graywolf#315 report on the
Samsung/Android 11 device. Fast devices come up well under 5s and never hit
the wall; v0.14.0's live-radar/maps stack plausibly lengthened Go-child
startup vs v0.13.6, consistent with "0.13.6 flaky, 0.14.0 hard-fails."

Goal: make backend startup duration irrelevant to ANR by moving the blocking
waits off the main thread, preserving the startup ordering invariants.

## Constraints (these shape the split)

1. `startForeground` must run synchronously on the main thread within 5s of
   `startForegroundService`. It already does, before any blocking work --
   keep it there.
2. Callbacks-before-boot: `installPttCallback` / `installAudioTxCallback`
   (and `modemVersion()`, which triggers `loadLibrary`) must complete before
   `bootModem`, because modem boot can fire TX/PTT callbacks
   (`GraywolfService.kt`, comment near :327).
3. `PlatformServer.start` must precede `bootGoChild` -- the Go child dials
   the platform socket on launch.
4. `GpsAdapter.start()` must run on a thread with a Looper -- it calls
   `requestLocationUpdates` and `registerGnssStatusCallback`
   (`GpsAdapter.kt:76,80`), both of which bind callbacks to the calling
   thread's Looper. It cannot go on a raw worker.
5. `UsbPttAdapter.init` must precede `onResume`'s `enumerate()`
   (`GraywolfService.kt`, comment near :336).

## The split: surgical, at the bootModem boundary

Keep everything up to and including the adapter wiring on the main thread in
`onCreate`, in its current order, and move only the genuinely blocking tail
onto a dedicated daemon worker.

Stays on the main thread in `onCreate`: notification channel + receivers,
`startForeground`, `modemVersion()`/callbacks/`AudioTxPump`,
`UsbPttAdapter.init`, the existing `graywolf-io-init` thread spawn,
`PlatformServer.start` + `BindContendedException` handling, `GpsAdapter.start`,
adapter wiring.

Moves to a new `graywolf-boot` daemon thread (the only blocking work):
`bootModem()`, `audioPump.start()`, `bootGoChild()` (still sets
`goListenerReady = true`), `supervisor.start { goLauncher?.process }`.

After the split the residual main-thread cost in `onCreate` is `loadLibrary`
+ a happy-path socket bind -- both sub-second. The 20s of awaits that caused
the ANR are gone. `MainActivity` already polls the `@Volatile`
`goListenerReady` flag asynchronously, so the WebView paints the moment the
worker flips it, with the UI thread never blocked. The idiom (raw
`kotlin.concurrent.thread`, mirroring the existing `graywolf-io-init` worker)
is chosen over coroutines/HandlerThread to keep the diff small and reviewable.

## Correction 1 -- GoLauncher owns its forked process (fixes orphan-on-interrupt)

The defect: in `bootGoChild`, `GoLauncher.startAndAwaitReady` calls
`pb.start()` (forking `libgraywolf.so`) before it blocks on `gate.wait`, but
the `goLauncher` field is only assigned after readiness succeeds. If
`onDestroy` interrupts the boot worker mid-`gate.wait`, the
`InterruptedException` propagated out uncaught, the worker unwound with the
field still `null`, and `onDestroy`'s `goLauncher?.stop()` no-oped -- the
forked process was orphaned.

Fix lives in `GoLauncher` so the invariant "if I forked it, I own its
teardown" is in one place. `startAndAwaitReady` now swallows the interrupt
(preserving interrupt status), destroys its own process on any non-ready exit
(timeout or interrupt), and returns `false` instead of throwing:

```kotlin
synchronized(gate) {
    if (!ready.get()) {
        try {
            gate.wait(readinessTimeoutMs)
        } catch (ie: InterruptedException) {
            Thread.currentThread().interrupt()   // preserve interrupt status
        }
    }
}
if (!ready.get()) {
    stop()            // destroy the child we forked; never leak it
    return false
}
return true
```

With this, an interrupted boot returns `false` cleanly and `bootGoChild`'s
existing failure path runs with no live Go process left behind, regardless of
whether the field was ever assigned. Regression-guarded by a `GoLauncherTest`
case that interrupts a parked boot and asserts `false` + destroyed process.

## Correction 2 -- supervisor cannot survive teardown (fixes start-after-stop)

The defect: `Supervisor.start()` sets `stopFlag = false` and spawns three
daemon watchers; `stop()` sets `stopFlag = true` and interrupts them
(`Supervisor.kt:23-99`). The boot worker checks `stopping` and then calls
`supervisor.start(...)`. If `onDestroy` runs `supervisor.stop()` in the gap
between that check and the call, the worker re-arms the supervisor with fresh
watcher threads nothing ever stops.

Fix: keep an early `supervisor.stop()` (so the supervisor can't trigger a
restart that fights teardown while we wait on the join), then call
`supervisor.stop()` again after joining the boot thread. `stop()` is
idempotent, so the second call is a no-op in the normal case and tears down a
worker-armed supervisor in the race case. `Supervisor` itself is unchanged.

## Code shape

```kotlin
@Volatile private var bootThread: Thread? = null
@Volatile private var stopping = false
// goLauncher marked @Volatile so a timed-out join still sees the worker's write.

override fun onCreate() {
    super.onCreate()
    // ... channel, receivers, startForeground, modem callbacks, UsbPttAdapter.init,
    //     graywolf-io-init thread, PlatformServer.start, GpsAdapter.start,
    //     adapter wiring ... (all unchanged, on the main thread)

    bootThread = thread(start = true, isDaemon = true, name = "graywolf-boot") {
        if (stopping) return@thread
        if (!bootModem()) { stopSelf(); return@thread }
        if (stopping) { ModemBridge.modemStop(); return@thread }
        audioPump.start()
        if (!bootGoChild()) {                       // now never orphans the child
            audioPump.stop(); ModemBridge.modemStop(); stopSelf(); return@thread
        }
        if (stopping) return@thread
        supervisor.start { goLauncher?.process }
    }
}

override fun onDestroy() {
    stopping = true
    goListenerReady = false
    supervisor.stop()                               // (a) prevent restart-fighting during the join
    bootThread?.let { it.interrupt(); it.join(BOOT_JOIN_MS) }   // BOOT_JOIN_MS = 2_500
    bootThread = null
    supervisor.stop()                               // (b) idempotent: tear down a worker-armed supervisor
    startupThread?.let { it.interrupt(); it.join(2_000) }
    startupThread = null
    // ... existing teardown: gpsAdapter.stop, goLauncher.stop, audioPump.stop,
    //     audioTxPump.stop, UsbPttAdapter.closeAll, adapters.shutdown,
    //     platformServer.stop, ModemBridge.modemStop, unregister receivers ...
}
```

Why this is correct:
- Go child can't leak -- `GoLauncher` destroys its own process on
  interrupt/timeout (Correction 1), so teardown no longer depends on the field
  having been assigned.
- Supervisor can't survive -- the post-join `supervisor.stop()` (Correction 2)
  catches any re-arm that slipped through the TOCTOU window; the pre-join stop
  keeps it from triggering a restart while we tear down.
- Bounded join -- `gate.wait` honors interrupt; native `modemAwaitReady` may
  not, so the join is bounded (`BOOT_JOIN_MS`). On timeout, teardown proceeds
  -- same philosophy as the existing `startupThread` join.
- Worker self-cleans, does not rely on process death. The daemon worker is NOT
  guaranteed to die promptly when `onDestroy` returns (under `START_STICKY`
  Android keeps the process until the low-memory killer reclaims it), so the
  worker must not leak resources it built after a timed-out join. Every
  `stopping` checkpoint therefore tears down what the thread has built so far:
  the check after `bootModem` calls `modemStop()`; the check after `bootGoChild`
  succeeds (but before `supervisor.start`) calls `goLauncher?.stop()` +
  `audioPump.stop()` + `modemStop()`. This closes the window where a join that
  timed out earlier (e.g. on a non-interruptible native call) lets the worker
  fork the Go child / start audio after `onDestroy`'s own teardown already ran
  against a still-null `goLauncher`. All of these are idempotent with
  `onDestroy`'s teardown, so the happy path double-stop is harmless.
- No self-join -- `onDestroy` always runs on the main thread; the worker only
  schedules the stop via `stopSelf()` and returns.

## Explicitly NOT doing

- Not moving `GpsAdapter.start` / `PlatformServer.start` / adapter wiring off
  main (constraint 4 + ordering; not the blocking calls).
- Not switching to coroutines / `HandlerThread` -- matches the raw-thread
  idiom (`startupThread`).
- Not changing the 10s readiness timeouts -- they're a backstop, not the bug.

## Verification

- ANR fix: launch on device/emulator; confirm via `poc-b:` markers that
  `onCreate` returns immediately and `go_child_up` / `webview_loaded` still
  fire. Stub `modemAwaitReady` / Go readiness to sleep ~8s and confirm the UI
  stays responsive with no ANR dialog (the pre-fix build ANRs here).
- Teardown race (covers both corrections): swipe-to-stop while readiness is
  artificially delayed, then confirm a clean shutdown with no orphaned
  `libgraywolf.so` process and no lingering `go-watcher` / `modem-watcher` /
  `supervisor-restart` threads.
- Unit: `GoLauncherTest` gains the interrupt regression for Correction 1;
  `SupervisorTest` / `PlatformServerTest` are unaffected (the change moves
  *where* the boot runs, not its logic).

## Risk / blast radius

Low and localized: `GraywolfService.onCreate`/`onDestroy` (~30 lines net) +
`@Volatile` on a couple of fields + a ~6-line change in
`GoLauncher.startAndAwaitReady`. No protocol, native, or web changes.
`Supervisor` is relied on (already idempotent) but not modified. The subtlety
is the teardown path, which the two corrections close.
