//! Audio input sources for the modem.
//!
//! Three implementations are provided:
//!
//! - [`soundcard::SoundcardSource`] — live input from a `cpal` device
//! - [`flac::FlacSource`] — realtime playback of a FLAC file, pacing samples
//!   at the file's native rate so downstream DSP sees the same timing as a
//!   live radio
//! - [`stdin_raw::StdinRawSource`] — raw little-endian i16 PCM on stdin
//!
//! Every source runs on a dedicated thread and publishes chunks of samples
//! through a bounded channel. Consumers drain the channel in the demod
//! thread.

pub mod flac;
mod playback;
pub mod soundcard;
pub mod stdin_raw;

/// The highest sample rate the modem will ever operate a stream at.
///
/// Every Graywolf modem mode (AFSK 1200, G3RUH 9600, QPSK/8-PSK) is well
/// served by 48 kHz; nothing needs hi-fi rates. We deliberately never
/// advertise, default to, or open a stream above this. Rates above 48 kHz
/// are a trap on USB audio: an ALSA `plughw:`/`default` PCM advertises a
/// synthetic resample range (up to 192 kHz) even though the hardware codec
/// runs at 48 kHz, and a stream opened at the inflated rate desyncs the
/// demodulator's bit timing — every frame fails FCS, RX goes silent.
pub const MODEM_MAX_SAMPLE_RATE: u32 = 48_000;

/// Sample rates we advertise when enumerating device capabilities. Covers
/// the common amateur-radio rates. Capped at [`MODEM_MAX_SAMPLE_RATE`] —
/// see that constant for why we never go above 48 kHz.
pub const STANDARD_SAMPLE_RATES: &[u32] = &[8000, 11025, 16000, 22050, 44100, 48000];

/// Preferred sample rates for quick level scans, in priority order.
/// 48 kHz is native for most USB audio; 44.1 kHz is the CD-standard fallback.
pub const PREFERRED_SCAN_RATES: &[u32] = &[48000, 44100];

use std::sync::mpsc::SyncSender;
use std::thread::JoinHandle;

/// A chunk of audio samples produced by a source. Mono, i16, at the source's
/// native sample rate (reported separately via [`AudioSource::sample_rate`]).
pub type AudioChunk = Vec<i16>;

/// A live audio source running on its own thread.
pub struct AudioSource {
    pub sample_rate: u32,
    pub thread: Option<JoinHandle<()>>,
    pub stop: std::sync::Arc<std::sync::atomic::AtomicBool>,
}

impl AudioSource {
    pub fn stop(&self) {
        self.stop
            .store(true, std::sync::atomic::Ordering::Relaxed);
    }

    /// Signal the source thread to stop and block until it exits. This
    /// ensures the underlying cpal stream is fully dropped and the ALSA
    /// device is released before returning — without this, a subsequent
    /// device enumeration can fail because the hardware is still held.
    pub fn stop_and_join(&mut self) {
        self.stop
            .store(true, std::sync::atomic::Ordering::Relaxed);
        if let Some(handle) = self.thread.take() {
            let _ = handle.join();
        }
    }
}

/// Shared channel buffer capacity. Large enough to tolerate ~1 second of
/// scheduling jitter at 48 kHz before back-pressure kicks in.
pub const CHUNK_QUEUE_DEPTH: usize = 64;

pub type SampleSink = SyncSender<AudioChunk>;

#[cfg(test)]
mod rate_invariants {
    use super::*;

    #[test]
    fn standard_rates_never_exceed_modem_ceiling() {
        for &r in STANDARD_SAMPLE_RATES {
            assert!(
                r <= MODEM_MAX_SAMPLE_RATE,
                "advertised rate {r} exceeds modem ceiling {MODEM_MAX_SAMPLE_RATE}; \
                 rates above 48 kHz desync the demod on USB plughw devices"
            );
        }
    }

    #[test]
    fn scan_rates_never_exceed_modem_ceiling() {
        for &r in PREFERRED_SCAN_RATES {
            assert!(r <= MODEM_MAX_SAMPLE_RATE, "scan rate {r} exceeds ceiling");
        }
    }
}
