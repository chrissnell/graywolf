package com.nw5w.graywolf.binaries

import org.junit.Assert.assertFalse
import org.junit.Assert.assertNull
import org.junit.Test
import java.util.concurrent.atomic.AtomicReference

/**
 * GoLauncher's positive-path readiness is exercised end-to-end on
 * hardware (Task 19's adb logcat verification). The unit-test surface
 * is necessarily limited because GoLauncher constructs ProcessBuilder
 * directly with no DI seam -- a refactor that injects a process
 * factory would be a phase 5+ cleanup.
 */
class GoLauncherTest {
    @Test
    fun startAndAwaitReady_returnsFalseOnTimeoutWhenChildEmitsNothing() {
        // /bin/cat with no args reads from stdin and prints nothing on
        // stdout, so the readiness gate must time out and return false.
        // Confirms the negative path: launcher does not spuriously
        // signal ready when stdout is silent.
        val launcher = GoLauncher(
            executablePath = "/bin/cat",
            env = mapOf("LC_ALL" to "C"),
        )
        val ok = launcher.startAndAwaitReady(500)
        assertFalse("cat with no stdin produces no readiness byte", ok)
        launcher.stop()
    }

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

    @Test
    fun stop_isIdempotent() {
        val launcher = GoLauncher(executablePath = "/bin/cat", env = emptyMap())
        launcher.stop()
        launcher.stop() // second call must not throw
    }
}
