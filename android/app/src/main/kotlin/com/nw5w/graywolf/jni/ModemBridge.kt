package com.nw5w.graywolf.jni

object ModemBridge {
    init {
        // System.loadLibrary("graywolfmodem") matches libgraywolfmodem.so.
        // The Rust [lib] name override (Task 1) ensures cargo-ndk produces
        // that exact filename.
        System.loadLibrary("graywolfmodem")
    }

    external fun modemVersion(): String
    external fun modemStart(socketPath: String, gainDb: Float): Int
    external fun modemAwaitReady(timeoutMs: Long): Boolean
    external fun modemPushSamples(buf: ShortArray, len: Int)
    external fun modemSetGainDb(db: Float)
    external fun modemStop()
}
