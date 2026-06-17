# Move the Blocking Backend Boot Off the Launch Path — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Stop `GraywolfService.onCreate` from ANR-ing on launch (graywolf#315) by moving the two blocking backend boot waits (`bootModem`, `bootGoChild`) off the main thread onto a dedicated `graywolf-boot` daemon worker, while keeping teardown leak-free.

**Architecture:** Surgical split at the `bootModem` boundary. Everything fast or Looper-dependent (`startForeground`, modem callbacks, `UsbPttAdapter.init`, `PlatformServer.start`, `GpsAdapter.start`, adapter wiring) stays on the main thread in `onCreate`, in its current order. Only the blocking tail — `bootModem()` → `audioPump.start()` → `bootGoChild()` → `supervisor.start(...)` — moves to a daemon worker. `onDestroy` coordinates with that worker via an interrupt + bounded join, and two corrections close stop-during-boot leaks: `GoLauncher` destroys its own forked process on interrupt/timeout (no orphaned `libgraywolf.so`), and `supervisor.stop()` runs on both sides of the join (no re-armed supervisor).

**Tech Stack:** Kotlin, Android foreground `Service`, `kotlin.concurrent.thread` (matching the existing `graywolf-io-init` worker idiom), JUnit 4 unit tests run via the Gradle wrapper.

**Source of truth:** This plan implements **Design v2** from GRA-122 (the revision that folded in the two corrections from the GRA-123 review). The split, constraints, and problem statement are unchanged from the original design; Corrections 1 and 2 are the v2 additions.

---

## Background — why each piece is where it is

Read this before starting; it is the "questionable taste" insurance.

The bug: `GraywolfService` shares the app's single main/UI thread with `MainActivity` (no `android:process` on the `<service>`). `Service.onCreate` runs on that main thread and, after the fast `startForeground`, performs two synchronous blocking waits — `bootModem()` → `ModemBridge.modemAwaitReady(10_000)` (`GraywolfService.kt:151`) and `bootGoChild()` → `GoLauncher.startAndAwaitReady(10_000)` = `gate.wait(10_000)` (`GoLauncher.kt:57`). If both don't reach readiness inside Android's ANR window (~5s), the main thread is pinned, the WebView can't paint (the white screen), and the OS kills the app, which drops the foreground notification — exactly the graywolf#315 report.

**Constraints that shape the split (do not violate):**

1. `startForeground` must run synchronously on the main thread within 5s of `startForegroundService`. It already does, before any blocking work — leave it on main.
2. Modem JNI callbacks must be installed before `bootModem`: `ModemBridge.installPttCallback` / `installAudioTxCallback` (and `modemVersion()`, which triggers `loadLibrary`) must complete first, because modem boot can fire TX/PTT callbacks (`GraywolfService.kt:327-333`). These stay on main, ahead of the worker.
3. `PlatformServer.start()` must precede `bootGoChild` — the Go child dials the platform socket on launch (`GraywolfService.kt:359-364`). `PlatformServer.start` stays on main (fast in the happy path); only `bootGoChild` moves.
4. `GpsAdapter.start()` must run on a thread with a Looper — `requestLocationUpdates` / `registerGnssStatusCallback` bind to the calling thread's Looper (`GpsAdapter.kt:76,80`). It stays on the main thread; do **not** move it to the worker.
5. `UsbPttAdapter.init` must precede `onResume`'s `enumerate()` (`GraywolfService.kt:336-338`). Stays on main.

**Idiom:** the codebase already runs blocking HAL init off-main via a raw `kotlin.concurrent.thread` named `graywolf-io-init`, joined (bounded) in `onDestroy` (`GraywolfService.kt:351-354`, `446-458`). The new `graywolf-boot` worker mirrors that exact pattern. Do **not** introduce coroutines or a `HandlerThread` — match the existing idiom for a small, reviewable diff.

**The two corrections (the part most likely to bite):**

- **Correction 1 (Task 1):** In `bootGoChild`, `GoLauncher.startAndAwaitReady` calls `pb.start()` (forking `libgraywolf.so`) *before* it blocks on `gate.wait`, but the `goLauncher` field is only assigned *after* readiness succeeds (`GraywolfService.kt:186-191`). If `onDestroy` interrupts the boot worker mid-`gate.wait`, the uncaught `InterruptedException` unwinds the worker, the field stays `null`, and `onDestroy`'s `goLauncher?.stop()` no-ops — the forked process is orphaned. Fix lives in `GoLauncher` so "if I forked it, I own its teardown" is in one place: `startAndAwaitReady` swallows the interrupt, destroys its own process on any non-ready exit, and returns `false`.
- **Correction 2 (Task 3):** `Supervisor.start()` sets `stopFlag = false` and spawns three daemon watchers; `stop()` sets `stopFlag = true` and interrupts them (`Supervisor.kt:23-99`). The boot worker checks `stopping` and then calls `supervisor.start(...)`. If `onDestroy` runs `supervisor.stop()` in the gap, the worker re-arms the supervisor with watcher threads nothing will ever stop. Fix: call `supervisor.stop()` both before the boot-thread join (so it can't trigger a restart that fights teardown) and again after (idempotent; tears down a worker-armed supervisor). `stop()` is already idempotent (`Supervisor.kt:92-99`).

**Verified against the checked-out tree** (branch reflects current `main`): every line reference above matches the code as written. `GoLauncher.startAndAwaitReady` has no try/catch around `gate.wait` today (`GoLauncher.kt:56-59`), so the interrupt does propagate — Correction 1 is real, not theoretical.

---

## File Structure

- **`android/app/src/main/kotlin/com/nw5w/graywolf/binaries/GoLauncher.kt`** — Correction 1. `startAndAwaitReady` becomes interrupt-safe and self-cleaning. ~6-line change to the final wait block (lines 56-59).
- **`android/app/src/test/kotlin/com/nw5w/graywolf/binaries/GoLauncherTest.kt`** — add one regression test for Correction 1 (interrupt → returns `false`, process destroyed). Existing two tests unchanged.
- **`android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt`** — the split. New `bootThread`/`stopping` fields + `@Volatile` on `goLauncher`; `onCreate` blocking tail moves to the `graywolf-boot` worker; `onDestroy` gains the bounded join + the two-sided `supervisor.stop()`. ~30 lines net.
- **`android/app/src/main/kotlin/com/nw5w/graywolf/binaries/Supervisor.kt`** — **no change** (already idempotent; relied on, not modified).
- **`.context/2026-06-17-android-move-blocking-boot-off-launch.md`** — the design doc (v2), so the corrected invariant is what future readers see. Repo convention is to keep subsystem design intent in `.context/`.

**Test command (run from `android/`):**
```bash
./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.binaries.GoLauncherTest"
```
The `GraywolfService` change is **not** unit-testable in this module — `testOptions` deliberately keeps Robolectric out (`app/build.gradle.kts:116-120`) and `onCreate`/`onDestroy` need a real Android runtime. Its verification is the device/emulator pass in Task 5. This matches the existing test posture (see the class doc in `GoLauncherTest.kt`: positive readiness path is hardware-verified, not unit-tested).

---

## Task 1: Correction 1 — `GoLauncher` owns its forked process

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/binaries/GoLauncher.kt:56-59`
- Test: `android/app/src/test/kotlin/com/nw5w/graywolf/binaries/GoLauncherTest.kt`

- [ ] **Step 1: Write the failing test**

Add this test to `GoLauncherTest.kt` (keep the two existing tests). Add the imports `import org.junit.Assert.assertNull` and `import java.util.concurrent.atomic.AtomicReference` at the top of the file.

```kotlin
    @Test
    fun startAndAwaitReady_destroysProcessAndReturnsFalseWhenInterrupted() {
        // /bin/cat never emits a readiness byte, so the worker parks in
        // gate.wait. Interrupting it must (a) return false, not throw, and
        // (b) destroy the forked child so onDestroy can't orphan it even
        // though the service never assigned its goLauncher field.
        val launcher = GoLauncher(executablePath = "/bin/cat", env = emptyMap())
        val result = AtomicReference<Boolean>()
        val worker = Thread { result.set(launcher.startAndAwaitReady(10_000)) }
        worker.start()
        Thread.sleep(300) // let the child fork and the worker reach gate.wait
        worker.interrupt()
        worker.join(2_000)
        assertFalse("interrupted boot must return false, not throw", result.get())
        assertNull("interrupted boot must destroy its forked process", launcher.process)
    }
```

- [ ] **Step 2: Run the test to verify it fails**

Run (from `android/`):
```bash
./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.binaries.GoLauncherTest"
```
Expected: FAIL. The current code lets `InterruptedException` propagate out of `startAndAwaitReady` (`GoLauncher.kt:56-59` has no catch), so the worker dies before setting `result`, leaving `result.get()` null → `assertFalse(null)` throws (NPE/assertion failure). The process is never destroyed, so `launcher.process` is non-null.

- [ ] **Step 3: Make `startAndAwaitReady` interrupt-safe and self-cleaning**

In `GoLauncher.kt`, replace the final wait block (currently lines 56-59):

```kotlin
        synchronized(gate) {
            if (!ready.get()) gate.wait(readinessTimeoutMs)
        }
        return ready.get()
```

with:

```kotlin
        synchronized(gate) {
            if (!ready.get()) {
                try {
                    gate.wait(readinessTimeoutMs)
                } catch (ie: InterruptedException) {
                    Thread.currentThread().interrupt() // preserve interrupt status for the caller
                }
            }
        }
        if (!ready.get()) {
            stop()            // destroy the child we forked; never leak it on timeout/interrupt
            return false
        }
        return true
```

Note: `stop()` already exists (`GoLauncher.kt:62-65`), is idempotent, and nulls `process` after `destroy()`.

- [ ] **Step 4: Run the test to verify it passes**

Run (from `android/`):
```bash
./gradlew :app:testDebugUnitTest --tests "com.nw5w.graywolf.binaries.GoLauncherTest"
```
Expected: PASS — all three tests green (`startAndAwaitReady_returnsFalseOnTimeoutWhenChildEmitsNothing`, `stop_isIdempotent`, `startAndAwaitReady_destroysProcessAndReturnsFalseWhenInterrupted`).

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/binaries/GoLauncher.kt \
        android/app/src/test/kotlin/com/nw5w/graywolf/binaries/GoLauncherTest.kt
git commit -m "android: make GoLauncher destroy its forked child on interrupt/timeout"
```

---

## Task 2: `GraywolfService` scaffolding fields

This task adds the fields the worker and teardown need, with **no behavior change** — the blocking boot still runs inline on the main thread after this task. It exists so Task 3 compiles cleanly and stays reviewable.

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt`

- [ ] **Step 1: Add the boot-worker fields and make `goLauncher` visible across threads**

In `GraywolfService.kt`, change the `goLauncher` field declaration (currently `GraywolfService.kt:46`):

```kotlin
    private var goLauncher: GoLauncher? = null
```

to:

```kotlin
    // Written by the graywolf-boot worker (onCreate), read by onDestroy /
    // supervisor on other threads -- @Volatile so a timed-out join still
    // sees the worker's write.
    @Volatile private var goLauncher: GoLauncher? = null
```

Then, immediately after the `startupThread` field (currently `GraywolfService.kt:51`), add:

```kotlin
    // Worker that runs the blocking backend boot (bootModem/bootGoChild) off
    // the main thread; mirrors the graywolf-io-init pattern. onDestroy joins
    // it (bounded) before tearing those resources down.
    @Volatile private var bootThread: Thread? = null
    // Set true in onDestroy so the boot worker can bail between steps and undo
    // partial state instead of finishing a boot that's about to be torn down.
    @Volatile private var stopping = false
```

- [ ] **Step 2: Add the bounded-join timeout constant**

In the `companion object` (currently `GraywolfService.kt:486-501`), add alongside the other constants (e.g. right after `private const val NOTIF_ID = 0x6757`):

```kotlin
        // Bounded join for the graywolf-boot worker in onDestroy. gate.wait
        // honors interrupt promptly; the native modemAwaitReady may not, so the
        // join is bounded and teardown proceeds on timeout (the daemon worker
        // then dies with the process) -- same philosophy as the startupThread join.
        private const val BOOT_JOIN_MS = 2_500L
```

- [ ] **Step 3: Verify it still compiles**

Run (from `android/`):
```bash
./gradlew :app:compileDebugKotlin
```
Expected: BUILD SUCCESSFUL. No behavior change yet — the new fields are unused, the inline boot at `onCreate` lines 409-421 is untouched.

- [ ] **Step 4: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt
git commit -m "android: add graywolf-boot worker scaffolding to GraywolfService"
```

---

## Task 3: Move the blocking boot to the worker and coordinate teardown

`onCreate` and `onDestroy` change together so the worker is always joined by teardown — landing one without the other would leave a correctness gap. Both halves are in this single task / commit.

**Files:**
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt:409-421` (onCreate tail)
- Modify: `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt:446-484` (onDestroy)

- [ ] **Step 1: Move the blocking tail of `onCreate` onto the `graywolf-boot` worker**

In `GraywolfService.kt`, replace the inline blocking boot at the end of `onCreate` (currently lines 409-421):

```kotlin
        if (!bootModem()) {
            stopSelf()
            return
        }
        audioPump.start()
        if (!bootGoChild()) {
            audioPump.stop()
            ModemBridge.modemStop()
            stopSelf()
            return
        }

        supervisor.start { goLauncher?.process }
    }
```

with the worker spawn (this is the last statement in `onCreate`):

```kotlin
        // Backend boot is the only blocking work left in onCreate (bootModem +
        // bootGoChild can each wait up to 10s). Run it off the main thread so
        // startup duration can never ANR the UI. MainActivity polls the @Volatile
        // goListenerReady flag asynchronously, so the WebView paints the moment
        // the worker flips it -- with the UI thread never blocked. onDestroy
        // interrupts + joins this worker (bounded) before tearing its resources.
        bootThread = thread(start = true, isDaemon = true, name = "graywolf-boot") {
            if (stopping) return@thread
            if (!bootModem()) {
                stopSelf()
                return@thread
            }
            if (stopping) {
                ModemBridge.modemStop()
                return@thread
            }
            audioPump.start()
            if (!bootGoChild()) {              // Correction 1: never orphans the Go child now
                audioPump.stop()
                ModemBridge.modemStop()
                stopSelf()
                return@thread
            }
            if (stopping) return@thread
            supervisor.start { goLauncher?.process }
        }
    }
```

Notes: `thread(...)` is `kotlin.concurrent.thread`, already imported (`GraywolfService.kt:41`). The `BindContendedException` early-`return` path (`GraywolfService.kt:365-373`) is *above* this block and unchanged — that bind still happens on the main thread per constraint 3. `onStartCommand` still returns `START_STICKY` (`GraywolfService.kt:424`); nothing on the main thread depends on the backend being up synchronously.

- [ ] **Step 2: Rework `onDestroy` for teardown coordination (Correction 2 + the bounded join)**

In `GraywolfService.kt`, replace the head of `onDestroy` (currently lines 446-458):

```kotlin
    override fun onDestroy() {
        supervisor.stop()
        goListenerReady = false
        // The off-main-thread audio/USB init (onCreate) may still be in flight --
        // wait for it (bounded) before stopping txPump / closing USB handles, so we
        // don't tear down a half-built AudioTrack or open USB handle. If it's wedged
        // on the HAL the join times out and teardown proceeds; the daemon thread dies
        // with the process. The wait runs here, not on input dispatch, so it can't ANR.
        startupThread?.let {
            it.interrupt()
            it.join(2_000)
        }
        startupThread = null
```

with:

```kotlin
    override fun onDestroy() {
        // Signal the boot worker first so any inter-step `stopping` check bails
        // before bringing more backend up.
        stopping = true
        goListenerReady = false
        // (a) Stop the supervisor BEFORE joining the boot worker so it can't fire
        // a restart that fights teardown while we wait.
        supervisor.stop()
        // Interrupt + bounded-join the boot worker before tearing its resources
        // down. gate.wait honors interrupt promptly; native modemAwaitReady may
        // not, so the join is bounded -- on timeout teardown proceeds and the
        // daemon worker dies with the process. GoLauncher destroys its own forked
        // child on interrupt (Correction 1), so an interrupted boot leaks nothing.
        bootThread?.let {
            it.interrupt()
            it.join(BOOT_JOIN_MS)
        }
        bootThread = null
        // (b) Stop the supervisor AGAIN, after the join: idempotent in the normal
        // case, but tears down a supervisor the worker may have re-armed in the
        // TOCTOU window between its `stopping` check and supervisor.start (Correction 2).
        supervisor.stop()
        // The off-main-thread audio/USB init (onCreate) may still be in flight --
        // wait for it (bounded) before stopping txPump / closing USB handles, so we
        // don't tear down a half-built AudioTrack or open USB handle. If it's wedged
        // on the HAL the join times out and teardown proceeds; the daemon thread dies
        // with the process. The wait runs here, not on input dispatch, so it can't ANR.
        startupThread?.let {
            it.interrupt()
            it.join(2_000)
        }
        startupThread = null
```

The rest of `onDestroy` from `gpsAdapter?.stop()` onward (currently lines 459-483) is **unchanged**. In particular `goLauncher?.stop()` (line 461) remains — after the bounded join it tears down a Go child that *did* reach readiness; an interrupted-mid-boot child was already destroyed by `GoLauncher` itself (Correction 1).

- [ ] **Step 3: Verify it compiles**

Run (from `android/`):
```bash
./gradlew :app:compileDebugKotlin
```
Expected: BUILD SUCCESSFUL.

- [ ] **Step 4: Run the full unit-test suite to confirm no regressions**

Run (from `android/`):
```bash
./gradlew :app:testDebugUnitTest
```
Expected: BUILD SUCCESSFUL. `SupervisorTest`, `GoLauncherTest`, `PlatformServerTest` all pass — the change moves *where* the boot runs, not its logic, so existing tests are unaffected (the GoLauncher interrupt test from Task 1 is the only new one).

- [ ] **Step 5: Commit**

```bash
git add android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt
git commit -m "android: run backend boot on graywolf-boot worker, off the launch path"
```

---

## Task 4: Land the design doc in `.context/`

Repo convention keeps subsystem design intent in `.context/`. Land the **v2** design (with the corrected invariants) so future readers see the right teardown contract, not the original.

**Files:**
- Create: `.context/2026-06-17-android-move-blocking-boot-off-launch.md`

- [ ] **Step 1: Write the design doc**

Create `.context/2026-06-17-android-move-blocking-boot-off-launch.md` with the Design v2 content. Use the v2 design as posted on GRA-122 (the revision that includes Corrections 1 and 2). The doc must cover, at minimum: the ANR problem statement, the five constraints, the split (what stays on main vs. moves to the `graywolf-boot` worker), Correction 1 (`GoLauncher` owns its forked process), Correction 2 (two-sided `supervisor.stop()`), the revised `onCreate`/`onDestroy` code shape, the verification plan, and the risk/blast-radius note. Cross-reference graywolf#315.

- [ ] **Step 2: Commit**

```bash
git add .context/2026-06-17-android-move-blocking-boot-off-launch.md
git commit -m "docs: design for moving the blocking backend boot off the launch path"
```

---

## Task 5: Device / emulator verification

This is the real proof — the service change cannot be unit-tested in this module. Run on an Android device or emulator. **Do not mark the work complete on unit tests alone.**

**No files changed.** This task is verification only.

- [ ] **Step 1: Build and install a debug build**

Run (from `android/`):
```bash
./gradlew :app:assembleDebug
```
Then install the resulting APK on the target (`adb install -r app/build/outputs/apk/debug/app-debug.apk`).

- [ ] **Step 2: Confirm the ANR fix on a clean launch**

Launch the app and watch logcat:
```bash
adb logcat -c && adb logcat | grep -iE "poc-b|GraywolfService|ANR"
```
Expected: `onCreate` returns immediately (no ANR), the WebView paints (no multi-second white screen), and the `poc-b: go_child_up` marker (`GraywolfService.kt:193`) plus the existing `webview_loaded` marker still fire once the worker finishes.

- [ ] **Step 3: Confirm the UI stays responsive under a slow backend**

Artificially delay readiness to simulate the slow Samsung/Android 11 device that hits the bug — e.g. temporarily make `modemAwaitReady` (or the Go child's readiness emit) sleep ~8s. Relaunch and interact with the UI during the boot window.
Expected: the UI stays responsive and **no "app isn't responding" dialog appears**. (Build the pre-fix commit the same way to confirm it ANRs here — that is the regression this whole change removes.) Revert the artificial delay before finishing.

- [ ] **Step 4: Confirm the teardown race is leak-free (covers both corrections)**

With readiness still artificially delayed (so the boot worker is mid-flight), swipe the app away from recents during the boot window to trigger `onTaskRemoved` → `stopSelf` → `onDestroy`. Then check for leaks:
```bash
adb shell ps -A | grep -i libgraywolf      # must show NO orphaned Go child
adb shell ps -AT | grep -iE "go-watcher|modem-watcher|supervisor-restart"  # must show NO lingering supervisor threads
```
Expected: clean shutdown — no crash, **no orphaned `libgraywolf.so` process** (Correction 1), and **no lingering `go-watcher` / `modem-watcher` / `supervisor-restart` threads** (Correction 2). Revert the artificial delay.

- [ ] **Step 5: Record results**

Note the logcat markers and the leak-check output in the issue comment / PR description as the verification evidence. Per superpowers:verification-before-completion, paste the actual output — do not assert "verified" without it.

---

## Self-Review

**Spec coverage (Design v2 → tasks):**
- ANR root cause / move blocking waits off main → Task 3 (`onCreate` worker).
- Constraints 1-5 (startForeground, callbacks-before-boot, PlatformServer-before-Go, GpsAdapter Looper, UsbPttAdapter ordering) → preserved by Task 3 leaving everything above the worker on the main thread, in order; documented in Background + Task 4 doc.
- Correction 1 (GoLauncher owns its forked process) → Task 1 (code + regression test).
- Correction 2 (two-sided `supervisor.stop()`) → Task 3 Step 2.
- Bounded boot-thread join + `@Volatile` visibility + `stopping` inter-step bail → Tasks 2 & 3.
- Verification plan incl. the swipe-to-stop-during-boot race → Task 5.
- Design doc in `.context/` → Task 4.
- "Explicitly NOT doing" (no GpsAdapter/PlatformServer move, no coroutines/HandlerThread, no timeout changes) → respected; the worker carries only `bootModem`/`audioPump.start`/`bootGoChild`/`supervisor.start`.

**Placeholder scan:** none — every code step shows the exact before/after; test code is concrete; commands have expected output.

**Type / name consistency:** `bootThread`, `stopping`, `BOOT_JOIN_MS`, `goLauncher` (now `@Volatile`), `thread(...)`, `stopSelf()`, `supervisor.start { goLauncher?.process }`, `supervisor.stop()`, `GoLauncher.stop()` — all match the names used in the checked-out source and are introduced before use (Task 2 before Task 3). `BOOT_JOIN_MS` is `2_500L` to match `Thread.join(Long)`.

**Known limitation surfaced (not hidden):** the `GraywolfService` change has no unit test (module excludes Robolectric by design); Task 5 device verification is its only proof, consistent with the existing `GoLauncherTest`/`SupervisorTest` posture that hardware-verifies the boot path. This is called out in the File Structure section and Task 5 rather than papered over.
