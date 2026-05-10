//! Captured-from-`ConfigureChannel` mapping that the JNI ingest path
//! and the DSP thread consult so emitted IPC messages are tagged with
//! the operator-configured channel + audio device IDs.
//!
//! Without this, `DeviceLevelUpdate` and `ReceivedFrame` would always
//! ship `device_id=0`/`channel=0` (the demodulator's internal indices)
//! regardless of which channel the user wired up in the SPA, and the
//! per-channel `rx_frames` counter + per-device level meter would
//! never tick.
//!
//! On Android there is exactly one audio source (Kotlin's
//! `AudioRecord(MediaRecorder.AudioSource.MIC)` -> JNI `modemPushSamples`)
//! so the mapping is degenerate: whatever channel the operator most
//! recently configured is the one we attribute everything to. Phase 5+
//! may grow this into a real per-device pipe table when output device
//! / TX support lands.

#![cfg(target_os = "android")]

use std::sync::atomic::{AtomicU32, AtomicU64, Ordering};

/// Default device id when no `ConfigureChannel` has been received yet.
/// Matches the `id=1` the configstore assigns to the first user-created
/// AudioDevice row, so a fresh install with one device + one channel
/// gets sane meter/counter behavior even before the SPA's first push.
const DEFAULT_DEVICE_ID: u32 = 1;
const DEFAULT_CHANNEL_ID: u32 = 1;

static INPUT_DEVICE_ID: AtomicU32 = AtomicU32::new(DEFAULT_DEVICE_ID);
static CHANNEL_ID: AtomicU32 = AtomicU32::new(DEFAULT_CHANNEL_ID);

/// Cumulative count of frames the demodulator has emitted since modem
/// start. Read by the IPC writer loop to populate StatusUpdate.rx_frames
/// at ~1 Hz. The Go modembridge status_cache mirrors this into the
/// per-channel ChannelStats the SPA Dashboard polls.
static RX_FRAMES: AtomicU64 = AtomicU64::new(0);

pub fn set_from_configure(channel: u32, input_device_id: u32) {
    if channel != 0 {
        CHANNEL_ID.store(channel, Ordering::Relaxed);
    }
    if input_device_id != 0 {
        INPUT_DEVICE_ID.store(input_device_id, Ordering::Relaxed);
    }
}

pub fn input_device_id() -> u32 {
    INPUT_DEVICE_ID.load(Ordering::Relaxed)
}

pub fn channel_id() -> u32 {
    CHANNEL_ID.load(Ordering::Relaxed)
}

pub fn increment_rx_frames() {
    RX_FRAMES.fetch_add(1, Ordering::Relaxed);
}

pub fn rx_frames() -> u64 {
    RX_FRAMES.load(Ordering::Relaxed)
}
