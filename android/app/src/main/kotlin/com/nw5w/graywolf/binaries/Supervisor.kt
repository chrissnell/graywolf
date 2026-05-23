package com.nw5w.graywolf.binaries

import android.util.Log
import com.nw5w.graywolf.jni.ModemBridge
import kotlin.concurrent.thread

class Supervisor(
    private val maxFailuresIn60s: Int = 3,
    private val onRestart: () -> Boolean,
    private val onDegraded: () -> Unit = {},
    private val onHealthy: () -> Unit = {},
    private val now: () -> Long = { System.currentTimeMillis() },
) {
    @Volatile private var stopFlag = false
    private val policy = RestartPolicy(maxFailuresIn60s)
    private var goWatcher: Thread? = null
    private var modemWatcher: Thread? = null
    private var restartLoop: Thread? = null
    private val restartLock = Object()
    @Volatile private var failureSignalled = false
    @Volatile private var degraded = false

    fun start(processSupplier: () -> Process?) {
        stopFlag = false
        failureSignalled = false

        goWatcher = thread(name = "go-watcher", isDaemon = true) {
            var prev: Process? = null
            while (!stopFlag) {
                val p = processSupplier()
                if (p == null || p === prev) {
                    try { Thread.sleep(500) } catch (_: InterruptedException) { return@thread }
                    continue
                }
                prev = p
                val rc = try { p.waitFor() } catch (_: InterruptedException) { -1 }
                if (stopFlag) return@thread
                Log.w(TAG, "poc-b: go_child_died rc=$rc")
                signalFailure()
            }
        }

        modemWatcher = thread(name = "modem-watcher", isDaemon = true) {
            while (!stopFlag) {
                try { Thread.sleep(2_000) } catch (_: InterruptedException) { return@thread }
                if (stopFlag) return@thread
                if (!ModemBridge.modemAwaitReady(0)) {
                    Log.w(TAG, "poc-b: modem_died (ready=false)")
                    signalFailure()
                }
            }
        }

        restartLoop = thread(name = "supervisor-restart", isDaemon = true) {
            while (!stopFlag) {
                synchronized(restartLock) {
                    while (!failureSignalled && !stopFlag) restartLock.wait()
                    failureSignalled = false
                }
                if (stopFlag) return@thread
                val d = policy.onFailure(now())
                if (d.degraded && !degraded) {
                    degraded = true
                    Log.e(TAG, "modem restart degraded: retrying every ${d.delayMs}ms")
                    try { onDegraded() } catch (t: Throwable) { Log.e(TAG, "onDegraded threw", t) }
                }
                try { Thread.sleep(d.delayMs) } catch (_: InterruptedException) { return@thread }
                if (!onRestart()) {
                    // Restart hook failed (e.g. modemAwaitReady timed out). Do
                    // NOT halt — re-signal so we try again after another delay.
                    Log.w(TAG, "restart hook returned false; will retry")
                    signalFailure()
                    continue
                }
                if (degraded) {
                    degraded = false
                    Log.i(TAG, "modem recovered from degraded state")
                    try { onHealthy() } catch (t: Throwable) { Log.e(TAG, "onHealthy threw", t) }
                }
                Log.i(TAG, "poc-b: supervisor_restart_succeeded")
            }
        }
    }

    private fun signalFailure() {
        synchronized(restartLock) {
            failureSignalled = true
            restartLock.notifyAll()
        }
    }

    fun stop() {
        stopFlag = true
        signalFailure()
        goWatcher?.interrupt()
        modemWatcher?.interrupt()
        restartLoop?.interrupt()
        goWatcher = null; modemWatcher = null; restartLoop = null
    }

    companion object { private const val TAG = "Supervisor" }
}
