package com.nw5w.graywolf.binaries

import org.junit.Assert.assertFalse
import org.junit.Test

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
    fun stop_isIdempotent() {
        val launcher = GoLauncher(executablePath = "/bin/cat", env = emptyMap())
        launcher.stop()
        launcher.stop() // second call must not throw
    }
}
