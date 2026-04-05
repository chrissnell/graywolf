//! Live soundcard input via `cpal`.
//!
//! cpal manages its own audio thread. The stream callback converts samples to
//! mono i16 and forwards them to the demod channel. The owning `AudioSource`
//! keeps the stream alive; dropping it (via `stop()` + drop) releases the
//! device.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;

use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use cpal::{SampleFormat, StreamConfig};

use super::AudioSource;

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
        host.input_devices()
            .map_err(|e| format!("enumerate devices: {}", e))?
            .find(|d| d.name().map(|n| n == cfg.device_name).unwrap_or(false))
            .ok_or_else(|| format!("input device not found: {}", cfg.device_name))?
    };

    let supported = device
        .default_input_config()
        .map_err(|e| format!("device default config: {}", e))?;

    let channels = cfg.channels.max(1) as u16;
    let stream_config = StreamConfig {
        channels,
        sample_rate: cpal::SampleRate(cfg.sample_rate),
        buffer_size: cpal::BufferSize::Default,
    };

    let want_ch = cfg.audio_channel as usize;
    let stop = Arc::new(AtomicBool::new(false));
    let stop_for_err = stop.clone();

    // The cpal stream is not Send on all platforms, so it must live on its
    // own thread that also runs a small park loop to keep it alive.
    let stop_for_thread = stop.clone();
    let sample_format = supported.sample_format();

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
                    eprintln!("unsupported cpal sample format: {:?}", other);
                    return;
                }
            };

            let stream = match build_result {
                Ok(s) => s,
                Err(e) => {
                    eprintln!("cpal build_input_stream failed: {}", e);
                    return;
                }
            };
            if let Err(e) = stream.play() {
                eprintln!("cpal stream play failed: {}", e);
                return;
            }

            while !stop_for_thread.load(Ordering::Relaxed) {
                thread::park_timeout(std::time::Duration::from_millis(100));
            }
            drop(stream);
        })
        .map_err(|e| format!("spawn soundcard thread: {}", e))?;

    Ok(AudioSource {
        sample_rate: cfg.sample_rate,
        _join: Some(join),
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
