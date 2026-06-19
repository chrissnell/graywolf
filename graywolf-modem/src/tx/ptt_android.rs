//! Android PTT driver — proxies key/unkey through to Kotlin's UsbPttAdapter
//! via the JNI upcall helpers in graywolf-modem::android::upcall.
//!
//! The `method` field carries one of the spec-Appendix-B PttMethod int
//! values (locked in `crate::tx::ptt_android_consts`). The Kotlin side
//! interprets that to pick which transport (CP2102N RTS, CDC-ACM DTR,
//! CM108 HID, VOX) to actuate.

#![cfg(any(target_os = "android", feature = "android-test-stub"))]

use super::ptt::PttDriver;
use super::ptt_android_consts::PTT_METHOD_DIGIRIG_TONE;

pub(crate) struct AndroidPtt {
    method: i32,
}

impl AndroidPtt {
    pub(crate) fn new(method: i32) -> Self {
        Self { method }
    }
}

impl PttDriver for AndroidPtt {
    fn key(&mut self) -> Result<(), String> {
        if self.method == PTT_METHOD_DIGIRIG_TONE {
            // Digirig Lite tone PTT: keying is done by a right-channel tone
            // synthesised in Kotlin's AudioTxPump, not a USB line. Start it at
            // the channel's current mark frequency.
            let hz = crate::config_state::mark_freq() as i32;
            return crate::jni_audio_set_tone(true, hz)
                .map_err(|e| format!("android digirig tone key (hz={hz}): {e}"));
        }
        crate::jni_ptt_set(self.method, true)
            .map_err(|e| format!("android ptt key (method={}): {}", self.method, e))
    }

    fn unkey(&mut self) -> Result<(), String> {
        if self.method == PTT_METHOD_DIGIRIG_TONE {
            let hz = crate::config_state::mark_freq() as i32;
            return crate::jni_audio_set_tone(false, hz)
                .map_err(|e| format!("android digirig tone unkey: {e}"));
        }
        crate::jni_ptt_set(self.method, false)
            .map_err(|e| format!("android ptt unkey (method={}): {}", self.method, e))
    }
}

#[cfg(test)]
#[cfg(feature = "android-test-stub")]
mod tests {
    use super::*;
    use serial_test::serial;
    use std::sync::{Arc, Mutex};

    #[test]
    #[serial]
    fn digirig_tone_method_keys_via_tone_upcall_not_ptt() {
        use crate::tx::ptt_android_consts::PTT_METHOD_DIGIRIG_TONE;
        crate::clear_mocks();
        // A known mark frequency so we can assert it is forwarded.
        crate::config_state::set_channel_dsp(1200, 1500, 2200);
        let tone: Arc<Mutex<Option<(bool, i32)>>> = Arc::new(Mutex::new(None));
        let tone2 = tone.clone();
        crate::install_tone_mock(move |active, hz| {
            *tone2.lock().unwrap() = Some((active, hz));
        });
        // pttSet must never fire for the tone method.
        crate::install_ptt_mock(|_, _| panic!("method 5 must not call pttSet"));

        let mut ptt = AndroidPtt::new(PTT_METHOD_DIGIRIG_TONE);
        ptt.key().expect("tone key should succeed");
        assert_eq!(
            *tone.lock().unwrap(),
            Some((true, 1500)),
            "key must start the tone at the channel mark frequency"
        );

        ptt.unkey().expect("tone unkey should succeed");
        assert_eq!(
            tone.lock().unwrap().unwrap().0,
            false,
            "unkey must stop the tone"
        );
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn key_invokes_callback_with_method_and_true() {
        crate::clear_mocks();
        let observed: Arc<Mutex<Option<(i32, bool)>>> = Arc::new(Mutex::new(None));
        let observed2 = observed.clone();
        crate::install_ptt_mock(move |m, k| {
            *observed2.lock().unwrap() = Some((m, k));
            true
        });

        let mut ptt = AndroidPtt::new(1); // PTT_METHOD_CP2102N_RTS
        ptt.key().expect("key should succeed when mock returns true");

        assert_eq!(
            *observed.lock().unwrap(),
            Some((1, true)),
            "key() must invoke callback with (method, true)"
        );
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn unkey_invokes_callback_with_method_and_false() {
        crate::clear_mocks();
        let observed: Arc<Mutex<Option<(i32, bool)>>> = Arc::new(Mutex::new(None));
        let observed2 = observed.clone();
        crate::install_ptt_mock(move |m, k| {
            *observed2.lock().unwrap() = Some((m, k));
            true
        });

        let mut ptt = AndroidPtt::new(3); // PTT_METHOD_AIOC_CDC_DTR
        ptt.unkey().expect("unkey should succeed when mock returns true");

        assert_eq!(
            *observed.lock().unwrap(),
            Some((3, false)),
            "unkey() must invoke callback with (method, false)"
        );
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn callback_failure_propagates_with_context() {
        // A mock that returns false should surface as an Err whose message
        // contains both "android ptt key" and the method int.
        crate::clear_mocks();
        crate::install_ptt_mock(|_, _| false);

        let mut ptt = AndroidPtt::new(2); // PTT_METHOD_CM108_HID
        let err = ptt.key().expect_err("key should fail when mock returns false");

        assert!(
            err.contains("android ptt key"),
            "error must mention 'android ptt key'; got: {err}"
        );
        assert!(
            err.contains('2') || err.contains("method=2"),
            "error must contain the method int; got: {err}"
        );
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn all_four_valid_method_ints_round_trip_through_key() {
        use crate::tx::ptt_android_consts::{
            PTT_METHOD_AIOC_CDC_DTR, PTT_METHOD_CM108_HID, PTT_METHOD_CP2102N_RTS, PTT_METHOD_VOX,
        };

        for &method in &[
            PTT_METHOD_CP2102N_RTS,
            PTT_METHOD_CM108_HID,
            PTT_METHOD_AIOC_CDC_DTR,
            PTT_METHOD_VOX,
        ] {
            crate::clear_mocks();
            let seen: Arc<Mutex<Option<i32>>> = Arc::new(Mutex::new(None));
            let seen2 = seen.clone();
            crate::install_ptt_mock(move |m, _| {
                *seen2.lock().unwrap() = Some(m);
                true
            });

            let mut ptt = AndroidPtt::new(method);
            ptt.key().unwrap_or_else(|e| panic!("key failed for method {method}: {e}"));

            assert_eq!(
                *seen.lock().unwrap(),
                Some(method),
                "callback must receive method={method}"
            );
        }
        crate::clear_mocks();
    }
}
