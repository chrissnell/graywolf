//! PTT method integer constants for the Android actuator path.
//!
//! **Single canonical mapping in Rust** — these values mirror the
//! `PttMethod` proto enum (proto/platform.proto) and the Kotlin
//! `PttMethodConsts` object. Spec Appendix B is the authoritative source;
//! T13 will add a cross-language sync integration test.

/// proto `PTT_METHOD_UNKNOWN = 0` — error / unset
pub const PTT_METHOD_UNKNOWN: i32 = 0;
/// proto `PTT_METHOD_CP2102N_RTS = 1` — Digirig (CP2102N USB serial RTS)
pub const PTT_METHOD_CP2102N_RTS: i32 = 1;
/// proto `PTT_METHOD_CM108_HID = 2` — wired-GPIO CM108 dongles
pub const PTT_METHOD_CM108_HID: i32 = 2;
/// proto `PTT_METHOD_AIOC_CDC_DTR = 3` — AIOC firmware ≥1.2.0 CDC-ACM DTR
pub const PTT_METHOD_AIOC_CDC_DTR: i32 = 3;
/// proto `PTT_METHOD_VOX = 4` — no PTT wire; audio drives VOX
pub const PTT_METHOD_VOX: i32 = 4;
/// proto `PTT_METHOD_DIGIRIG_TONE = 5` — no PTT wire; a right-channel tone
/// keys the Digirig Lite while AFSK plays on the left (Android tone PTT).
pub const PTT_METHOD_DIGIRIG_TONE: i32 = 5;

#[cfg(test)]
mod tests {
    use super::*;

    /// Asserts the five PTT method constants match spec Appendix B exactly.
    /// Failing here means the Rust constants diverged from the proto — fix
    /// both before landing.
    #[test]
    fn ptt_method_constants_match_spec_appendix_b() {
        assert_eq!(PTT_METHOD_UNKNOWN, 0);
        assert_eq!(PTT_METHOD_CP2102N_RTS, 1);
        assert_eq!(PTT_METHOD_CM108_HID, 2);
        assert_eq!(PTT_METHOD_AIOC_CDC_DTR, 3);
        assert_eq!(PTT_METHOD_VOX, 4);
        assert_eq!(PTT_METHOD_DIGIRIG_TONE, 5);
    }
}
