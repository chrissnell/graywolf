package com.nw5w.graywolf.binaries

import android.util.Log
import com.nw5w.graywolf.jni.ModemBridge
import kotlin.concurrent.thread

class Supervisor(
    private val maxFailuresIn60s: Int = 3,
    private val onRestart: () -> Boolean,
) {
    @Volatile private var stopFlag = false
    private val backoffsMs = longArrayOf(1_000, 2_000, 5_000, 10_000)
    private var goWatcher: Thread? = null
    private var modemWatcher: Thread? = null
    private var restartLoop: Thread? = null
    private val recentFailures = ArrayDeque<Long>()
    private val restartLock = Object()
    @Volatile private var failureSignalled = false

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
            var idx = 0
            while (!stopFlag) {
                synchronized(restartLock) {
                    while (!failureSignalled && !stopFlag) restartLock.wait()
                    failureSignalled = false
                }
                if (stopFlag) return@thread
                pruneFailures()
                recentFailures.addLast(System.currentTimeMillis())
                if (recentFailures.size > maxFailuresIn60s) {
                    Log.e(TAG, "$maxFailuresIn60s failures in 60s; halting restart")
                    return@thread
                }
                try { Thread.sleep(backoffsMs[idx]) } catch (_: InterruptedException) { return@thread }
                idx = minOf(idx + 1, backoffsMs.lastIndex)
                if (!onRestart()) {
                    Log.e(TAG, "restart hook returned false; halting")
                    return@thread
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

    private fun pruneFailures() {
        val cutoff = System.currentTimeMillis() - 60_000
        while (recentFailures.isNotEmpty() && recentFailures.first() < cutoff) {
            recentFailures.removeFirst()
        }
    }

    companion object { private const val TAG = "Supervisor" }
}
