//! Live soundcard input and output via `cpal`.
//!
//! cpal manages its own audio thread. Each stream callback converts samples
//! to/from mono i16 and forwards them over an `mpsc` channel. The owning
//! `AudioSource` (input) or `AudioSink` (output) keeps the stream alive;
//! dropping it releases the device.

use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::mpsc::{channel, Receiver, Sender};
use std::sync::Arc;
use std::thread;
use std::thread::JoinHandle;

use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use cpal::{Device, SampleFormat, StreamConfig};

use super::AudioSource;

/// Check that (`sample_rate`, `channels`) falls within at least one of the
/// given stream config ranges. Returns `Ok(())` on match, or an error
/// listing what the device actually supports.
///
/// On Linux/ALSA the default PCM device does software resampling so this
/// rarely rejects; on Windows/WASAPI the config must be natively supported.
pub(crate) fn validate_stream_config(
    configs: impl Iterator<Item = cpal::SupportedStreamConfigRange>,
    sample_rate: u32,
    channels: u16,
    direction: &str,
) -> Result<(), String> {
    let mut rates: Vec<u32> = Vec::new();
    let mut ch_counts: Vec<u16> = Vec::new();
    for c in configs {
        let min = c.min_sample_rate();
        let max = c.max_sample_rate();
        if sample_rate >= min && sample_rate <= max && c.channels() == channels {
            return Ok(());
        }
        for &r in super::STANDARD_SAMPLE_RATES {
            if r >= min && r <= max && !rates.contains(&r) {
                rates.push(r);
            }
        }
        let ch = c.channels();
        if !ch_counts.contains(&ch) {
            ch_counts.push(ch);
        }
    }
    rates.sort_unstable();
    ch_counts.sort_unstable();
    Err(format!(
        "device does not support {} Hz / {}ch {} (supported rates: {:?}, channels: {:?})",
        sample_rate, channels, direction, rates, ch_counts
    ))
}

/// Try the requested channel count first; if the device doesn't support it,
/// pick the smallest supported channel count that works at the given sample
/// rate. This lets callers always request mono while gracefully handling
/// stereo-only devices (common with USB sound cards like SignaLink).
fn negotiate_channels<F, I>(
    device: &Device,
    sample_rate: u32,
    preferred: u16,
    direction: &str,
    get_configs: F,
) -> Result<u16, String>
where
    F: Fn(&Device) -> Result<I, cpal::SupportedStreamConfigsError>,
    I: Iterator<Item = cpal::SupportedStreamConfigRange>,
{
    let configs = get_configs(device)
        .map_err(|e| format!("query supported {} configs: {}", direction, e))?;
    if validate_stream_config(configs, sample_rate, preferred, direction).is_ok() {
        return Ok(preferred);
    }

    // Preferred channel count not supported — find the smallest that works.
    let configs = get_configs(device)
        .map_err(|e| format!("query supported {} configs: {}", direction, e))?;
    let mut fallbacks: Vec<u16> = Vec::new();
    for c in configs {
        let ch = c.channels();
        let min = c.min_sample_rate();
        let max = c.max_sample_rate();
        if sample_rate >= min && sample_rate <= max && !fallbacks.contains(&ch) {
            fallbacks.push(ch);
        }
    }
    fallbacks.sort_unstable();

    if let Some(&ch) = fallbacks.first() {
        eprintln!(
            "audio {}: device does not support {}ch, using {}ch (will extract single channel)",
            direction, preferred, ch
        );
        Ok(ch)
    } else {
        Err(format!(
            "device does not support {} Hz {} in any channel configuration",
            sample_rate, direction
        ))
    }
}

pub struct SoundcardConfig {
    pub device_name: String, // "" or "default" selects the default device
    pub sample_rate: u32,
    pub channels: u32,
    pub audio_channel: u32, // 0-indexed channel to extract
}

pub fn spawn(
    cfg: SoundcardConfig,
    sink: std::sync::mpsc::SyncSender<super::AudioChunk>,
) -> Result<AudioSource, String> {
    let host = cpal::default_host();

    let device = if cfg.device_name.is_empty() || cfg.device_name == "default" {
        host.default_input_device()
            .ok_or_else(|| "no default input device".to_string())?
    } else {
        find_device_by_id(
            host.input_devices().map_err(|e| format!("enumerate input devices: {}", e))?,
            &cfg.device_name,
        ).ok_or_else(|| format!("input device not found: {}", cfg.device_name))?
    };

    let supported = device
        .default_input_config()
        .map_err(|e| format!("device default config: {}", e))?;

    let channels = negotiate_channels(
        &device, cfg.sample_rate, cfg.channels.max(1) as u16, "input",
        |d| d.supported_input_configs(),
    )?;

    let stream_config = StreamConfig {
        channels,
        sample_rate: cfg.sample_rate,
        buffer_size: cpal::BufferSize::Default,
    };

    let want_ch = cfg.audio_channel as usize;
    let stop = Arc::new(AtomicBool::new(false));
    let stop_for_err = stop.clone();

    // The cpal stream is not Send on all platforms, so it must live on its
    // own thread that also runs a small park loop to keep it alive.
    let stop_for_thread = stop.clone();
    let sample_format = supported.sample_format();
    let (ready_tx, ready_rx) = channel::<Result<(), String>>();

    let join = thread::Builder::new()
        .name("audio-soundcard".into())
        .spawn(move || {
            let err_fn = move |e| {
                eprintln!("cpal stream error: {}", e);
                stop_for_err.store(true, Ordering::Relaxed);
            };

            let build_result: Result<cpal::Stream, cpal::BuildStreamError> = match sample_format {
                SampleFormat::F32 => {
                    let sink = sink.clone();
                    device.build_input_stream(
                        &stream_config,
                        move |data: &[f32], _| {
                            let chunk = extract_channel_f32(data, channels as usize, want_ch);
                            let _ = sink.try_send(chunk);
                        },
                        err_fn,
                        None,
                    )
                }
                SampleFormat::I16 => {
                    let sink = sink.clone();
                    device.build_input_stream(
                        &stream_config,
                        move |data: &[i16], _| {
                            let chunk = extract_channel_i16(data, channels as usize, want_ch);
                            let _ = sink.try_send(chunk);
                        },
                        err_fn,
                        None,
                    )
                }
                SampleFormat::U16 => {
                    let sink = sink.clone();
                    device.build_input_stream(
                        &stream_config,
                        move |data: &[u16], _| {
                            let chunk = extract_channel_u16(data, channels as usize, want_ch);
                            let _ = sink.try_send(chunk);
                        },
                        err_fn,
                        None,
                    )
                }
                other => {
                    let _ = ready_tx.send(Err(format!(
                        "unsupported input sample format: {:?}", other
                    )));
                    return;
                }
            };

            let stream = match build_result {
                Ok(s) => s,
                Err(e) => {
                    let _ = ready_tx.send(Err(format!("build_input_stream: {}", e)));
                    return;
                }
            };
            if let Err(e) = stream.play() {
                let _ = ready_tx.send(Err(format!("input stream play: {}", e)));
                return;
            }
            let _ = ready_tx.send(Ok(()));

            while !stop_for_thread.load(Ordering::Relaxed) {
                thread::park_timeout(std::time::Duration::from_millis(100));
            }
            drop(stream);
        })
        .map_err(|e| format!("spawn soundcard thread: {}", e))?;

    ready_rx.recv()
        .map_err(|_| "audio input thread exited unexpectedly".to_string())?
        .map_err(|e| format!("cpal {}", e))?;

    Ok(AudioSource {
        sample_rate: cfg.sample_rate,
        thread: Some(join),
        stop,
    })
}

fn extract_channel_f32(data: &[f32], channels: usize, want: usize) -> Vec<i16> {
    let mut out = Vec::with_capacity(data.len() / channels.max(1));
    if channels <= 1 {
        for &s in data {
            out.push((s.clamp(-1.0, 1.0) * 32767.0) as i16);
        }
    } else {
        for frame in data.chunks(channels) {
            let s = *frame.get(want).unwrap_or(&0.0);
            out.push((s.clamp(-1.0, 1.0) * 32767.0) as i16);
        }
    }
    out
}

fn extract_channel_i16(data: &[i16], channels: usize, want: usize) -> Vec<i16> {
    if channels <= 1 {
        return data.to_vec();
    }
    let mut out = Vec::with_capacity(data.len() / channels);
    for frame in data.chunks(channels) {
        out.push(*frame.get(want).unwrap_or(&0));
    }
    out
}

fn extract_channel_u16(data: &[u16], channels: usize, want: usize) -> Vec<i16> {
    let convert = |s: u16| -> i16 { (s as i32 - 32768) as i16 };
    if channels <= 1 {
        return data.iter().copied().map(convert).collect();
    }
    let mut out = Vec::with_capacity(data.len() / channels);
    for frame in data.chunks(channels) {
        out.push(convert(*frame.get(want).unwrap_or(&0)));
    }
    out
}

/// Parameters for opening an output soundcard stream. Mirrors
/// [`SoundcardConfig`] on the input side.
pub struct SoundcardOutputConfig {
    /// "" or "default" selects the default device.
    pub device_name: String,
    pub sample_rate: u32,
    pub channels: u32,
    /// 0-indexed output channel to write samples into on a multi-channel
    /// device. The other channels are filled with silence.
    pub audio_channel: u32,
}

/// Resolve a cpal output [`Device`] by its pcm_id (e.g. `plughw:CARD=Foo,DEV=0`).
pub fn resolve_output_device(pcm_id: &str) -> Result<Device, String> {
    let host = cpal::default_host();
    if pcm_id.is_empty() || pcm_id == "default" {
        host.default_output_device()
            .ok_or_else(|| "no default output device".to_string())
    } else {
        find_device_by_id(
            host.output_devices().map_err(|e| format!("enumerate output devices: {}", e))?,
            pcm_id,
        ).ok_or_else(|| format!("output device not found: {}", pcm_id))
    }
}

/// Find a cpal device whose pcm_id (the driver-level identifier returned by
/// `DeviceTrait::name()`) matches `id`. This is the unique ALSA device
/// string like `hw:CARD=AllInOneCable,DEV=0` — not the human-friendly
/// description.
#[allow(deprecated)] // DeviceTrait::name() gives the raw pcm_id we need
pub fn find_device_by_id(devices: impl Iterator<Item = Device>, id: &str) -> Option<Device> {
    for d in devices {
        if let Ok(pcm_id) = d.name() {
            if pcm_id == id {
                return Some(d);
            }
        }
    }
    None
}

/// Owns a live cpal output stream and a queue of pending i16 sample
/// submissions. The stream stays open for the sink's lifetime — direwolf
/// keeps its output device continuously open for the same reason: avoiding
/// the startup latency and audible pops of re-opening cpal on every frame.
/// Between submissions the callback emits silence, which VOX-keyed rigs
/// unkey through naturally.
pub struct AudioSink {
    submit_tx: Sender<Vec<i16>>,
    /// Running total of samples the caller has ever submitted.
    submitted: Arc<AtomicUsize>,
    /// Running total of samples the stream callback has copied into the
    /// DAC output buffer. Monotonically non-decreasing.
    drained: Arc<AtomicUsize>,
    stop: Arc<AtomicBool>,
    join: Option<JoinHandle<()>>,
}

impl AudioSink {
    /// Queue samples for playback. Returns the cumulative sample watermark
    /// the caller should wait for via [`AudioSink::drained_samples`] before
    /// considering this submission fully rendered by the callback.
    pub fn submit(&self, samples: Vec<i16>) -> Result<usize, String> {
        let n = samples.len();
        let total = self.submitted.fetch_add(n, Ordering::Relaxed) + n;
        self.submit_tx
            .send(samples)
            .map_err(|e| format!("audio sink submit: {}", e))?;
        Ok(total)
    }

    /// Cumulative sample count the output callback has drained from the
    /// submission queue. The caller compares this to the watermark returned
    /// by [`AudioSink::submit`] to know when playback of that submission has
    /// left the callback. Note that the hardware may still hold a few
    /// milliseconds in its DAC pipeline after the callback releases samples;
    /// callers that need sample-accurate tail behavior should also wait the
    /// expected audio duration.
    pub fn drained_samples(&self) -> usize {
        self.drained.load(Ordering::Relaxed)
    }
}

impl Drop for AudioSink {
    fn drop(&mut self) {
        self.stop.store(true, Ordering::Relaxed);
        if let Some(j) = self.join.take() {
            j.thread().unpark();
            let _ = j.join();
        }
    }
}

/// Open an output device and spawn the cpal stream on its own thread.
/// The cpal stream is not `Send` on all platforms, so ownership must stay
/// on the thread that built it — same pattern as [`spawn`] for input.
///
/// If `device` is `Some`, it is used directly — no enumeration happens.
/// Pass a handle obtained from [`resolve_output_device`] to skip
/// enumeration at transmit time.
pub fn spawn_output(cfg: SoundcardOutputConfig, device: Option<Device>) -> Result<AudioSink, String> {
    let device = match device {
        Some(d) => d,
        None => resolve_output_device(&cfg.device_name)?,
    };

    let supported = device
        .default_output_config()
        .map_err(|e| format!("device default config: {}", e))?;

    let channels = negotiate_channels(
        &device, cfg.sample_rate, cfg.channels.max(1) as u16, "output",
        |d| d.supported_output_configs(),
    )?;

    if cfg.audio_channel >= channels as u32 {
        return Err(format!(
            "output audio_channel {} out of range for {}-channel device",
            cfg.audio_channel, channels
        ));
    }

    let stream_config = StreamConfig {
        channels,
        sample_rate: cfg.sample_rate,
        buffer_size: cpal::BufferSize::Default,
    };

    let (submit_tx, submit_rx) = channel::<Vec<i16>>();
    let submitted = Arc::new(AtomicUsize::new(0));
    let drained = Arc::new(AtomicUsize::new(0));
    let stop = Arc::new(AtomicBool::new(false));

    let want_ch = cfg.audio_channel as usize;
    let sample_format = supported.sample_format();

    let drained_for_thread = drained.clone();
    let submitted_for_thread = submitted.clone();
    let stop_for_err = stop.clone();
    let stop_for_thread = stop.clone();
    let (ready_tx, ready_rx) = channel::<Result<(), String>>();

    let join = thread::Builder::new()
        .name("audio-soundcard-out".into())
        .spawn(move || {
            let err_fn = move |e| {
                eprintln!("cpal output stream error: {}", e);
                stop_for_err.store(true, Ordering::Relaxed);
            };

            let mut state = OutputState::new(submit_rx, drained_for_thread, submitted_for_thread);
            let ch_usize = channels as usize;

            let build_result: Result<cpal::Stream, cpal::BuildStreamError> = match sample_format {
                SampleFormat::F32 => device.build_output_stream(
                    &stream_config,
                    move |data: &mut [f32], _| {
                        let mut next = || state.next_sample();
                        fill_output_f32(data, ch_usize, want_ch, &mut next);
                    },
                    err_fn,
                    None,
                ),
                SampleFormat::I16 => device.build_output_stream(
                    &stream_config,
                    move |data: &mut [i16], _| {
                        let mut next = || state.next_sample();
                        fill_output_i16(data, ch_usize, want_ch, &mut next);
                    },
                    err_fn,
                    None,
                ),
                SampleFormat::U16 => device.build_output_stream(
                    &stream_config,
                    move |data: &mut [u16], _| {
                        let mut next = || state.next_sample();
                        fill_output_u16(data, ch_usize, want_ch, &mut next);
                    },
                    err_fn,
                    None,
                ),
                other => {
                    let _ = ready_tx.send(Err(format!(
                        "unsupported output sample format: {:?}", other
                    )));
                    return;
                }
            };

            let stream = match build_result {
                Ok(s) => s,
                Err(e) => {
                    let _ = ready_tx.send(Err(format!("build_output_stream: {}", e)));
                    return;
                }
            };
            if let Err(e) = stream.play() {
                let _ = ready_tx.send(Err(format!("output stream play: {}", e)));
                return;
            }
            let _ = ready_tx.send(Ok(()));

            while !stop_for_thread.load(Ordering::Relaxed) {
                thread::park_timeout(std::time::Duration::from_millis(100));
            }
            drop(stream);
        })
        .map_err(|e| format!("spawn soundcard output thread: {}", e))?;

    ready_rx.recv()
        .map_err(|_| "audio output thread exited unexpectedly".to_string())?
        .map_err(|e| format!("cpal {}", e))?;

    Ok(AudioSink {
        submit_tx,
        submitted,
        drained,
        stop,
        join: Some(join),
    })
}

/// Per-callback cursor over the submission queue. Not `Send` across
/// callback invocations by necessity — the cpal callback is `FnMut` and
/// keeps this state by move.
struct OutputState {
    rx: Receiver<Vec<i16>>,
    current: Vec<i16>,
    pos: usize,
    drained: Arc<AtomicUsize>,
    submitted: Arc<AtomicUsize>,
}

impl OutputState {
    fn new(
        rx: Receiver<Vec<i16>>,
        drained: Arc<AtomicUsize>,
        submitted: Arc<AtomicUsize>,
    ) -> Self {
        Self {
            rx,
            current: Vec::new(),
            pos: 0,
            drained,
            submitted,
        }
    }

    /// Pull the next mono i16 sample from the queue, or `None` if nothing
    /// is available. Bumps the drained counter on each yielded sample.
    ///
    /// Invariant: the TX worker serializes submit + drain per frame, so
    /// whenever this function returns `None`, `drained` must already have
    /// caught up to `submitted`. A shortfall means a mid-transmission
    /// underrun — a bug in the worker's drain loop, not normal silence.
    /// The `debug_assert` documents that invariant and catches regressions
    /// (including any future change that streams multiple `Vec`s per
    /// frame) in debug builds without spending cycles in release.
    fn next_sample(&mut self) -> Option<i16> {
        loop {
            if self.pos < self.current.len() {
                let s = self.current[self.pos];
                self.pos += 1;
                self.drained.fetch_add(1, Ordering::Relaxed);
                return Some(s);
            }
            match self.rx.try_recv() {
                Ok(next) => {
                    self.current = next;
                    self.pos = 0;
                }
                Err(_) => {
                    debug_assert!(
                        self.submitted.load(Ordering::Relaxed)
                            <= self.drained.load(Ordering::Relaxed),
                        "cpal output: mid-transmission underrun (submitted={}, drained={})",
                        self.submitted.load(Ordering::Relaxed),
                        self.drained.load(Ordering::Relaxed),
                    );
                    return None;
                }
            }
        }
    }
}

/// Fill an `f32` output buffer with samples from `next`, routing each
/// mono sample into channel `want` of a `channels`-wide frame. The
/// remaining channels are zeroed. Silence (`0`) is written when `next`
/// returns `None`.
fn fill_output_f32(
    data: &mut [f32],
    channels: usize,
    want: usize,
    next: &mut dyn FnMut() -> Option<i16>,
) {
    let ch = channels.max(1);
    for frame in data.chunks_mut(ch) {
        let sample = next().unwrap_or(0);
        let f = (sample as f32) / 32768.0;
        if ch <= 1 {
            for slot in frame.iter_mut() {
                *slot = f;
            }
        } else {
            for (i, slot) in frame.iter_mut().enumerate() {
                *slot = if i == want { f } else { 0.0 };
            }
        }
    }
}

/// i16 counterpart of [`fill_output_f32`].
fn fill_output_i16(
    data: &mut [i16],
    channels: usize,
    want: usize,
    next: &mut dyn FnMut() -> Option<i16>,
) {
    let ch = channels.max(1);
    for frame in data.chunks_mut(ch) {
        let sample = next().unwrap_or(0);
        if ch <= 1 {
            for slot in frame.iter_mut() {
                *slot = sample;
            }
        } else {
            for (i, slot) in frame.iter_mut().enumerate() {
                *slot = if i == want { sample } else { 0 };
            }
        }
    }
}

/// u16 counterpart of [`fill_output_f32`]. Silence is encoded as
/// mid-scale (`0x8000`) since u16 PCM is offset-binary.
fn fill_output_u16(
    data: &mut [u16],
    channels: usize,
    want: usize,
    next: &mut dyn FnMut() -> Option<i16>,
) {
    let ch = channels.max(1);
    let to_u16 = |s: i16| -> u16 { (s as i32 + 32768) as u16 };
    let silence: u16 = 0x8000;
    for frame in data.chunks_mut(ch) {
        let sample = next().unwrap_or(0);
        let out = to_u16(sample);
        if ch <= 1 {
            for slot in frame.iter_mut() {
                *slot = out;
            }
        } else {
            for (i, slot) in frame.iter_mut().enumerate() {
                *slot = if i == want { out } else { silence };
            }
        }
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Helper: turn a slice of mono samples into the `next()` closure the
    /// `fill_output_*` functions expect.
    fn source(samples: &[i16]) -> impl FnMut() -> Option<i16> + '_ {
        let mut iter = samples.iter().copied();
        move || iter.next()
    }

    #[test]
    fn fill_output_f32_writes_mono_sample_into_every_slot() {
        let mut out = [0.0f32; 3];
        let mut next = source(&[16_384, -16_384, 0]);
        fill_output_f32(&mut out, 1, 0, &mut next);
        assert_eq!(out, [0.5, -0.5, 0.0]);
    }

    #[test]
    fn fill_output_f32_routes_stereo_to_requested_channel() {
        let mut out = [0.0f32; 6];
        let mut next = source(&[16_384, -16_384, 8_192]);
        fill_output_f32(&mut out, 2, 1, &mut next);
        assert_eq!(out, [0.0, 0.5, 0.0, -0.5, 0.0, 8_192.0 / 32_768.0]);
    }

    #[test]
    fn fill_output_f32_emits_silence_when_source_exhausted() {
        let mut out = [1.0f32; 4];
        let mut next = source(&[100]);
        fill_output_f32(&mut out, 1, 0, &mut next);
        assert_eq!(out, [100.0 / 32_768.0, 0.0, 0.0, 0.0]);
    }

    #[test]
    fn fill_output_i16_mono_is_identity() {
        let mut out = [0i16; 3];
        let mut next = source(&[100, -200, 32_000]);
        fill_output_i16(&mut out, 1, 0, &mut next);
        assert_eq!(out, [100, -200, 32_000]);
    }

    #[test]
    fn fill_output_i16_stereo_routes_to_left_and_silences_right() {
        let mut out = [0i16; 4];
        let mut next = source(&[100, -200]);
        fill_output_i16(&mut out, 2, 0, &mut next);
        assert_eq!(out, [100, 0, -200, 0]);
    }

    #[test]
    fn fill_output_u16_mono_uses_offset_binary() {
        let mut out = [0u16; 3];
        let mut next = source(&[0, 32_767, -32_768]);
        fill_output_u16(&mut out, 1, 0, &mut next);
        assert_eq!(out, [0x8000, 0xFFFF, 0x0000]);
    }

    #[test]
    fn fill_output_u16_silence_is_mid_scale_on_unused_channels() {
        let mut out = [0u16; 4];
        let mut next = source(&[16_384, -16_384]);
        fill_output_u16(&mut out, 2, 1, &mut next);
        assert_eq!(out, [0x8000, 0xC000, 0x8000, 0x4000]);
    }

    #[test]
    fn output_state_yields_samples_across_submissions_and_bumps_drained() {
        let (tx, rx) = channel::<Vec<i16>>();
        let drained = Arc::new(AtomicUsize::new(0));
        let submitted = Arc::new(AtomicUsize::new(0));
        let mut state = OutputState::new(rx, drained.clone(), submitted.clone());

        // Match the AudioSink::submit invariant: bump `submitted` before
        // sending so the `next_sample` debug_assert sees a consistent
        // ledger when it eventually starves.
        submitted.fetch_add(2, Ordering::Relaxed);
        tx.send(vec![1, 2]).unwrap();
        submitted.fetch_add(1, Ordering::Relaxed);
        tx.send(vec![3]).unwrap();

        assert_eq!(state.next_sample(), Some(1));
        assert_eq!(state.next_sample(), Some(2));
        assert_eq!(state.next_sample(), Some(3));
        assert_eq!(state.next_sample(), None);
        assert_eq!(drained.load(Ordering::Relaxed), 3);
    }

    #[test]
    fn spawn_output_rejects_audio_channel_out_of_range() {
        // Use a `match` instead of `.unwrap_err()` so the test doesn't
        // require `AudioSink: Debug`.
        let result = spawn_output(SoundcardOutputConfig {
            device_name: String::new(),
            sample_rate: 48_000,
            channels: 2,
            audio_channel: 2,
        }, None);
        match result {
            Err(e) => assert!(e.contains("out of range"), "unexpected error: {}", e),
            Ok(_) => panic!("expected out-of-range rejection"),
        }
    }
}
