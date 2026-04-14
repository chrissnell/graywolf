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
pub mod soundcard;
pub mod stdin_raw;

use std::sync::mpsc::SyncSender;
use std::thread::JoinHandle;

/// A chunk of audio samples produced by a source. Mono, i16, at the source's
/// native sample rate (reported separately via [`AudioSource::sample_rate`]).
pub type AudioChunk = Vec<i16>;

/// A live audio source running on its own thread.
pub struct AudioSource {
    pub sample_rate: u32,
    pub _join: Option<JoinHandle<()>>,
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
        if let Some(handle) = self._join.take() {
            let _ = handle.join();
        }
    }
}

/// Shared channel buffer capacity. Large enough to tolerate ~1 second of
/// scheduling jitter at 48 kHz before back-pressure kicks in.
pub const CHUNK_QUEUE_DEPTH: usize = 64;

pub type SampleSink = SyncSender<AudioChunk>;
