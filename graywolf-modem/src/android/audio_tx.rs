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
/// `AudioTrack.write(WRITE_BLOCKING)` only blocks until the samples are
/// *accepted into* the AudioTrack ring buffer, not until they have been
/// emitted from the DAC/USB output — so a submitted-count drain signal
/// unkeys PTT while the tail of the buffer (txtail flags, and real frame
/// data when the output latency exceeds the configured txtail) is still
/// in flight, clipping the transmission.
///
/// Instead, `drained_samples` reports the number of frames *physically
/// presented at the output* during this transmission, obtained via the
/// `presentedFrames()` JNI downcall (Kotlin `AudioTrack.getTimestamp`,
/// which includes the full HAL→USB output latency). The AudioTrack head
/// is cumulative across the track's lifetime, so we subtract the position
/// captured at TX start. `drive_tx_cycle`'s drain loop then holds PTT until
/// `drained_samples() >= watermark`, i.e. until the last sample has left
/// the dongle. If the downcall fails we degrade to the submitted count so
/// the wall-clock gate still bounds the wait (never under-reporting → bias
/// toward over-holding PTT, which is harmless dead carrier).
pub struct AndroidTxSink {
    submitted: std::sync::atomic::AtomicUsize,
    baseline: std::sync::OnceLock<i64>,
}

impl AndroidTxSink {
    pub fn new() -> Self {
        Self {
            submitted: std::sync::atomic::AtomicUsize::new(0),
            baseline: std::sync::OnceLock::new(),
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
        // Capture the track's absolute presented-frame position once, before
        // this transmission's audio enters the pipeline, so drained_samples()
        // can report per-TX progress. A failed query → baseline 0; combined
        // with the same-failure fallback in drained_samples(), that keeps the
        // old wall-clock-bounded behaviour rather than wedging.
        if self.baseline.get().is_none() {
            let base = crate::jni_audio_tx_presented_frames().unwrap_or(0);
            let _ = self.baseline.set(base);
        }
        let n = tx_emit_samples(&samples)?;
        let total = self
            .submitted
            .fetch_add(n, std::sync::atomic::Ordering::Release)
            + n;
        Ok(total)
    }

    fn drained_samples(&self) -> usize {
        match crate::jni_audio_tx_presented_frames() {
            Ok(current) => {
                let base = self.baseline.get().copied().unwrap_or(current);
                (current - base).max(0) as usize
            }
            Err(_) => self.submitted.load(std::sync::atomic::Ordering::Acquire),
        }
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
    use crate::install_audio_tx_presented_mock;
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
    fn android_tx_sink_submit_forwards_buffer_and_returns_watermark() {
        crate::clear_mocks();
        let received: Arc<Mutex<Vec<i16>>> = Arc::new(Mutex::new(Vec::new()));
        let received2 = received.clone();
        crate::install_audio_tx_mock(move |buf| {
            *received2.lock().unwrap() = buf.to_vec();
            buf.len() as i32
        });
        install_audio_tx_presented_mock(|| 0);
        let sink = AndroidTxSink::new();
        let result = <AndroidTxSink as TxSink>::submit(&sink, vec![1i16, 2, 3]);
        assert_eq!(result, Ok(3), "submit should return the cumulative watermark");
        assert_eq!(
            *received.lock().unwrap(),
            vec![1i16, 2, 3],
            "mock must receive the exact buffer"
        );
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn android_tx_sink_drained_samples_tracks_presented_minus_baseline() {
        crate::clear_mocks();
        crate::install_audio_tx_mock(|buf| buf.len() as i32);
        // The AudioTrack head is cumulative across the track's lifetime. The
        // sink must subtract the position captured at TX start so it reports
        // per-TX playout progress, not the absolute track position.
        let pos = Arc::new(Mutex::new(1000i64));
        let pos2 = pos.clone();
        install_audio_tx_presented_mock(move || *pos2.lock().unwrap());

        let sink = AndroidTxSink::new();
        // Baseline (1000) is captured on first submit, before audio plays out.
        let _ = <AndroidTxSink as TxSink>::submit(&sink, vec![0i16; 10]);
        assert_eq!(sink.drained_samples(), 0, "nothing presented yet → 0");

        *pos.lock().unwrap() = 1007;
        assert_eq!(sink.drained_samples(), 7, "7 frames past baseline");

        *pos.lock().unwrap() = 1010;
        assert_eq!(sink.drained_samples(), 10, "all 10 frames presented");
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn android_tx_sink_drained_samples_falls_back_to_submitted_on_query_error() {
        crate::clear_mocks();
        crate::install_audio_tx_mock(|buf| buf.len() as i32);
        // No presented mock installed → jni_audio_tx_presented_frames errors.
        // The sink must degrade to the submitted count (old behaviour: never
        // under-report, so the drain wait still bounds on the wall clock).
        let sink = AndroidTxSink::new();
        let _ = <AndroidTxSink as TxSink>::submit(&sink, vec![0i16; 5]);
        assert_eq!(sink.drained_samples(), 5, "fallback to submitted count");
        crate::clear_mocks();
    }

    #[test]
    #[serial]
    fn android_tx_sink_submit_error_does_not_accumulate_submitted() {
        crate::clear_mocks();
        // Mock returns -1 (ERROR) → tx_emit_samples propagates Err.
        crate::install_audio_tx_mock(|_| -1);
        install_audio_tx_presented_mock(|| 0);
        let sink = AndroidTxSink::new();
        let result = <AndroidTxSink as TxSink>::submit(&sink, vec![0i16, 0, 0]);
        assert!(result.is_err(), "submit should propagate the error");
        assert_eq!(
            sink.drained_samples(),
            0,
            "submitted count must not advance on error"
        );
        crate::clear_mocks();
    }
}
