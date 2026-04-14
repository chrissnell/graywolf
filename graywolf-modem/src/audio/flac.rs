//! Realtime FLAC playback source.
//!
//! Reads all samples from a FLAC file at load time, then paces them to the
//! sink at the file's native rate using wall-clock sleeps between small
//! chunks. This lets us feed known-good test vectors through the modem as if
//! they were live audio, keeping the DSP path identical to the soundcard case.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::sync_channel;
use std::sync::Arc;
use std::thread;
use std::time::{Duration, Instant};

use super::{AudioSource, CHUNK_QUEUE_DEPTH};

/// Samples per paced chunk. ~20 ms at 48 kHz; small enough for tight DCD
/// response, large enough to keep scheduling overhead negligible.
const CHUNK_SAMPLES: usize = 960;

pub struct FlacConfig {
    pub path: String,
    /// If >0, override the file's native rate (rare; used for resample tests).
    pub rate_override: u32,
    /// For stereo/multi-channel files, which channel to feed to the modem.
    pub audio_channel: u32,
}

pub fn spawn(
    cfg: FlacConfig,
    sink: std::sync::mpsc::SyncSender<super::AudioChunk>,
) -> Result<AudioSource, String> {
    let mut reader = claxon::FlacReader::open(&cfg.path)
        .map_err(|e| format!("open flac {}: {}", cfg.path, e))?;
    let info = reader.streaminfo();
    let file_rate = info.sample_rate;
    let bits = info.bits_per_sample;
    let channels = info.channels as usize;
    let sample_rate = if cfg.rate_override > 0 { cfg.rate_override } else { file_rate };
    let want_channel = cfg.audio_channel as usize;

    // Decode fully — test tracks are <100 MB; keeps the realtime loop simple.
    let mut all: Vec<i16> = Vec::with_capacity(info.samples.unwrap_or(0) as usize);
    for (idx, s) in reader.samples().enumerate() {
        let s = s.map_err(|e| format!("flac decode: {}", e))?;
        let sample = if bits > 16 {
            (s >> (bits - 16)) as i16
        } else if bits < 16 {
            (s << (16 - bits)) as i16
        } else {
            s as i16
        };
        if channels == 1 || (idx % channels) == want_channel {
            all.push(sample);
        }
    }

    let stop = Arc::new(AtomicBool::new(false));
    let stop_clone = stop.clone();

    let join = thread::Builder::new()
        .name("audio-flac".into())
        .spawn(move || pace_loop(all, sample_rate, sink, stop_clone))
        .map_err(|e| format!("spawn flac thread: {}", e))?;

    Ok(AudioSource {
        sample_rate,
        _join: Some(join),
        stop,
    })
}

fn pace_loop(
    samples: Vec<i16>,
    sample_rate: u32,
    sink: std::sync::mpsc::SyncSender<super::AudioChunk>,
    stop: Arc<AtomicBool>,
) {
    let start = Instant::now();
    let total = samples.len();
    let mut sent = 0usize;
    let ns_per_sample = 1_000_000_000u64 / sample_rate as u64;

    while sent < total && !stop.load(Ordering::Relaxed) {
        let end = (sent + CHUNK_SAMPLES).min(total);
        let chunk: Vec<i16> = samples[sent..end].to_vec();
        if sink.send(chunk).is_err() {
            return; // consumer dropped
        }
        sent = end;

        // Pace to realtime. `target_elapsed_ns` is when sample `sent` should
        // have been emitted relative to `start`.
        let target_ns = sent as u64 * ns_per_sample;
        let elapsed_ns = start.elapsed().as_nanos() as u64;
        if target_ns > elapsed_ns {
            thread::sleep(Duration::from_nanos(target_ns - elapsed_ns));
        }
    }
}

/// Synchronous helper used by tests: decode a FLAC file and drive the sink
/// at full speed (no realtime pacing).
pub fn spawn_fast(
    path: &str,
    audio_channel: u32,
    sink: std::sync::mpsc::SyncSender<super::AudioChunk>,
) -> Result<AudioSource, String> {
    let mut reader = claxon::FlacReader::open(path)
        .map_err(|e| format!("open flac {}: {}", path, e))?;
    let info = reader.streaminfo();
    let bits = info.bits_per_sample;
    let channels = info.channels as usize;
    let sample_rate = info.sample_rate;
    let want_channel = audio_channel as usize;

    let mut all: Vec<i16> = Vec::with_capacity(info.samples.unwrap_or(0) as usize);
    for (idx, s) in reader.samples().enumerate() {
        let s = s.map_err(|e| format!("flac decode: {}", e))?;
        let sample = if bits > 16 {
            (s >> (bits - 16)) as i16
        } else if bits < 16 {
            (s << (16 - bits)) as i16
        } else {
            s as i16
        };
        if channels == 1 || (idx % channels) == want_channel {
            all.push(sample);
        }
    }

    let stop = Arc::new(AtomicBool::new(false));
    let stop_clone = stop.clone();
    let join = thread::Builder::new()
        .name("audio-flac-fast".into())
        .spawn(move || {
            for chunk in all.chunks(CHUNK_SAMPLES) {
                if stop_clone.load(Ordering::Relaxed) {
                    return;
                }
                if sink.send(chunk.to_vec()).is_err() {
                    return;
                }
            }
        })
        .map_err(|e| format!("spawn flac-fast thread: {}", e))?;

    Ok(AudioSource {
        sample_rate,
        _join: Some(join),
        stop,
    })
}

// Unused-import suppression for sync_channel (kept for caller convenience).
#[allow(dead_code)]
fn _unused_sync_channel_helper() {
    let _: (std::sync::mpsc::SyncSender<()>, std::sync::mpsc::Receiver<()>) =
        sync_channel(CHUNK_QUEUE_DEPTH);
}
