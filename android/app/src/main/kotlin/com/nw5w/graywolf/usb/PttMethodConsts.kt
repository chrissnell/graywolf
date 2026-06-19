package com.nw5w.graywolf.usb

/**
 * PTT method integer constants for the Android actuator path.
 *
 * **Single canonical mapping in Kotlin** — these values mirror the
 * `PttMethod` proto enum (proto/platform.proto) and the Rust
 * `ptt_android_consts` module. Spec Appendix B is the authoritative source;
 * T13 will add a cross-language sync integration test.
 */
object PttMethodConsts {
    /** proto `PTT_METHOD_UNKNOWN = 0` — error / unset */
    const val PTT_METHOD_UNKNOWN = 0
    /** proto `PTT_METHOD_CP2102N_RTS = 1` — Digirig (CP2102N USB serial RTS) */
    const val PTT_METHOD_CP2102N_RTS = 1
    /** proto `PTT_METHOD_CM108_HID = 2` — wired-GPIO CM108 dongles */
    const val PTT_METHOD_CM108_HID = 2
    /** proto `PTT_METHOD_AIOC_CDC_DTR = 3` — AIOC firmware ≥1.2.0 CDC-ACM DTR */
    const val PTT_METHOD_AIOC_CDC_DTR = 3
    /** proto `PTT_METHOD_VOX = 4` — no PTT wire; audio drives VOX */
    const val PTT_METHOD_VOX = 4
    /** proto `PTT_METHOD_DIGIRIG_TONE = 5` — no PTT wire; right-channel tone keys the Digirig Lite */
    const val PTT_METHOD_DIGIRIG_TONE = 5
}
