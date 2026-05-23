package com.nw5w.graywolf.binaries

/**
 * Pure restart-decision logic, decoupled from threads/JNI/Process so it is
 * host-JVM testable. Counts failures in a sliding 60s window. Inside the
 * window it escalates through [backoffsMs]; once the window limit is
 * exceeded it enters DEGRADED mode and keeps retrying at [degradedDelayMs]
 * instead of halting forever (the old behavior that left the modem deaf
 * with no recovery). Time is injected so tests are deterministic.
 */
class RestartPolicy(
    private val maxFailuresIn60s: Int = 3,
    private val backoffsMs: LongArray = longArrayOf(1_000, 2_000, 5_000, 10_000),
    private val degradedDelayMs: Long = 30_000,
) {
    data class Decision(val restart: Boolean, val delayMs: Long, val degraded: Boolean)

    private val recent = ArrayDeque<Long>()
    private var idx = 0

    @Synchronized
    fun onFailure(nowMs: Long): Decision {
        val cutoff = nowMs - 60_000
        while (recent.isNotEmpty() && recent.first() < cutoff) recent.removeFirst()
        recent.addLast(nowMs)
        return if (recent.size > maxFailuresIn60s) {
            Decision(restart = true, delayMs = degradedDelayMs, degraded = true)
        } else {
            val delay = backoffsMs[minOf(idx, backoffsMs.lastIndex)]
            idx = minOf(idx + 1, backoffsMs.lastIndex)
            Decision(restart = true, delayMs = delay, degraded = false)
        }
    }
}
