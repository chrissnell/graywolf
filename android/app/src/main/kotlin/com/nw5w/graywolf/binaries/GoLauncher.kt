package com.nw5w.graywolf.binaries

import android.util.Log
import java.io.BufferedReader
import java.io.InputStreamReader

class GoLauncher(
    private val executablePath: String,
    private val env: Map<String, String>,
) {
    @Volatile var process: Process? = null
        private set

    /**
     * Exec the binary. Blocks up to [readinessTimeoutMs] for the child to
     * write a single `\n` to stdout. Returns true if the readiness byte
     * arrived, false on timeout.
     */
    fun startAndAwaitReady(readinessTimeoutMs: Long): Boolean {
        val pb = ProcessBuilder(executablePath)
        pb.environment().putAll(env)
        pb.redirectErrorStream(false)
        val p = pb.start()
        process = p

        Thread({
            try {
                BufferedReader(InputStreamReader(p.errorStream)).useLines { lines ->
                    lines.forEach { Log.w(TAG_STDERR, it) }
                }
            } catch (t: Throwable) {
                Log.w(TAG_STDERR, "stderr drain ended: $t")
            }
        }, "go-stderr").apply { isDaemon = true; start() }

        val ready = java.util.concurrent.atomic.AtomicBoolean(false)
        val gate = Object()
        Thread({
            try {
                val r = p.inputStream
                val first = try { r.read() } catch (_: Throwable) { -1 }
                if (first == '\n'.code) {
                    synchronized(gate) {
                        ready.set(true)
                        gate.notifyAll()
                    }
                }
                BufferedReader(InputStreamReader(r)).useLines { lines ->
                    lines.forEach { Log.i(TAG_STDOUT, it) }
                }
            } catch (t: Throwable) {
                Log.i(TAG_STDOUT, "stdout drain ended: $t")
            }
        }, "go-stdout").apply { isDaemon = true; start() }

        synchronized(gate) {
            if (!ready.get()) gate.wait(readinessTimeoutMs)
        }
        return ready.get()
    }

    fun stop() {
        process?.destroy()
        process = null
    }

    companion object {
        private const val TAG_STDOUT = "graywolf-pocb"
        private const val TAG_STDERR = "graywolf-pocb-err"
    }
}
