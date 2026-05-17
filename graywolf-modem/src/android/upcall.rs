//! JNI upcall helpers — Rust → Kotlin callbacks for PTT and TX audio.
//!
//! **Android runtime path** (`target_os = "android"`): each helper attaches
//! the current thread to the JVM, looks up a cached `GlobalRef` + `JMethodID`,
//! invokes the Kotlin callback, and returns.
//!
//! **Host stub path** (`feature = "android-test-stub"`): helpers dispatch into
//! `Mutex<Option<Box<dyn Fn>>>` closures installed by tests. No JVM involved.
//!
//! Only one of the two paths compiles at a time; the cfg gates are mutually
//! exclusive at the call-site level (the `android/mod.rs` JNI exports gate on
//! `target_os = "android"`, while tests run on the host with the feature flag).

#[cfg(all(target_os = "android", not(feature = "android-test-stub")))]
mod android_impl {
    use std::sync::{Mutex, OnceLock};

    use jni::objects::{GlobalRef, JMethodID, JObject, JShortArray, JValue};
    use jni::JavaVM;
    use log::error;

    // ── PTT callback storage ─────────────────────────────────────────────────

    struct PttCallback {
        obj: GlobalRef,
        method: JMethodID,
    }
    // SAFETY: GlobalRef + JMethodID are valid across threads; we only mutate
    // under the Mutex and never expose raw pointers.
    unsafe impl Send for PttCallback {}

    static PTT_CB: OnceLock<Mutex<Option<PttCallback>>> = OnceLock::new();

    fn ptt_slot() -> &'static Mutex<Option<PttCallback>> {
        PTT_CB.get_or_init(|| Mutex::new(None))
    }

    // ── AudioTx callback storage ──────────────────────────────────────────────

    struct AudioTxCallback {
        obj: GlobalRef,
        method: JMethodID,
    }
    unsafe impl Send for AudioTxCallback {}

    static AUDIO_TX_CB: OnceLock<Mutex<Option<AudioTxCallback>>> = OnceLock::new();

    fn audio_tx_slot() -> &'static Mutex<Option<AudioTxCallback>> {
        AUDIO_TX_CB.get_or_init(|| Mutex::new(None))
    }

    // ── Install helpers (called from JNI exports in mod.rs) ──────────────────

    /// Store the Kotlin `UsbPttCallback` instance + resolved `pttSet(IZ)Z` method ID.
    ///
    /// The `env` and `obj` come directly from the JNI export; we promote `obj`
    /// to a `GlobalRef` so it survives beyond the JNI frame. Replaces any prior
    /// installation; the old `GlobalRef` is dropped, returning the slot to the
    /// JVM reference table.
    pub(super) fn install_ptt(env: &mut jni::JNIEnv<'_>, obj: JObject<'_>) {
        let global = match env.new_global_ref(&obj) {
            Ok(g) => g,
            Err(e) => {
                error!("installPttCallback: new_global_ref failed: {e}");
                return;
            }
        };
        // Resolve `pttSet(int method, boolean keyed) -> boolean`
        let method = match env.get_method_id(&obj, "pttSet", "(IZ)Z") {
            Ok(m) => m,
            Err(e) => {
                error!("installPttCallback: get_method_id(pttSet) failed: {e}");
                return;
            }
        };
        *ptt_slot().lock().unwrap() = Some(PttCallback { obj: global, method });
        log::info!("installPttCallback: installed");
    }

    /// Store the Kotlin `AudioTxCallback` instance + resolved `pushSamples([SI)I`
    /// method ID. Replaces any prior installation.
    pub(super) fn install_audio_tx(env: &mut jni::JNIEnv<'_>, obj: JObject<'_>) {
        let global = match env.new_global_ref(&obj) {
            Ok(g) => g,
            Err(e) => {
                error!("installAudioTxCallback: new_global_ref failed: {e}");
                return;
            }
        };
        let method = match env.get_method_id(&obj, "pushSamples", "([SI)I") {
            Ok(m) => m,
            Err(e) => {
                error!("installAudioTxCallback: get_method_id(pushSamples) failed: {e}");
                return;
            }
        };
        *audio_tx_slot().lock().unwrap() = Some(AudioTxCallback { obj: global, method });
        log::info!("installAudioTxCallback: installed");
    }

    // ── Upcall helpers ────────────────────────────────────────────────────────

    fn get_vm() -> Result<JavaVM, String> {
        let ctx = ndk_context::android_context();
        // SAFETY: ndk_context stores the JavaVM pointer installed in JNI_OnLoad.
        unsafe { JavaVM::from_raw(ctx.vm().cast()) }
            .map_err(|e| format!("JavaVM::from_raw: {e}"))
    }

    /// Invoke the installed `UsbPttCallback.pttSet(method, keyed) -> boolean`.
    ///
    /// Returns `Err` when:
    /// - no callback is installed
    /// - JVM attach or call fails
    /// - Kotlin returned `false` (actuator reported failure)
    pub(crate) fn jni_ptt_set(method: i32, keyed: bool) -> Result<(), String> {
        let vm = get_vm()?;
        let mut env = vm
            .attach_current_thread()
            .map_err(|e| format!("pttSet: attach_current_thread: {e}"))?;

        let slot = ptt_slot().lock().unwrap();
        let cb = slot
            .as_ref()
            .ok_or_else(|| "no PTT callback installed".to_string())?;

        let keyed_jni = jni::sys::JNI_TRUE.min(1) * keyed as u8;

        // SAFETY: method ID was resolved against the same object class at
        // install time; GlobalRef keeps the object alive.
        let result = unsafe {
            env.call_method_unchecked(
                cb.obj.as_obj(),
                cb.method,
                jni::signature::ReturnType::Primitive(jni::signature::Primitive::Boolean),
                &[
                    jni::sys::jvalue { i: method },
                    jni::sys::jvalue { z: keyed_jni as u8 },
                ],
            )
        }
        .map_err(|e| format!("pttSet JNI call failed: {e}"))?;

        let returned = result
            .z()
            .map_err(|e| format!("pttSet bad return type: {e}"))?;

        if returned {
            Ok(())
        } else {
            Err(format!("pttSet(method={method}, keyed={keyed}) returned false"))
        }
    }

    /// Invoke the installed `AudioTxCallback.pushSamples(samples, count) -> int`.
    ///
    /// Returns the `int` the Kotlin side returned (matches `AudioTrack.write`
    /// convention: bytes written or a negative error code).
    ///
    /// Returns `Err` when:
    /// - no callback is installed
    /// - JVM attach, array allocation, or call fails
    pub(crate) fn jni_tx_push_samples(buf: &[i16]) -> Result<i32, String> {
        let vm = get_vm()?;
        let mut env = vm
            .attach_current_thread()
            .map_err(|e| format!("tx_push_samples: attach_current_thread: {e}"))?;

        let slot = audio_tx_slot().lock().unwrap();
        let cb = slot
            .as_ref()
            .ok_or_else(|| "no AudioTx callback installed".to_string())?;

        // Allocate a JVM short[] and fill it.
        let arr: JShortArray = env
            .new_short_array(buf.len() as jni::sys::jsize)
            .map_err(|e| format!("tx_push_samples: new_short_array: {e}"))?;
        env.set_short_array_region(&arr, 0, buf)
            .map_err(|e| format!("tx_push_samples: set_short_array_region: {e}"))?;

        let count = buf.len() as i32;

        // SAFETY: method ID and GlobalRef are valid for this callback object.
        let result = unsafe {
            env.call_method_unchecked(
                cb.obj.as_obj(),
                cb.method,
                jni::signature::ReturnType::Primitive(jni::signature::Primitive::Int),
                &[
                    jni::sys::jvalue {
                        l: arr.as_raw() as *mut _,
                    },
                    jni::sys::jvalue { i: count },
                ],
            )
        }
        .map_err(|e| format!("tx_push_samples JNI call failed: {e}"))?;

        result
            .i()
            .map_err(|e| format!("tx_push_samples bad return type: {e}"))
    }
}

// ── Host stub (android-test-stub feature) ────────────────────────────────────

#[cfg(feature = "android-test-stub")]
#[allow(dead_code)] // jni_ptt_set / jni_tx_push_samples called by T3/T4 (pending)
mod stub_impl {
    use std::sync::Mutex;

    static PTT_MOCK: Mutex<Option<Box<dyn Fn(i32, bool) -> bool + Send + Sync>>> =
        Mutex::new(None);
    static AUDIO_TX_MOCK: Mutex<Option<Box<dyn Fn(&[i16]) -> i32 + Send + Sync>>> =
        Mutex::new(None);

    /// Test-only: install a closure that receives `pttSet(method, keyed)` calls.
    /// Returns the `bool` the closure produces; `true` → `Ok(())`, `false` → `Err`.
    pub fn install_ptt_mock<F>(f: F)
    where
        F: Fn(i32, bool) -> bool + Send + Sync + 'static,
    {
        *PTT_MOCK.lock().unwrap() = Some(Box::new(f));
    }

    /// Test-only: install a closure that receives `pushSamples` calls.
    /// The return value flows back as `Ok(n)`.
    pub fn install_audio_tx_mock<F>(f: F)
    where
        F: Fn(&[i16]) -> i32 + Send + Sync + 'static,
    {
        *AUDIO_TX_MOCK.lock().unwrap() = Some(Box::new(f));
    }

    /// Test-only: clear both mocks. Call between test cases to reset state.
    pub fn clear_mocks() {
        *PTT_MOCK.lock().unwrap() = None;
        *AUDIO_TX_MOCK.lock().unwrap() = None;
    }

    pub(crate) fn jni_ptt_set(method: i32, keyed: bool) -> Result<(), String> {
        let guard = PTT_MOCK.lock().unwrap();
        let f = guard
            .as_ref()
            .ok_or_else(|| "no PTT callback installed".to_string())?;
        if f(method, keyed) {
            Ok(())
        } else {
            Err(format!("pttSet(method={method}, keyed={keyed}) returned false"))
        }
    }

    pub(crate) fn jni_tx_push_samples(buf: &[i16]) -> Result<i32, String> {
        let guard = AUDIO_TX_MOCK.lock().unwrap();
        let f = guard
            .as_ref()
            .ok_or_else(|| "no AudioTx callback installed".to_string())?;
        Ok(f(buf))
    }
}

// ── Public surface — re-export whichever impl is active ──────────────────────

#[cfg(all(target_os = "android", not(feature = "android-test-stub")))]
pub(crate) use android_impl::{install_audio_tx, install_ptt, jni_ptt_set, jni_tx_push_samples};

// These helpers are pub(crate) so T3/T4 can call them. They have no callers
// yet in stub mode (T3 and T4 are pending tasks), so suppress the warning.
#[cfg(feature = "android-test-stub")]
#[allow(unused_imports)]
pub(crate) use stub_impl::{jni_ptt_set, jni_tx_push_samples};
#[cfg(feature = "android-test-stub")]
pub use stub_impl::{clear_mocks, install_audio_tx_mock, install_ptt_mock};

// (When building on the host without either flag, this module exposes nothing
//  and the android/ pub mod declaration below is itself cfg-gated, so the
//  missing symbols don't propagate to other modules.)

// ── Unit tests (stub mode only) ───────────────────────────────────────────────

#[cfg(all(test, feature = "android-test-stub"))]
mod tests {
    use super::stub_impl::{clear_mocks, install_audio_tx_mock, install_ptt_mock};
    use super::{jni_ptt_set, jni_tx_push_samples};

    // --- PTT tests -----------------------------------------------------------

    #[test]
    fn ptt_set_without_mock_returns_err() {
        clear_mocks();
        let err = jni_ptt_set(1, true).unwrap_err();
        assert!(
            err.contains("no PTT callback installed"),
            "unexpected message: {err}"
        );
        clear_mocks();
    }

    #[test]
    fn ptt_set_with_mock_returning_true_returns_ok() {
        clear_mocks();
        use std::sync::{Arc, Mutex};
        let observed: Arc<Mutex<Option<(i32, bool)>>> = Arc::new(Mutex::new(None));
        let observed2 = observed.clone();
        install_ptt_mock(move |m, k| {
            *observed2.lock().unwrap() = Some((m, k));
            true
        });
        assert!(jni_ptt_set(2, true).is_ok());
        assert_eq!(*observed.lock().unwrap(), Some((2, true)));
        clear_mocks();
    }

    #[test]
    fn ptt_set_with_mock_returning_false_returns_err_with_returned_false() {
        clear_mocks();
        install_ptt_mock(|_, _| false);
        let err = jni_ptt_set(3, false).unwrap_err();
        assert!(
            err.contains("returned false"),
            "unexpected message: {err}"
        );
        clear_mocks();
    }

    // --- AudioTx tests -------------------------------------------------------

    #[test]
    fn tx_push_without_mock_returns_err() {
        clear_mocks();
        let err = jni_tx_push_samples(&[1, 2, 3]).unwrap_err();
        assert!(
            err.contains("no AudioTx callback installed"),
            "unexpected message: {err}"
        );
        clear_mocks();
    }

    #[test]
    fn tx_push_with_mock_receives_slice_content() {
        clear_mocks();
        use std::sync::{Arc, Mutex};
        let captured: Arc<Mutex<Vec<i16>>> = Arc::new(Mutex::new(Vec::new()));
        let captured2 = captured.clone();
        install_audio_tx_mock(move |buf| {
            *captured2.lock().unwrap() = buf.to_vec();
            buf.len() as i32 * 2 // bytes written
        });
        let samples: &[i16] = &[10, 20, 30];
        let ret = jni_tx_push_samples(samples).unwrap();
        assert_eq!(ret, 6); // 3 samples × 2 bytes
        assert_eq!(*captured.lock().unwrap(), vec![10i16, 20, 30]);
        clear_mocks();
    }

    #[test]
    fn tx_push_mock_return_value_flows_back_as_ok() {
        clear_mocks();
        install_audio_tx_mock(|_| 42);
        let ret = jni_tx_push_samples(&[0; 8]).unwrap();
        assert_eq!(ret, 42);
        clear_mocks();
    }

    #[test]
    fn clear_mocks_resets_both_callbacks() {
        install_ptt_mock(|_, _| true);
        install_audio_tx_mock(|_| 0);
        clear_mocks();
        assert!(jni_ptt_set(1, true).is_err());
        assert!(jni_tx_push_samples(&[1]).is_err());
    }
}
