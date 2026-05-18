package com.nw5w.graywolf.jni

/**
 * Called by the Rust modem TX governor to actuate the radio's PTT line via
 * the operator-configured USB transport. Implementation lives in
 * UsbPttAdapter; installed once at GraywolfService.onCreate.
 *
 * @param method one of PttMethodConsts.PTT_METHOD_* (per spec Appendix B)
 * @param keyed  true to key the radio, false to unkey
 * @return true on success, false to propagate as Err back into Rust
 */
interface UsbPttCallback {
    fun pttSet(method: Int, keyed: Boolean): Boolean
}

/**
 * Called by the Rust modem TX governor on every PCM frame. Implementation
 * lives in AudioTxPump; installed once at GraywolfService.onCreate.
 *
 * Blocking call — the Rust TX thread blocks on AudioTrack.write so the
 * audio buffer can drain naturally.
 *
 * @param samples PCM16 mono samples at modem sample rate (22050 Hz)
 * @param count   number of samples to consume from the start of `samples`
 * @return AudioTrack.write convention: bytes written or a negative error
 */
interface AudioTxCallback {
    fun pushSamples(samples: ShortArray, count: Int): Int
}

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
    external fun modemBuildTestFrame(): ShortArray
    external fun installPttCallback(cb: UsbPttCallback)
    external fun installAudioTxCallback(cb: AudioTxCallback)
}
