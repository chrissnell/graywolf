//! TX audio helper — Rust → Kotlin `AudioTxPump.pushSamples` upcall.
//!
//! This file is a sibling of `audio.rs` (RX path) but lives under its own
//! cfg gate so tests can run on the host with `android-test-stub` without
//! dragging in the android-only RX machinery (`config_state`, level accumulator).

// android-test-stub extends this to the host so stub-mode tests can exercise
// tx_emit_samples via crate::jni_tx_push_samples without a JVM.
#![cfg(any(target_os = "android", feature = "android-test-stub"))]

/// [`TxSink`] implementation that pushes PCM samples to Kotlin's
/// `AudioTxPump` via the cached JNI callback. Replaces the cpal
/// `AudioSink` on Android where cpal cannot reach the `AudioTrack`
/// instance that `AudioTxPump` holds open and routes to the USB OTG
/// dongle via `setPreferredDevice`.
///
/// `AudioTrack.write(WRITE_BLOCKING)` blocks until the samples are
/// accepted into the AudioTrack ring buffer (typically consumed by the
/// hardware during the call), so by the time `tx_emit_samples` returns
/// the samples are effectively drained. `drained_samples` therefore
/// tracks the cumulative submitted count so `drive_tx_cycle`'s drain
/// loop exits immediately on the first `drained_samples() >= watermark`
/// check.
pub struct AndroidTxSink {
    drained: std::sync::atomic::AtomicUsize,
}

impl AndroidTxSink {
    pub fn new() -> Self {
        Self {
            drained: std::sync::atomic::AtomicUsize::new(0),
        }
    }
}

impl Default for AndroidTxSink {
    fn default() -> Self {
        Self::new()
    }
}

impl crate::modem::TxSink for AndroidTxSink {
    fn submit(&self, samples: Vec<i16>) -> Result<usize, String> {
        let n = tx_emit_samples(&samples)?;
        self.drained
            .fetch_add(n, std::sync::atomic::Ordering::Release);
        Ok(n)
    }

    fn drained_samples(&self) -> usize {
        self.drained.load(std::sync::atomic::Ordering::Acquire)
    }
}

/// Push a PCM buffer to the Kotlin `AudioTxPump.pushSamples` via JNI.
///
/// Called by the modem TX governor on every rendered PCM frame. This function
/// **blocks** for the duration of the Kotlin-side `AudioTrack.write` call
/// (WRITE_BLOCKING mode, per spec §3.2); the Rust TX thread is expected to
/// block here while the audio drains into the USB output buffer.
///
/// Return semantics mirror `AudioTrack.write`:
/// - `Ok(n)` — Kotlin returned `n >= 0` (bytes or samples written). A short
///   write (`n < buf.len()`) is returned as `Ok(n)`; detecting and handling
///   underruns is the TX governor's responsibility.
/// - `Err(msg)` — Kotlin returned a negative error code (ERROR=-1,
///   ERROR_BAD_VALUE=-2, ERROR_INVALID_OPERATION=-3, ERROR_DEAD_OBJECT=-6)
///   or the JNI call itself failed. The message includes both the error code
///   and the input length for log attribution.
///
/// Empty buffers short-circuit before the JNI call and return `Ok(0)`.
pub fn tx_emit_samples(buf: &[i16]) -> Result<usize, String> {
    if buf.is_empty() {
        return Ok(0);
    }
    match crate::jni_tx_push_samples(buf)? {
        n if n < 0 => Err(format!(
            "AudioTxPump.pushSamples returned {} for {} samples",
            n,
            buf.len()
        )),
        n => Ok(n as usize),
    }
}

#[cfg(test)]
#[cfg(feature = "android-test-stub")]
mod tests {
    use std::sync::atomic::{AtomicBool, Ordering};
    use std::sync::{Arc, Mutex};

    use serial_test::serial;

    use super::{tx_emit_samples, AndroidTxSink};
    use crate::modem::TxSink;

    #[test]
    #[serial]
    fn tx_emit_samples_empty_buf_returns_ok_zero_without_calling_jni() {
        crate::clear_mocks();
        let called = Arc::new(AtomicBool::new(false));
        let called2 = called.clone();
        crate::install_audio_tx_mock(move |_| {
            called2.store(true, Ordering::SeqCst);
            0
        });
        let result = tx_emit_samples(&[]);
        assert_eq!(result, Ok(0));
        assert!(!called.load(Ordering::SeqCst), "JNI mock must not be called for empty buf");
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn tx_emit_samples_forwards_buffer_to_mock() {
        crate::clear_mocks();
        let received: Arc<Mutex<Vec<i16>>> = Arc::new(Mutex::new(Vec::new()));
        let received2 = received.clone();
        crate::install_audio_tx_mock(move |buf| {
            *received2.lock().unwrap() = buf.to_vec();
            buf.len() as i32
        });
        let _ = tx_emit_samples(&[1i16, 2, 3]);
        assert_eq!(*received.lock().unwrap(), vec![1i16, 2, 3]);
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn tx_emit_samples_propagates_positive_return_as_ok_usize() {
        crate::clear_mocks();
        crate::install_audio_tx_mock(|_| 3);
        let result = tx_emit_samples(&[0i16, 0, 0]);
        assert_eq!(result, Ok(3));
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn tx_emit_samples_short_write_is_ok_not_err() {
        crate::clear_mocks();
        crate::install_audio_tx_mock(|_| 1); // partial — only 1 of 3
        let result = tx_emit_samples(&[0i16, 0, 0]);
        assert_eq!(result, Ok(1));
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn tx_emit_samples_negative_return_becomes_err_with_context() {
        crate::clear_mocks();
        crate::install_audio_tx_mock(|_| -2);
        let err = tx_emit_samples(&[0i16, 0, 0]).unwrap_err();
        assert!(err.contains("-2"), "message should contain error code: {err}");
        assert!(err.contains('3'), "message should contain input length: {err}");
        crate::clear_mocks();
    }

    // ── AndroidTxSink tests ───────────────────────────────────────────────────

    #[test]
    #[serial]
    fn android_tx_sink_submit_forwards_buffer_and_accumulates_drained() {
        crate::clear_mocks();
        let received: Arc<Mutex<Vec<i16>>> = Arc::new(Mutex::new(Vec::new()));
        let received2 = received.clone();
        crate::install_audio_tx_mock(move |buf| {
            *received2.lock().unwrap() = buf.to_vec();
            buf.len() as i32
        });
        let sink = AndroidTxSink::new();
        let result = <AndroidTxSink as TxSink>::submit(&sink, vec![1i16, 2, 3]);
        assert_eq!(result, Ok(3), "submit should return sample count");
        assert_eq!(
            *received.lock().unwrap(),
            vec![1i16, 2, 3],
            "mock must receive the exact buffer"
        );
        assert_eq!(
            sink.drained_samples(),
            3,
            "drained_samples must equal submitted count"
        );
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn android_tx_sink_submit_error_does_not_accumulate_drained() {
        crate::clear_mocks();
        // Mock returns -1 (ERROR) → tx_emit_samples propagates Err.
        crate::install_audio_tx_mock(|_| -1);
        let sink = AndroidTxSink::new();
        let result = <AndroidTxSink as TxSink>::submit(&sink, vec![0i16, 0, 0]);
        assert!(result.is_err(), "submit should propagate the error");
        assert_eq!(
            sink.drained_samples(),
            0,
            "drained_samples must not advance on error"
        );
        crate::clear_mocks();
    }
}
