package com.nw5w.graywolf.binaries

import org.junit.Assert.assertEquals
import org.junit.Test

class RestartPolicyTest {
    @Test
    fun normalFailuresUseBackoffCurveThenCap() {
        val p = RestartPolicy(maxFailuresIn60s = 3, backoffsMs = longArrayOf(1_000, 2_000, 5_000))
        // First 3 failures within the window: escalating backoff, still "restart".
        assertEquals(RestartPolicy.Decision(restart = true, delayMs = 1_000, degraded = false), p.onFailure(0L))
        assertEquals(RestartPolicy.Decision(restart = true, delayMs = 2_000, degraded = false), p.onFailure(1_000L))
        assertEquals(RestartPolicy.Decision(restart = true, delayMs = 5_000, degraded = false), p.onFailure(2_000L))
    }

    @Test
    fun exceedingWindowEntersDegradedButKeepsRetrying() {
        val p = RestartPolicy(maxFailuresIn60s = 3, backoffsMs = longArrayOf(1_000), degradedDelayMs = 30_000)
        p.onFailure(0L); p.onFailure(10L); p.onFailure(20L)
        // 4th failure inside 60s: degraded — but STILL restarts (never halts), at the long delay.
        val d = p.onFailure(30L)
        assertEquals(true, d.restart)
        assertEquals(true, d.degraded)
        assertEquals(30_000L, d.delayMs)
    }

    @Test
    fun failuresOlderThan60sDoNotCountAndClearDegraded() {
        val p = RestartPolicy(maxFailuresIn60s = 3, backoffsMs = longArrayOf(1_000), degradedDelayMs = 30_000)
        p.onFailure(0L); p.onFailure(10L); p.onFailure(20L); p.onFailure(30L) // degraded
        // A failure 61s after the oldest: window has pruned the early ones → healthy backoff again.
        val d = p.onFailure(61_001L)
        assertEquals(false, d.degraded)
        assertEquals(true, d.restart)
    }
}
