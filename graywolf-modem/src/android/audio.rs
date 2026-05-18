//! Sample sink for the Kotlin -> Rust JNI hand-off.
//!
//! Kotlin owns `android.media.AudioRecord` (POC-A established this path
//! works on USB-Audio class devices where AAudio rail-pins capture).
//! Each chunk arrives via `modemPushSamples(short[], int len)`. We apply
//! the operator-set software gain (invariant N9), forward the chunk to
//! the demod thread, and emit a `DeviceLevelUpdate` at ~5 Hz so the SPA
//! input-level meter has something to render.

#![cfg(target_os = "android")]

use std::sync::atomic::{AtomicI32, Ordering};
use std::sync::mpsc::SyncSender;
use std::sync::{Mutex, OnceLock};
use std::time::{Duration, Instant};

use crate::ipc::proto::DeviceLevelUpdate;

/// Q15 fixed-point gain. -6 dB default to match POC-A's run-report value.
/// Updated via `Java_…_modemSetGainDb`. Read on every push.
pub static GAIN_Q15: AtomicI32 = AtomicI32::new(0);

pub fn db_to_q15(db_value: f32) -> i32 {
    let lin = 10f32.powf(db_value / 20.0);
    (lin * (1 << 15) as f32) as i32
}

pub fn set_gain_db(db_value: f32) {
    let q = db_to_q15(db_value.clamp(-30.0, 20.0));
    GAIN_Q15.store(q, Ordering::Relaxed);
}

/// Bus for outgoing DeviceLevelUpdate messages. Set by the demod thread
/// (run_demod) before any JNI ingest call can arrive; cleared on stop.
static LEVEL_TX: OnceLock<Mutex<Option<SyncSender<DeviceLevelUpdate>>>> = OnceLock::new();

fn level_tx_slot() -> &'static Mutex<Option<SyncSender<DeviceLevelUpdate>>> {
    LEVEL_TX.get_or_init(|| Mutex::new(None))
}

pub fn install_level_tx(tx: SyncSender<DeviceLevelUpdate>) {
    *level_tx_slot().lock().unwrap() = Some(tx);
}

pub fn clear_level_tx() {
    *level_tx_slot().lock().unwrap() = None;
}

/// Per-process accumulator for peak/rms over a 200 ms window. Emits one
/// DeviceLevelUpdate per window so the SPA's level meter ticks at 5 Hz
/// without flooding the IPC channel. Reset every emit.
struct LevelAccum {
    peak_abs: f32,
    sum_sq: f64,
    n: usize,
    window_start: Instant,
}

impl LevelAccum {
    fn new() -> Self {
        Self { peak_abs: 0.0, sum_sq: 0.0, n: 0, window_start: Instant::now() }
    }
    fn push(&mut self, samples: &[i16]) {
        for &s in samples {
            let a = (s as f32).abs();
            if a > self.peak_abs {
                self.peak_abs = a;
            }
            self.sum_sq += (s as f64) * (s as f64);
        }
        self.n += samples.len();
    }
    fn ready_to_emit(&self, now: Instant) -> bool {
        now.duration_since(self.window_start) >= Duration::from_millis(200) && self.n > 0
    }
    fn drain(&mut self) -> (f32, f32, bool) {
        let peak_linear = self.peak_abs / 32768.0;
        let rms_linear = if self.n > 0 {
            ((self.sum_sq / self.n as f64).sqrt() / 32768.0) as f32
        } else {
            0.0
        };
        let peak_dbfs = if peak_linear > 0.0 {
            (20.0 * peak_linear.log10()).max(-60.0)
        } else {
            -60.0
        };
        let rms_dbfs = if rms_linear > 0.0 {
            (20.0 * rms_linear.log10()).max(-60.0)
        } else {
            -60.0
        };
        // Clipping: peak within ~-1 dBFS of full-scale.
        let clipping = self.peak_abs >= 32000.0;
        // Reset.
        self.peak_abs = 0.0;
        self.sum_sq = 0.0;
        self.n = 0;
        self.window_start = Instant::now();
        (peak_dbfs, rms_dbfs, clipping)
    }
}

static ACCUM: OnceLock<Mutex<LevelAccum>> = OnceLock::new();
fn accum() -> &'static Mutex<LevelAccum> {
    ACCUM.get_or_init(|| Mutex::new(LevelAccum::new()))
}

/// Apply gain, clamp to i16, copy into a fresh Vec for the demod queue.
/// Also fold the gain'd samples into the level accumulator and emit a
/// DeviceLevelUpdate when the 200 ms window closes.
///
/// Returns Err if the demod queue is closed (modem stopped); the caller
/// (JNI push) treats that as a no-op.
pub fn ingest(samples: &[i16], tx: &SyncSender<Vec<i16>>) -> Result<(), ()> {
    let q15 = GAIN_Q15.load(Ordering::Relaxed);
    let mut chunk: Vec<i16> = Vec::with_capacity(samples.len());
    for &s in samples {
        let v = (s as i32 * q15) >> 15;
        chunk.push(v.clamp(i16::MIN as i32, i16::MAX as i32) as i16);
    }

    // Update level accumulator with the post-gain chunk.
    {
        let mut a = accum().lock().unwrap();
        a.push(&chunk);
        let now = Instant::now();
        if a.ready_to_emit(now) {
            let (peak_dbfs, rms_dbfs, clipping) = a.drain();
            // Drop the lock before crossing the IPC channel boundary.
            drop(a);
            let device_id = super::config_state::input_device_id();
            let msg = DeviceLevelUpdate { device_id, peak_dbfs, rms_dbfs, clipping };
            if let Some(level_tx) = level_tx_slot().lock().unwrap().as_ref() {
                let _ = level_tx.try_send(msg);
            }
        }
    }

    // try_send: if the demod can't keep up we drop. Better than blocking
    // the JNI thread (which is Kotlin's high-priority audio thread).
    let _ = tx.try_send(chunk);
    Ok(())
}

