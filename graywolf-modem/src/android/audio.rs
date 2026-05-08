//! Sample sink for the Kotlin -> Rust JNI hand-off.
//!
//! Kotlin owns `android.media.AudioRecord` (POC-A established this path
//! works on USB-Audio class devices where AAudio rail-pins capture).
//! Each chunk arrives via `modemPushSamples(short[], int len)`. We apply
//! the operator-set software gain (invariant N9), then forward the chunk
//! into the existing demod input channel for the demod thread to consume.

#![cfg(target_os = "android")]

use std::sync::atomic::{AtomicI32, Ordering};
use std::sync::mpsc::SyncSender;

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

/// Apply gain, clamp to i16, copy into a fresh Vec for the demod queue.
/// Returns Err if the demod queue is closed (modem stopped); the caller
/// (JNI push) treats that as a no-op.
pub fn ingest(samples: &[i16], tx: &SyncSender<Vec<i16>>) -> Result<(), ()> {
    let q15 = GAIN_Q15.load(Ordering::Relaxed);
    let mut chunk: Vec<i16> = Vec::with_capacity(samples.len());
    for &s in samples {
        let v = (s as i32 * q15) >> 15;
        chunk.push(v.clamp(i16::MIN as i32, i16::MAX as i32) as i16);
    }
    // try_send: if the demod can't keep up we drop. Better than blocking
    // the JNI thread (which is Kotlin's high-priority audio thread).
    let _ = tx.try_send(chunk);
    Ok(())
}
