//! Android PTT driver — proxies key/unkey through to Kotlin's UsbPttAdapter
//! via the JNI upcall helpers in graywolf-modem::android::upcall.
//!
//! The `method` field carries one of the spec-Appendix-B PttMethod int
//! values (locked in `crate::tx::ptt_android_consts`). The Kotlin side
//! interprets that to pick which transport (CP2102N RTS, CDC-ACM DTR,
//! CM108 HID, VOX) to actuate.

#![cfg(any(target_os = "android", feature = "android-test-stub"))]

use super::ptt::PttDriver;

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
        crate::jni_ptt_set(self.method, true)
            .map_err(|e| format!("android ptt key (method={}): {}", self.method, e))
    }

    fn unkey(&mut self) -> Result<(), String> {
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
