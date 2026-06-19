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
//!
//! TX DSP parameters (baud, mark/space Hz, txdelay/txtail ms) are also
//! stored here so `TransmitFrame` dispatch in `run_demod` can call
//! `tx::build_samples` with the same parameters that were used to set
//! up the demodulator.

#![cfg(any(target_os = "android", feature = "android-test-stub"))]

use std::sync::atomic::{AtomicBool, AtomicU32, AtomicU64, Ordering};

/// Default device id when no `ConfigureChannel` has been received yet.
/// Matches the `id=1` the configstore assigns to the first user-created
/// AudioDevice row, so a fresh install with one device + one channel
/// gets sane meter/counter behavior even before the SPA's first push.
const DEFAULT_DEVICE_ID: u32 = 1;
const DEFAULT_CHANNEL_ID: u32 = 1;

static INPUT_DEVICE_ID: AtomicU32 = AtomicU32::new(DEFAULT_DEVICE_ID);
static CHANNEL_ID: AtomicU32 = AtomicU32::new(DEFAULT_CHANNEL_ID);

/// AFSK / DSP parameters captured from the most recent ConfigureChannel.
/// Defaults match standard 1200-baud Bell-202 APRS.
static BAUD: AtomicU32 = AtomicU32::new(1200);
static MARK_FREQ: AtomicU32 = AtomicU32::new(1200);
static SPACE_FREQ: AtomicU32 = AtomicU32::new(2200);

/// TX timing captured from the most recent ConfigurePtt.
static TXDELAY_MS: AtomicU32 = AtomicU32::new(300);
static TXTAIL_MS: AtomicU32 = AtomicU32::new(100);

/// Cumulative count of frames the demodulator has emitted since modem
/// start. Read by the IPC writer loop to populate StatusUpdate.rx_frames
/// at ~1 Hz. The Go modembridge status_cache mirrors this into the
/// per-channel ChannelStats the SPA Dashboard polls.
static RX_FRAMES: AtomicU64 = AtomicU64::new(0);

/// Cumulative count of frames handed to the TX worker since modem
/// start. Read by the IPC writer loop to populate StatusUpdate.tx_frames
/// at ~1 Hz. Counts frames successfully enqueued for transmission
/// (build_samples ok + tx_worker.transmit ok) -- the same "dispatched
/// for TX" semantics as the desktop pkg/kiss ChannelStats.TxFrames.
static TX_FRAMES: AtomicU64 = AtomicU64::new(0);

pub fn set_from_configure(channel: u32, input_device_id: u32) {
    if channel != 0 {
        CHANNEL_ID.store(channel, Ordering::Relaxed);
    }
    if input_device_id != 0 {
        INPUT_DEVICE_ID.store(input_device_id, Ordering::Relaxed);
    }
}

/// Capture DSP parameters from ConfigureChannel for later use by TransmitFrame.
pub fn set_channel_dsp(baud: u32, mark_freq: u32, space_freq: u32) {
    if baud != 0 {
        BAUD.store(baud, Ordering::Relaxed);
    }
    if mark_freq != 0 {
        MARK_FREQ.store(mark_freq, Ordering::Relaxed);
    }
    if space_freq != 0 {
        SPACE_FREQ.store(space_freq, Ordering::Relaxed);
    }
}

/// Capture PTT timing from ConfigurePtt for later use by TransmitFrame.
pub fn set_ptt_timing(txdelay_ms: u32, txtail_ms: u32) {
    if txdelay_ms != 0 {
        TXDELAY_MS.store(txdelay_ms, Ordering::Relaxed);
    }
    if txtail_ms != 0 {
        TXTAIL_MS.store(txtail_ms, Ordering::Relaxed);
    }
}

pub fn input_device_id() -> u32 {
    INPUT_DEVICE_ID.load(Ordering::Relaxed)
}

pub fn channel_id() -> u32 {
    CHANNEL_ID.load(Ordering::Relaxed)
}

pub fn baud() -> u32 {
    BAUD.load(Ordering::Relaxed)
}

pub fn mark_freq() -> u32 {
    MARK_FREQ.load(Ordering::Relaxed)
}

/// True when the active channel's PTT method is Digirig Lite tone PTT, so
/// the TX builder prepends the silent left-channel lead-in for it. Set from
/// the `ConfigurePtt` dispatch arm.
static DIGIRIG_TONE: AtomicBool = AtomicBool::new(false);

pub fn set_digirig_tone(on: bool) {
    DIGIRIG_TONE.store(on, Ordering::Relaxed);
}

pub fn digirig_tone() -> bool {
    DIGIRIG_TONE.load(Ordering::Relaxed)
}

pub fn space_freq() -> u32 {
    SPACE_FREQ.load(Ordering::Relaxed)
}

pub fn txdelay_ms() -> u32 {
    TXDELAY_MS.load(Ordering::Relaxed)
}

pub fn txtail_ms() -> u32 {
    TXTAIL_MS.load(Ordering::Relaxed)
}

pub fn increment_rx_frames() {
    RX_FRAMES.fetch_add(1, Ordering::Relaxed);
}

pub fn rx_frames() -> u64 {
    RX_FRAMES.load(Ordering::Relaxed)
}

pub fn increment_tx_frames() {
    TX_FRAMES.fetch_add(1, Ordering::Relaxed);
}

pub fn tx_frames() -> u64 {
    TX_FRAMES.load(Ordering::Relaxed)
}

#[cfg(test)]
mod tests {
    use super::{digirig_tone, set_digirig_tone};

    #[test]
    fn digirig_tone_flag_round_trips() {
        set_digirig_tone(true);
        assert!(digirig_tone());
        set_digirig_tone(false);
        assert!(!digirig_tone());
    }
}
