//! Live soundcard input and output via `cpal`.
//!
//! cpal manages its own audio thread. Each stream callback converts samples
//! to/from mono i16 and forwards them over an `mpsc` channel. The owning
//! `AudioSource` (input) or `AudioSink` (output) keeps the stream alive;
//! dropping it releases the device.

use std::collections::HashMap;
use std::sync::atomic::{AtomicBool, AtomicUsize, Ordering};
use std::sync::mpsc::{channel, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::thread;
use std::thread::JoinHandle;
use std::time::{Duration, Instant};

use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use cpal::{Device, SampleFormat, StreamConfig};

use super::AudioSource;

/// Backoff schedule for stream rebuild attempts after a cpal error.
/// The holding thread walks this slice; once it exhausts the list it
/// stays at the final value until a rebuild succeeds and the stream
/// runs uninterrupted for [`BACKOFF_RESET_AFTER`], at which point the
/// index resets to zero.
const REBUILD_BACKOFF: &[Duration] = &[
    Duration::from_millis(100),
    Duration::from_millis(250),
    Duration::from_millis(500),
    Duration::from_secs(1),
    Duration::from_secs(2),
    Duration::from_secs(5),
];

/// After a rebuild succeeds and the stream runs this long without
/// another error, clear the backoff counter. Shorter than one radio
/// PTT cycle so a single transient ALSA blip doesn't stick us on long
/// backoffs for the rest of the session.
const BACKOFF_RESET_AFTER: Duration = Duration::from_secs(60);

/// Sleep the current thread for the backoff duration at `*idx`, then
/// advance `*idx` up to the last slot. Wakes early if `stop` is set so
/// shutdown isn't delayed by a pending retry wait. Walks the schedule
/// in short ticks for the same reason.
fn backoff_wait(idx: &mut usize, stop: &Arc<AtomicBool>) {
    let d = REBUILD_BACKOFF[*idx];
    let tick = Duration::from_millis(100);
    let mut remaining = d;
    while !remaining.is_zero() {
        if stop.load(Ordering::Relaxed) {
            return;
        }
        let step = if remaining < tick { remaining } else { tick };
        thread::sleep(step);
        remaining = remaining.saturating_sub(step);
    }
    if *idx + 1 < REBUILD_BACKOFF.len() {
        *idx += 1;
    }
}

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
pub(crate) fn negotiate_channels<F, I>(
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

    // Safety net for corrupt persisted config: clamp the requested rate
    // to one the device actually supports and the modem can decode (never
    // above MODEM_MAX_SAMPLE_RATE). A stale `sample_rate=96000` from a
    // plughw device that really runs 48 kHz is rejected here instead of
    // silently desyncing the demod's bit timing.
    let supported_ranges: Vec<(u32, u32)> = device
        .supported_input_configs()
        .map(|it| it.map(|c| (c.min_sample_rate(), c.max_sample_rate())).collect())
        .unwrap_or_default();
    let stream_rate = choose_stream_rate(
        cfg.sample_rate,
        supported.sample_rate(),
        &supported_ranges,
    );
    if stream_rate != cfg.sample_rate {
        eprintln!(
            "graywolf-modem: input rate {} Hz unsupported/corrupt; opening at {} Hz instead",
            cfg.sample_rate, stream_rate
        );
    }

    let channels = negotiate_channels(
        &device, stream_rate, cfg.channels.max(1) as u16, "input",
        |d| d.supported_input_configs(),
    )?;

    let stream_config = StreamConfig {
        channels,
        sample_rate: stream_rate,
        buffer_size: cpal::BufferSize::Default,
    };

    let want_ch = cfg.audio_channel as usize;
    let stop = Arc::new(AtomicBool::new(false));
    // `stream_failed` is set by the cpal error callback and watched by
    // the holding thread, which rebuilds the stream on transient ALSA
    // errors (POLLERR, underruns, device re-enumeration). Kept separate
    // from `stop` so external shutdown and internal recovery can't be
    // confused.
    let stream_failed = Arc::new(AtomicBool::new(false));

    // The cpal stream is not Send on all platforms, so it must live on its
    // own thread that also runs a small park loop to keep it alive.
    let stop_for_thread = stop.clone();
    let stream_failed_for_thread = stream_failed.clone();
    // Never trust default_input_config()'s format: on an ALSA plughw:/
    // default PCM it is F32, which POLLERR-loops cpal forever on cheap
    // USB radio codecs (AIOC). Pick the format the device actually
    // advertises at our rate, preferring native I16 (what arecord and
    // the detection probe use and what streams reliably). Fall back to
    // the cpal default only if the device advertises nothing usable.
    let input_cfgs: Vec<(u16, SampleFormat, u32, u32)> = device
        .supported_input_configs()
        .map(|it| {
            it.map(|c| {
                (
                    c.channels(),
                    c.sample_format(),
                    c.min_sample_rate(),
                    c.max_sample_rate(),
                )
            })
            .collect()
        })
        .unwrap_or_default();
    // Format and channels are chosen independently (rate-filtered): a
    // converting `plughw:`/`default` PCM accepts any (channels, format)
    // pair via software conversion, which is the only realistic graywolf
    // capture path. A non-converting raw `hw:` device that advertised
    // format and channels only as disjoint configs would reject the
    // combination, but cpal surfaces that as a build error via ready_tx
    // (loud, non-fatal) rather than a silent failure.
    let sample_format = pick_input_sample_format(&input_cfgs, stream_rate)
        .unwrap_or_else(|| supported.sample_format());
    let (ready_tx, ready_rx) = channel::<Result<(), String>>();

    let join = thread::Builder::new()
        .name("audio-soundcard".into())
        .spawn(move || {
            let mut ready_tx = Some(ready_tx);
            let mut backoff_idx: usize = 0;
            let mut last_failure: Option<Instant> = None;

            while !stop_for_thread.load(Ordering::Relaxed) {
                let stream_failed_for_err = stream_failed_for_thread.clone();
                let err_fn = move |e| {
                    eprintln!("cpal input stream error: {}", e);
                    stream_failed_for_err.store(true, Ordering::Relaxed);
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
                        if let Some(tx) = ready_tx.take() {
                            let _ = tx.send(Err(format!(
                                "unsupported input sample format: {:?}", other
                            )));
                        }
                        return;
                    }
                };

                let stream = match build_result {
                    Ok(s) => s,
                    Err(e) => {
                        if let Some(tx) = ready_tx.take() {
                            // First attempt failed — surface to caller so
                            // spawn() can return an error instead of
                            // silently retrying an undiagnosable device.
                            let _ = tx.send(Err(format!("build_input_stream: {}", e)));
                            return;
                        }
                        eprintln!("cpal rebuild input stream failed: {}", e);
                        backoff_wait(&mut backoff_idx, &stop_for_thread);
                        last_failure = Some(Instant::now());
                        continue;
                    }
                };
                if let Err(e) = stream.play() {
                    if let Some(tx) = ready_tx.take() {
                        let _ = tx.send(Err(format!("input stream play: {}", e)));
                        return;
                    }
                    eprintln!("cpal rebuild input stream play failed: {}", e);
                    drop(stream);
                    backoff_wait(&mut backoff_idx, &stop_for_thread);
                    last_failure = Some(Instant::now());
                    continue;
                }
                if let Some(tx) = ready_tx.take() {
                    let _ = tx.send(Ok(()));
                }

                let started = Instant::now();
                stream_failed_for_thread.store(false, Ordering::Relaxed);
                while !stop_for_thread.load(Ordering::Relaxed)
                    && !stream_failed_for_thread.load(Ordering::Relaxed)
                {
                    thread::park_timeout(Duration::from_millis(100));
                }
                drop(stream);

                if stop_for_thread.load(Ordering::Relaxed) {
                    return;
                }

                // Stream hit an error. Decide whether to reset the
                // backoff based on how long we ran since the last
                // failure; a single transient blip shouldn't pin us at
                // max backoff for the session.
                if last_failure.is_none_or(|t| t.elapsed() >= BACKOFF_RESET_AFTER)
                    && started.elapsed() >= BACKOFF_RESET_AFTER
                {
                    backoff_idx = 0;
                }
                eprintln!("cpal input stream failed, rebuilding");
                backoff_wait(&mut backoff_idx, &stop_for_thread);
                last_failure = Some(Instant::now());
            }
        })
        .map_err(|e| format!("spawn soundcard thread: {}", e))?;

    ready_rx.recv()
        .map_err(|_| "audio input thread exited unexpectedly".to_string())?
        .map_err(|e| format!("cpal {}", e))?;

    Ok(AudioSource {
        // Report the rate actually opened, not the requested one, so the
        // demodulator clocks bit timing against real audio.
        sample_rate: stream_rate,
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

/// String-only heuristic for the "Recommended" badge.
///
/// **Scope:** this is the *flare* heuristic only — used by
/// the `--list-audio` JSON emitter (`listing::collect_devices`) and the
/// Linux *output* path in `modem/mod.rs::collect_devices`. The runtime
/// *input/capture* picker no longer uses it: `collect_input_devices_linux`
/// verifies Recommended by actually probing the device
/// (`probe_capture`), because this heuristic recommends the
/// `plughw:CARD=<name>` form even on cheap USB chips where that PCM
/// fails to stream while only the raw `hw:` form works.
///
/// The split is deliberate. `--list-audio` runs as a separate
/// short-lived process with no knowledge of what a running modem holds
/// open, so it cannot probe safely; its `recommended` stays a cheap
/// hint and the live picker is authoritative. Keep this in sync with
/// the doc in `pkg/flareschema/audio.go` and the note in
/// `pkg/flareschema/convergence_test.go`.
///
/// The heuristic is "device targets a stable card name": ALSA's
/// `plughw:CARD=<name>` form, where `<name>` starts with an alphabetic
/// character (kernel-stable card name) rather than a numeric index
/// that can change across reboots when USB devices enumerate in a
/// different order. macOS / Windows pcm_ids never match `plughw:` and
/// thus always return false.
pub fn is_recommended_pcm_id(pcm_id: &str) -> bool {
    if !pcm_id.starts_with("plughw:") {
        return false;
    }
    if let Some(idx) = pcm_id.find("CARD=") {
        let after = &pcm_id[idx + 5..];
        after.starts_with(|c: char| c.is_ascii_alphabetic())
    } else {
        false
    }
}

// ---------------------------------------------------------------------------
// ALSA physical-card canonicalization.
//
// cpal's ALSA backend reports the same physical card under several pcm_ids:
// numeric (`hw:CARD=1`, `plughw:CARD=1`) and symbolic (`hw:CARD=Device`,
// `plughw:CARD=Device`). Showing all of them produces a redundant picker and
// hides which form actually streams. These helpers collapse the aliases to
// one physical card so the runtime picker can probe candidates in order and
// surface a single, verified-working entry. All pure + platform-independent
// (Linux is the only caller, but the logic is unit-tested everywhere).
// ---------------------------------------------------------------------------

/// Parse Linux `/proc/asound/cards` into `(card_index, card_name)` rows.
///
/// The file lists one card per stanza, e.g.
///
/// ```text
///  1 [Device         ]: USB-Audio - USB Audio Device
///                       GeneralPlus USB Audio Device at usb-...
/// ```
///
/// Only the header line (leading integer + `[name]`) is significant; the
/// indented continuation line is ignored.
pub fn parse_proc_asound_cards(contents: &str) -> Vec<(u32, String)> {
    let mut out = Vec::new();
    for line in contents.lines() {
        let t = line.trim_start();
        let digit_end = t.find(|c: char| !c.is_ascii_digit()).unwrap_or(t.len());
        if digit_end == 0 {
            continue; // continuation / non-header line
        }
        let idx: u32 = match t[..digit_end].parse() {
            Ok(n) => n,
            Err(_) => continue,
        };
        let lb = match t.find('[') {
            Some(i) => i,
            None => continue,
        };
        let rb = match t[lb + 1..].find(']') {
            Some(i) => lb + 1 + i,
            None => continue,
        };
        let name = t[lb + 1..rb].trim().to_string();
        if name.is_empty() {
            continue;
        }
        out.push((idx, name));
    }
    out
}

/// Extract the ALSA `CARD=` token from a cpal pcm_id: the slice between
/// `CARD=` and the next `,` (or end). `hw:CARD=Device,DEV=0` -> `Device`,
/// `plughw:CARD=1` -> `1`. `None` for ids with no card token (`default`,
/// non-ALSA platforms).
pub fn alsa_card_token(pcm_id: &str) -> Option<&str> {
    let start = pcm_id.find("CARD=")? + "CARD=".len();
    let rest = &pcm_id[start..];
    let end = rest.find(',').unwrap_or(rest.len());
    let tok = &rest[..end];
    if tok.is_empty() {
        None
    } else {
        Some(tok)
    }
}

/// Map a `CARD=` token (kernel card name *or* numeric index string) to its
/// canonical card index, built from parsed `/proc/asound/cards` rows.
pub fn build_card_resolver(cards: &[(u32, String)]) -> HashMap<String, u32> {
    let mut m = HashMap::new();
    for (idx, name) in cards {
        m.insert(name.clone(), *idx);
        m.insert(idx.to_string(), *idx);
    }
    m
}

/// Probe-order rank within one physical card. Lower = tried first:
/// `plughw:` (ALSA software conversion, tolerant of cheap USB chips)
/// before raw `hw:`; stable alphabetic card name before volatile numeric
/// index. This keeps the historically-recommended `plughw:CARD=<name>`
/// first (AIOC / DigiRig stay unchanged) while still allowing fallthrough
/// to a raw `hw:` form for hardware whose plughw path fails to stream.
fn alsa_candidate_rank(pcm_id: &str) -> (u8, u8) {
    let prefix = if pcm_id.starts_with("plughw:") {
        0
    } else if pcm_id.starts_with("hw:") {
        1
    } else {
        2
    };
    let name_form = match alsa_card_token(pcm_id) {
        Some(t) if t.starts_with(|c: char| c.is_ascii_alphabetic()) => 0,
        Some(_) => 1,
        None => 2,
    };
    (prefix, name_form)
}

/// One physical ALSA card with its candidate PCM ids ordered best-first.
#[derive(Debug, PartialEq, Eq)]
pub struct AlsaCardGroup {
    /// Canonical key: `"card:<idx>"` for resolvable cards, else the raw
    /// pcm_id (so `default` and unknown ids stay as singleton entries).
    pub key: String,
    /// PCM ids for this card, ordered by probe preference.
    pub candidates: Vec<String>,
}

/// Canonical physical-card key for a pcm_id: `"card:<idx>"` when its
/// `CARD=` token resolves, else the pcm_id itself (so `default` and
/// unknown ids stay distinct). The single source of truth for "do two
/// pcm_ids name the same physical card" — used by grouping and by the
/// in-use output reconciliation.
pub fn alsa_canonical_key(pcm_id: &str, resolve: impl Fn(&str) -> Option<u32>) -> String {
    match alsa_card_token(pcm_id).and_then(resolve) {
        Some(idx) => format!("card:{}", idx),
        None => pcm_id.to_string(),
    }
}

/// Collapse cpal-reported ALSA pcm_ids to one entry per physical card.
///
/// `resolve` maps a `CARD=` token to the canonical card index; pcm_ids
/// whose token does not resolve (or have none) become singleton groups
/// keyed by the pcm_id itself. Within a group, candidates are ordered
/// best-first via [`alsa_candidate_rank`]. First-seen card order is
/// preserved so the picker ordering stays stable.
pub fn group_alsa_cards(
    pcm_ids: &[String],
    resolve: impl Fn(&str) -> Option<u32>,
) -> Vec<AlsaCardGroup> {
    let mut order: Vec<String> = Vec::new();
    let mut groups: HashMap<String, Vec<String>> = HashMap::new();
    for id in pcm_ids {
        let key = alsa_canonical_key(id, &resolve);
        if !groups.contains_key(&key) {
            order.push(key.clone());
        }
        groups.entry(key).or_default().push(id.clone());
    }
    order
        .into_iter()
        .map(|key| {
            let mut candidates = groups.remove(&key).unwrap();
            // slice::sort_by_key is stable: equal-rank pcm_ids keep their
            // cpal enumeration order.
            candidates.sort_by_key(|p| alsa_candidate_rank(p));
            AlsaCardGroup { key, candidates }
        })
        .collect()
}

/// Pick the best input stream config for a brief probe / level scan:
/// fewest channels (mono > stereo) and most-native sample format
/// (I16 > F32 > U16), at a preferred scan rate the range supports.
///
/// Shared by the device-detection probe ([`probe_capture`])
/// and the input-level scanner so both negotiate identically.
/// `default_input_config()` is deliberately not used — it can hand back
/// parameters a raw `hw:` ALSA device rejects with EINVAL.
pub fn pick_input_probe_config(dev: &Device) -> Result<(SampleFormat, StreamConfig), String> {
    let configs = dev.supported_input_configs().map_err(|e| format!("{}", e))?;
    let mut best: Option<cpal::SupportedStreamConfigRange> = None;
    for cfg in configs {
        let dominated_by_best = best.as_ref().is_some_and(|b| {
            if cfg.channels() > b.channels() {
                return true;
            }
            if cfg.channels() < b.channels() {
                return false;
            }
            native_format_rank(cfg.sample_format()) >= native_format_rank(b.sample_format())
        });
        if !dominated_by_best {
            best = Some(cfg);
        }
    }
    let range = best.ok_or_else(|| "no supported input configurations".to_string())?;
    let rate = super::PREFERRED_SCAN_RATES
        .iter()
        .copied()
        .find(|&r| r >= range.min_sample_rate() && r <= range.max_sample_rate())
        .unwrap_or(range.max_sample_rate());
    Ok((
        range.sample_format(),
        StreamConfig {
            channels: range.channels(),
            sample_rate: rate,
            buffer_size: cpal::BufferSize::Default,
        },
    ))
}

/// Rank for sample-format preference: lower is better. Native
/// integer `I16` first (every cheap USB radio codec runs it and ALSA
/// `plughw:` streams it without resampling jitter), then `F32`, then
/// the rest. Single source of truth shared by the capture/playback
/// runtime streams and the detection probe.
fn native_format_rank(f: SampleFormat) -> u8 {
    match f {
        SampleFormat::I16 => 0,
        SampleFormat::F32 => 1,
        _ => 2,
    }
}

/// Pick the sample format to actually open a capture stream with, given
/// the device's supported `(channels, format, min_rate, max_rate)`
/// configs and the rate we will open at.
///
/// `default_input_config()` on an ALSA `plughw:`/`default` PCM hands back
/// `F32`; cpal opening that synthetic config on a full-speed USB gadget
/// POLLERR-loops forever and RX dies, even though the same hardware
/// streams `I16` (native) cleanly -- proven on the AIOC. We therefore
/// never trust `default_input_config()` for the format; we pick the
/// best format the device actually advertises *at the chosen rate*,
/// preferring native `I16`, exactly as the detection probe does.
pub fn pick_input_sample_format(
    configs: &[(u16, SampleFormat, u32, u32)],
    rate: u32,
) -> Option<SampleFormat> {
    pick_native_sample_format(configs, rate)
}

/// Shared selection core for both directions: from the device's
/// advertised `(channels, format, min_rate, max_rate)` configs, keep
/// only those whose range covers `rate`, then return the best-ranked
/// format (`native_format_rank`, native `I16` first). Returns `None`
/// when nothing is advertised at `rate`, so callers fall back to the
/// cpal default. Capture and playback share this so their format choice
/// cannot drift (invariant 33).
fn pick_native_sample_format(
    configs: &[(u16, SampleFormat, u32, u32)],
    rate: u32,
) -> Option<SampleFormat> {
    configs
        .iter()
        .filter(|&&(_ch, _f, min, max)| rate >= min && rate <= max)
        .min_by_key(|&&(_ch, f, _min, _max)| native_format_rank(f))
        .map(|&(_ch, f, _min, _max)| f)
}

/// Pick the sample format to actually open a playback stream with, given
/// the device's supported `(channels, format, min_rate, max_rate)`
/// configs and the rate we will open at.
///
/// Same trap as capture, on the TX side (#227). `default_output_config()`
/// on an ALSA `plughw:`/`default` PCM hands back `F32`, and opening an
/// F32 *output* stream on the same cheap USB radio codecs (Signalink,
/// Digirig, AIOC) makes `alsa::poll()` return POLLERR every period -- the
/// holding thread then rebuilds with the identical bad format and floods
/// the log forever while TX audio never reaches the rig. The RX path was
/// fixed to never trust `default_input_config()`; this is the matching
/// fix for TX. We pick the best format the device advertises *at the
/// chosen rate*, preferring native `I16`, falling back to the cpal
/// default only if the device advertises nothing usable.
pub fn pick_output_sample_format(
    configs: &[(u16, SampleFormat, u32, u32)],
    rate: u32,
) -> Option<SampleFormat> {
    pick_native_sample_format(configs, rate)
}

/// Decide the sample rate to actually open a stream at, given the
/// operator-configured `requested` rate, the device's `native` rate
/// (cpal `default_*_config().sample_rate()`), and the device's advertised
/// `supported` (min,max) ranges.
///
/// This is the safety net for corrupt persisted config: an ALSA
/// `plughw:`/`default` PCM advertises a synthetic resample range up to
/// 192 kHz even though the codec runs at 48 kHz, so a stale
/// `sample_rate=96000` in the DB would otherwise open a stream the demod
/// then clocks at the wrong rate (every frame fails FCS, RX dies). We
/// never open above [`super::MODEM_MAX_SAMPLE_RATE`], honor `requested`
/// only when it is sane and actually covered by a supported range, and
/// otherwise fall back to the device's native rate clamped to the ceiling.
pub fn choose_stream_rate(requested: u32, native: u32, supported: &[(u32, u32)]) -> u32 {
    let ceiling = super::MODEM_MAX_SAMPLE_RATE;
    let in_range = |r: u32| supported.iter().any(|&(lo, hi)| r >= lo && r <= hi);

    if requested != 0 && requested <= ceiling && in_range(requested) {
        return requested;
    }
    if native != 0 && native <= ceiling {
        return native;
    }
    ceiling
}

/// Briefly open `dev` for capture and report whether it actually streams.
///
/// Returns `true` only if a stream builds, `play()`s, and delivers at
/// least one callback within `timeout` without the cpal error callback
/// firing — the `alsa::poll() returned POLLERR` path that breaks cheap
/// USB audio chips routed through `plughw:`/named PCMs.
/// The stream is always dropped before returning, releasing the device.
///
/// MUST NOT be called on hardware already held by a live capture stream:
/// opening a second stream on an in-use device can disrupt the running
/// radio and, on cheap chips, fail spuriously. Callers gate this through
/// the in-use snapshot in `enumerate_audio_devices`.
pub fn probe_capture(dev: &Device, timeout: Duration) -> bool {
    let (fmt, cfg) = match pick_input_probe_config(dev) {
        Ok(v) => v,
        Err(_) => return false,
    };
    let failed = Arc::new(AtomicBool::new(false));
    let got_data = Arc::new(AtomicBool::new(false));

    let build: Result<cpal::Stream, cpal::BuildStreamError> = match fmt {
        SampleFormat::F32 => {
            let gd = got_data.clone();
            let ef = failed.clone();
            dev.build_input_stream(
                &cfg,
                move |_d: &[f32], _| gd.store(true, Ordering::Relaxed),
                move |e| {
                    eprintln!("probe_capture stream error: {}", e);
                    ef.store(true, Ordering::Relaxed);
                },
                None,
            )
        }
        SampleFormat::I16 => {
            let gd = got_data.clone();
            let ef = failed.clone();
            dev.build_input_stream(
                &cfg,
                move |_d: &[i16], _| gd.store(true, Ordering::Relaxed),
                move |e| {
                    eprintln!("probe_capture stream error: {}", e);
                    ef.store(true, Ordering::Relaxed);
                },
                None,
            )
        }
        SampleFormat::U16 => {
            let gd = got_data.clone();
            let ef = failed.clone();
            dev.build_input_stream(
                &cfg,
                move |_d: &[u16], _| gd.store(true, Ordering::Relaxed),
                move |e| {
                    eprintln!("probe_capture stream error: {}", e);
                    ef.store(true, Ordering::Relaxed);
                },
                None,
            )
        }
        _ => return false,
    };

    let stream = match build {
        Ok(s) => s,
        Err(_) => return false,
    };
    if stream.play().is_err() {
        return false;
    }

    let deadline = Instant::now() + timeout;
    while Instant::now() < deadline {
        if failed.load(Ordering::Relaxed) {
            return false;
        }
        if got_data.load(Ordering::Relaxed) {
            // Real audio flowed. Give POLLERR a short grace window to
            // surface (the cheap-chip failure can lag the first period),
            // then accept if still clean.
            thread::sleep(Duration::from_millis(40));
            return !failed.load(Ordering::Relaxed);
        }
        thread::sleep(Duration::from_millis(10));
    }
    // Timed out: a usable device delivers a callback quickly and stays
    // error-free; one that never produced data or errored is not usable.
    got_data.load(Ordering::Relaxed) && !failed.load(Ordering::Relaxed)
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

/// Find a cpal device whose stable id matches `id`.
///
/// On Linux/macOS the stable id is the cpal `name()` value (the ALSA
/// hw identifier like `hw:CARD=AllInOneCable,DEV=0`). On Windows it is
/// the IMMDevice endpoint id surfaced via cpal `Device::id()`; cpal's
/// `name()` there returns only the device class label (e.g.
/// `"Speakers"`) which is shared by every endpoint of that class, so
/// matching by `name()` would resolve to the wrong card. Issue #100.
#[allow(deprecated)] // DeviceTrait::name() gives the raw pcm_id we need on non-Windows
pub fn find_device_by_id(devices: impl Iterator<Item = Device>, id: &str) -> Option<Device> {
    for d in devices {
        #[cfg(target_os = "windows")]
        let matches = d.id().ok().map(|did| did.1 == id).unwrap_or(false);
        #[cfg(not(target_os = "windows"))]
        let matches = d.name().ok().map(|n| n == id).unwrap_or(false);
        if matches {
            return Some(d);
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
    let shared_rx = Arc::new(Mutex::new(submit_rx));
    let submitted = Arc::new(AtomicUsize::new(0));
    let drained = Arc::new(AtomicUsize::new(0));
    let stop = Arc::new(AtomicBool::new(false));
    let stream_failed = Arc::new(AtomicBool::new(false));

    let want_ch = cfg.audio_channel as usize;
    // Never trust default_output_config()'s format: on an ALSA plughw:/
    // default PCM it is F32, which POLLERR-loops cpal forever on the
    // cheap USB radio codecs used for TX (Signalink, Digirig, AIOC) --
    // the same failure the RX path hit (#227). Pick the format the
    // device actually advertises at our rate, preferring native I16,
    // and fall back to the cpal default only if it advertises nothing
    // usable.
    let output_cfgs: Vec<(u16, SampleFormat, u32, u32)> = device
        .supported_output_configs()
        .map(|it| {
            it.map(|c| {
                (
                    c.channels(),
                    c.sample_format(),
                    c.min_sample_rate(),
                    c.max_sample_rate(),
                )
            })
            .collect()
        })
        .unwrap_or_default();
    let sample_format = pick_output_sample_format(&output_cfgs, cfg.sample_rate)
        .unwrap_or_else(|| supported.sample_format());

    let drained_for_thread = drained.clone();
    let submitted_for_thread = submitted.clone();
    let shared_rx_for_thread = shared_rx.clone();
    let stop_for_thread = stop.clone();
    let stream_failed_for_thread = stream_failed.clone();
    let (ready_tx, ready_rx) = channel::<Result<(), String>>();

    let join = thread::Builder::new()
        .name("audio-soundcard-out".into())
        .spawn(move || {
            let mut ready_tx = Some(ready_tx);
            let mut backoff_idx: usize = 0;
            let mut last_failure: Option<Instant> = None;
            let ch_usize = channels as usize;

            while !stop_for_thread.load(Ordering::Relaxed) {
                let stream_failed_for_err = stream_failed_for_thread.clone();
                let err_fn = move |e| {
                    eprintln!("cpal output stream error: {}", e);
                    stream_failed_for_err.store(true, Ordering::Relaxed);
                };

                let mut state = OutputState::new(
                    shared_rx_for_thread.clone(),
                    drained_for_thread.clone(),
                    submitted_for_thread.clone(),
                );

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
                        if let Some(tx) = ready_tx.take() {
                            let _ = tx.send(Err(format!(
                                "unsupported output sample format: {:?}", other
                            )));
                        }
                        return;
                    }
                };

                let stream = match build_result {
                    Ok(s) => s,
                    Err(e) => {
                        if let Some(tx) = ready_tx.take() {
                            let _ = tx.send(Err(format!("build_output_stream: {}", e)));
                            return;
                        }
                        eprintln!("cpal rebuild output stream failed: {}", e);
                        backoff_wait(&mut backoff_idx, &stop_for_thread);
                        last_failure = Some(Instant::now());
                        continue;
                    }
                };
                if let Err(e) = stream.play() {
                    if let Some(tx) = ready_tx.take() {
                        let _ = tx.send(Err(format!("output stream play: {}", e)));
                        return;
                    }
                    eprintln!("cpal rebuild output stream play failed: {}", e);
                    drop(stream);
                    backoff_wait(&mut backoff_idx, &stop_for_thread);
                    last_failure = Some(Instant::now());
                    continue;
                }
                if let Some(tx) = ready_tx.take() {
                    let _ = tx.send(Ok(()));
                }

                let started = Instant::now();
                stream_failed_for_thread.store(false, Ordering::Relaxed);
                while !stop_for_thread.load(Ordering::Relaxed)
                    && !stream_failed_for_thread.load(Ordering::Relaxed)
                {
                    thread::park_timeout(Duration::from_millis(100));
                }
                drop(stream);

                if stop_for_thread.load(Ordering::Relaxed) {
                    return;
                }

                // Unblock any TX worker waiting on drained by treating
                // the in-flight transmission as "drained" — its audio
                // was truncated by the stream error anyway, so there's
                // no point holding the worker waiting for samples that
                // will never be played. Drain the queue too so stale
                // chunks don't play on the rebuilt stream.
                if let Ok(rx) = shared_rx_for_thread.lock() {
                    while rx.try_recv().is_ok() {}
                }
                let submitted_now = submitted_for_thread.load(Ordering::Relaxed);
                drained_for_thread.store(submitted_now, Ordering::Relaxed);

                if last_failure.is_none_or(|t| t.elapsed() >= BACKOFF_RESET_AFTER)
                    && started.elapsed() >= BACKOFF_RESET_AFTER
                {
                    backoff_idx = 0;
                }
                eprintln!("cpal output stream failed, rebuilding");
                backoff_wait(&mut backoff_idx, &stop_for_thread);
                last_failure = Some(Instant::now());
            }
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

/// Per-callback cursor over the submission queue. The `Receiver` lives
/// behind an `Arc<Mutex<..>>` so it survives across stream rebuilds:
/// the cpal callback is `FnMut` and takes state by move, so every
/// rebuild constructs a fresh `OutputState` that points at the same
/// persistent queue.
///
/// The mutex is never contended under normal operation — only the
/// cpal audio callback locks it. The holding thread accesses the
/// queue only during error recovery, when no callback is running.
struct OutputState {
    rx: Arc<Mutex<Receiver<Vec<i16>>>>,
    current: Vec<i16>,
    pos: usize,
    drained: Arc<AtomicUsize>,
    submitted: Arc<AtomicUsize>,
}

impl OutputState {
    fn new(
        rx: Arc<Mutex<Receiver<Vec<i16>>>>,
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
            let recv = {
                let rx = match self.rx.lock() {
                    Ok(g) => g,
                    // A poisoned mutex means another holder panicked,
                    // which shouldn't happen here — we never panic
                    // while holding it. Treat as "no samples".
                    Err(_) => return None,
                };
                rx.try_recv()
            };
            match recv {
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

    #[test]
    fn pick_input_format_prefers_i16_over_f32_at_rate() {
        // The real AIOC case: a synthetic plug PCM advertises F32 (and
        // a huge range) alongside the native I16/48k config. Must pick
        // I16 -- F32 here is what POLLERR-loops cpal.
        let cfgs = [
            (2u16, SampleFormat::F32, 4_000u32, 4_294_967_295u32),
            (1u16, SampleFormat::I16, 48_000u32, 48_000u32),
        ];
        assert_eq!(pick_input_sample_format(&cfgs, 48_000), Some(SampleFormat::I16));
    }

    #[test]
    fn pick_input_format_uses_f32_when_no_i16_at_rate() {
        let cfgs = [(1u16, SampleFormat::F32, 8_000u32, 48_000u32)];
        assert_eq!(pick_input_sample_format(&cfgs, 48_000), Some(SampleFormat::F32));
        // F32 ranked above non-I16/F32 formats.
        let cfgs2 = [
            (1u16, SampleFormat::U16, 8_000u32, 48_000u32),
            (1u16, SampleFormat::F32, 8_000u32, 48_000u32),
        ];
        assert_eq!(pick_input_sample_format(&cfgs2, 48_000), Some(SampleFormat::F32));
    }

    #[test]
    fn pick_input_format_respects_rate_bounds() {
        // I16 exists but only outside the target rate; F32 covers it.
        let cfgs = [
            (1u16, SampleFormat::I16, 8_000u32, 16_000u32),
            (1u16, SampleFormat::F32, 8_000u32, 48_000u32),
        ];
        assert_eq!(pick_input_sample_format(&cfgs, 48_000), Some(SampleFormat::F32));
        // Nothing covers the rate.
        assert_eq!(pick_input_sample_format(&cfgs, 96_000), None);
        assert_eq!(pick_input_sample_format(&[], 48_000), None);
    }

    #[test]
    fn pick_output_format_prefers_i16_over_f32_at_rate() {
        // #227: the TX mirror of the AIOC case. A synthetic plug PCM
        // advertises F32 (huge range) alongside native I16/48k. Must
        // pick I16 -- opening the F32 output stream is what POLLERR-loops
        // cpal on Signalink/Digirig/AIOC and floods the TX log.
        let cfgs = [
            (2u16, SampleFormat::F32, 4_000u32, 4_294_967_295u32),
            (1u16, SampleFormat::I16, 48_000u32, 48_000u32),
        ];
        assert_eq!(pick_output_sample_format(&cfgs, 48_000), Some(SampleFormat::I16));
    }

    #[test]
    fn pick_output_format_falls_back_and_respects_rate_bounds() {
        // F32 used only when no I16 covers the rate.
        let cfgs = [(1u16, SampleFormat::F32, 8_000u32, 48_000u32)];
        assert_eq!(pick_output_sample_format(&cfgs, 48_000), Some(SampleFormat::F32));
        // I16 exists but outside the target rate; F32 covers it.
        let cfgs2 = [
            (1u16, SampleFormat::I16, 8_000u32, 16_000u32),
            (1u16, SampleFormat::F32, 8_000u32, 48_000u32),
        ];
        assert_eq!(pick_output_sample_format(&cfgs2, 48_000), Some(SampleFormat::F32));
        // Nothing covers the rate / no configs → caller uses cpal default.
        assert_eq!(pick_output_sample_format(&cfgs2, 96_000), None);
        assert_eq!(pick_output_sample_format(&[], 48_000), None);
    }

    #[test]
    fn choose_stream_rate_honors_sane_supported_request() {
        // 48 kHz, in range, at/under ceiling → use it as-is.
        assert_eq!(choose_stream_rate(48_000, 48_000, &[(8_000, 48_000)]), 48_000);
        assert_eq!(choose_stream_rate(44_100, 48_000, &[(8_000, 48_000)]), 44_100);
    }

    #[test]
    fn choose_stream_rate_rejects_request_above_ceiling() {
        // Corrupt 96 kHz from a plughw device whose synthetic range
        // covers it — must NOT be honored; fall back to native 48 kHz.
        assert_eq!(choose_stream_rate(96_000, 48_000, &[(8_000, 192_000)]), 48_000);
        // 88.2 kHz "supported" by the plug plugin — still rejected.
        assert_eq!(choose_stream_rate(88_200, 44_100, &[(8_000, 192_000)]), 44_100);
    }

    #[test]
    fn choose_stream_rate_clamps_bad_native_and_unset_request() {
        // plughw default itself lies (native=96k) → clamp to ceiling.
        assert_eq!(choose_stream_rate(96_000, 96_000, &[(8_000, 192_000)]), 48_000);
        // requested unset (0) → device native.
        assert_eq!(choose_stream_rate(0, 48_000, &[(8_000, 48_000)]), 48_000);
        // requested not covered by any supported range → native.
        assert_eq!(choose_stream_rate(44_100, 48_000, &[(48_000, 48_000)]), 48_000);
    }

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
        let shared_rx = Arc::new(Mutex::new(rx));
        let mut state = OutputState::new(shared_rx, drained.clone(), submitted.clone());

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
    fn output_state_survives_rebuild_sharing_receiver() {
        // Simulates the full rebuild-after-error path that the holding
        // thread performs: (1) first OutputState drops mid-chunk, its
        // partial `current` buffer is lost; (2) holding thread drains
        // the queue and bumps `drained` up to `submitted` so the TX
        // worker unblocks; (3) a fresh OutputState plays new audio
        // submitted after recovery.
        let (tx, rx) = channel::<Vec<i16>>();
        let drained = Arc::new(AtomicUsize::new(0));
        let submitted = Arc::new(AtomicUsize::new(0));
        let shared_rx = Arc::new(Mutex::new(rx));

        submitted.fetch_add(2, Ordering::Relaxed);
        tx.send(vec![10, 20]).unwrap();
        submitted.fetch_add(2, Ordering::Relaxed);
        tx.send(vec![40, 50]).unwrap();

        let mut first = OutputState::new(
            shared_rx.clone(), drained.clone(), submitted.clone(),
        );
        assert_eq!(first.next_sample(), Some(10));
        drop(first);

        // Holding thread's post-failure cleanup.
        {
            let rx = shared_rx.lock().unwrap();
            while rx.try_recv().is_ok() {}
        }
        drained.store(submitted.load(Ordering::Relaxed), Ordering::Relaxed);

        // New audio submitted after rebuild plays cleanly; old partial
        // chunks and queued chunks were discarded during recovery.
        submitted.fetch_add(1, Ordering::Relaxed);
        tx.send(vec![99]).unwrap();
        let mut second = OutputState::new(
            shared_rx, drained.clone(), submitted.clone(),
        );
        assert_eq!(second.next_sample(), Some(99));
        assert_eq!(second.next_sample(), None);
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

    // --- ALSA physical-card canonicalization --------------------------

    /// Verbatim `/proc/asound/cards` from the field report (Pi 3B,
    /// Trixie, GeneralPlus USB dongle), including the indented
    /// continuation lines that must be ignored.
    const ISSUE_129_PROC_CARDS: &str = "\
 0 [Headphones     ]: bcm2835_headpho - bcm2835 Headphones
                      bcm2835 Headphones
 1 [Device         ]: USB-Audio - USB Audio Device
                      GeneralPlus USB Audio Device at usb-3f980000.usb-1.5, full speed
 2 [vc4hdmi        ]: vc4-hdmi - vc4-hdmi
                      vc4-hdmi
";

    #[test]
    fn parse_proc_asound_cards_ignores_continuation_lines() {
        let got = parse_proc_asound_cards(ISSUE_129_PROC_CARDS);
        assert_eq!(
            got,
            vec![
                (0, "Headphones".to_string()),
                (1, "Device".to_string()),
                (2, "vc4hdmi".to_string()),
            ]
        );
    }

    #[test]
    fn parse_proc_asound_cards_handles_empty_and_garbage() {
        assert!(parse_proc_asound_cards("").is_empty());
        assert!(parse_proc_asound_cards("no leading digit here\n   indented\n").is_empty());
    }

    #[test]
    fn alsa_card_token_extracts_name_and_index_forms() {
        assert_eq!(alsa_card_token("hw:CARD=Device,DEV=0"), Some("Device"));
        assert_eq!(alsa_card_token("plughw:CARD=1,DEV=0"), Some("1"));
        assert_eq!(alsa_card_token("plughw:CARD=AIOC"), Some("AIOC"));
        assert_eq!(alsa_card_token("default"), None);
        assert_eq!(alsa_card_token("hw:CARD="), None);
        assert_eq!(alsa_card_token("sysdefault:CARD=Device,DEV=0"), Some("Device"));
    }

    #[test]
    fn build_card_resolver_maps_name_and_index_to_same_card() {
        let cards = parse_proc_asound_cards(ISSUE_129_PROC_CARDS);
        let r = build_card_resolver(&cards);
        assert_eq!(r.get("Device").copied(), Some(1));
        assert_eq!(r.get("1").copied(), Some(1));
        assert_eq!(r.get("Headphones").copied(), Some(0));
        assert_eq!(r.get("0").copied(), Some(0));
        assert_eq!(r.get("nonexistent").copied(), None);
    }

    #[test]
    fn alsa_canonical_key_unifies_name_and_index_and_passes_through_default() {
        let cards = parse_proc_asound_cards(ISSUE_129_PROC_CARDS);
        let r = build_card_resolver(&cards);
        let key = |p: &str| alsa_canonical_key(p, |t| r.get(t).copied());
        // Numeric and symbolic forms of the same card collapse equal.
        assert_eq!(key("plughw:CARD=Device,DEV=0"), key("hw:CARD=Device,DEV=0"));
        assert_eq!(key("hw:CARD=Device,DEV=0"), key("plughw:CARD=1,DEV=0"));
        assert_eq!(key("hw:CARD=Device,DEV=0"), "card:1");
        assert_eq!(key("plughw:CARD=Headphones,DEV=0"), "card:0");
        // No card token -> passes through unchanged.
        assert_eq!(key("default"), "default");
        // Unknown card (not in /proc/asound/cards) stays distinct by
        // pcm_id rather than collapsing with anything else.
        assert_eq!(key("hw:CARD=Unknown,DEV=0"), "hw:CARD=Unknown,DEV=0");
    }

    #[test]
    fn group_alsa_cards_collapses_aliases_and_orders_candidates() {
        let cards = parse_proc_asound_cards(ISSUE_129_PROC_CARDS);
        let resolver = build_card_resolver(&cards);
        // The mix cpal's ALSA backend reports for this Pi: numeric and
        // symbolic forms of both physical capture cards, plus `default`.
        let pcm_ids: Vec<String> = [
            "hw:CARD=Headphones,DEV=0",
            "plughw:CARD=Headphones,DEV=0",
            "hw:CARD=0,DEV=0",
            "plughw:CARD=0,DEV=0",
            "hw:CARD=Device,DEV=0",
            "plughw:CARD=Device,DEV=0",
            "hw:CARD=1,DEV=0",
            "plughw:CARD=1,DEV=0",
            "default",
        ]
        .iter()
        .map(|s| s.to_string())
        .collect();

        let groups = group_alsa_cards(&pcm_ids, |t| resolver.get(t).copied());

        // Two physical cards + the `default` singleton, first-seen order.
        assert_eq!(groups.len(), 3);
        assert_eq!(groups[0].key, "card:0");
        assert_eq!(groups[1].key, "card:1");
        assert_eq!(groups[2].key, "default");

        // The GeneralPlus USB card: every alias collapsed, ordered
        // plughw-name > plughw-index > hw-name > hw-index. The broken
        // `plughw:CARD=Device` is tried first (AIOC/DigiRig succeed there,
        // unchanged); a probe failure falls through to `hw:CARD=1`, the
        // only form that streams on this hardware.
        assert_eq!(
            groups[1].candidates,
            vec![
                "plughw:CARD=Device,DEV=0".to_string(),
                "plughw:CARD=1,DEV=0".to_string(),
                "hw:CARD=Device,DEV=0".to_string(),
                "hw:CARD=1,DEV=0".to_string(),
            ]
        );
        assert_eq!(groups[2].candidates, vec!["default".to_string()]);
    }
}

/// Read-only enumeration of every cpal host's input + output devices.
///
/// Used by the `--list-audio` CLI subcommand. No streams are constructed
/// here — every call walks the host iterator and reads supported config
/// ranges, nothing more. Safe to call from a one-shot binary path that
/// exits immediately after.
///
/// The output structures mirror the Go-side `flareschema.AudioDevices`
/// shape so the JSON serialization is the contract.
pub mod listing {
    use cpal::traits::{DeviceTrait, HostTrait};
    use serde::Serialize;

    #[derive(Serialize)]
    pub struct AudioDevices {
        pub hosts: Vec<AudioHost>,
        #[serde(skip_serializing_if = "Vec::is_empty")]
        pub issues: Vec<CollectorIssue>,
    }

    #[derive(Serialize)]
    pub struct AudioHost {
        pub id: String,
        pub name: String,
        pub is_default: bool,
        pub devices: Vec<AudioDevice>,
    }

    #[derive(Serialize)]
    pub struct AudioDevice {
        pub name: String,
        pub direction: String,
        pub is_default: bool,
        // Flare-side "Recommended" *hint*. Set from the string-only
        // `is_recommended_pcm_id` heuristic (`plughw:CARD=<name>` with a
        // stable alphabetic card name). This intentionally does NOT
        // match the live web picker for capture devices — the
        // runtime path probes the device and recommends the form that
        // actually streams, which `--list-audio` cannot do safely from
        // a separate process. The live picker is authoritative; this
        // stays a cheap triage hint.
        #[serde(skip_serializing_if = "is_false")]
        pub recommended: bool,
        #[serde(skip_serializing_if = "Vec::is_empty")]
        pub supported_configs: Vec<AudioStreamConfigRange>,
    }

    // Helper for `skip_serializing_if`: keeps `recommended: false` off
    // the wire so older clients that don't know the field see no drift.
    fn is_false(v: &bool) -> bool { !*v }

    #[derive(Serialize)]
    pub struct AudioStreamConfigRange {
        pub channels: u16,
        pub min_sample_rate_hz: u32,
        pub max_sample_rate_hz: u32,
        pub sample_format: String,
    }

    #[derive(Serialize)]
    pub struct CollectorIssue {
        pub kind: String,
        pub message: String,
        #[serde(skip_serializing_if = "Option::is_none")]
        pub path: Option<String>,
    }

    pub fn enumerate() -> AudioDevices {
        let mut hosts_out = Vec::new();
        let mut issues = Vec::new();

        let default_host_id = cpal::default_host().id();

        for host_id in cpal::available_hosts() {
            let host = match cpal::host_from_id(host_id) {
                Ok(h) => h,
                Err(e) => {
                    issues.push(CollectorIssue {
                        kind: "host_init_failed".into(),
                        message: format!("{}: {}", host_id.name(), e),
                        path: None,
                    });
                    continue;
                }
            };

            // cpal deprecated `.name()` in favor of `.description()`,
            // which returns a structured DeviceDescription. We carry
            // its `.name()` field (an owned String) on the wire — the
            // JSON field stays `name` for the schema contract.
            //
            // Windows exception: `description().name()` returns only the
            // device class label (e.g. `"Speakers"`), shared by every
            // soundcard of that class.
            // Source the default-device match key from `Device::id()`
            // (the IMMDevice endpoint id, unique per endpoint) and have
            // `collect_devices` key its `is_default` comparison the
            // same way. Issue #100.
            #[cfg(target_os = "windows")]
            let default_input = host
                .default_input_device()
                .and_then(|d| d.id().ok().map(|id| id.1));
            #[cfg(target_os = "windows")]
            let default_output = host
                .default_output_device()
                .and_then(|d| d.id().ok().map(|id| id.1));
            #[cfg(not(target_os = "windows"))]
            let default_input = host
                .default_input_device()
                .and_then(|d| d.description().ok())
                .map(|desc| desc.name().to_string());
            #[cfg(not(target_os = "windows"))]
            let default_output = host
                .default_output_device()
                .and_then(|d| d.description().ok())
                .map(|desc| desc.name().to_string());

            let mut devices = Vec::new();
            collect_devices(
                &host,
                "input",
                default_input.as_deref(),
                &mut devices,
                &mut issues,
            );
            collect_devices(
                &host,
                "output",
                default_output.as_deref(),
                &mut devices,
                &mut issues,
            );

            hosts_out.push(AudioHost {
                id: host_id.name().to_lowercase(),
                name: host_id.name().to_string(),
                is_default: host_id == default_host_id,
                devices,
            });
        }

        AudioDevices {
            hosts: hosts_out,
            issues,
        }
    }

    #[allow(deprecated)] // DeviceTrait::name() gives the raw pcm_id we need for `recommended`.
    fn collect_devices(
        host: &cpal::Host,
        direction: &str,
        default_name: Option<&str>,
        devices: &mut Vec<AudioDevice>,
        issues: &mut Vec<CollectorIssue>,
    ) {
        let iter = match direction {
            "input" => host.input_devices(),
            "output" => host.output_devices(),
            _ => unreachable!(),
        };
        let iter = match iter {
            Ok(it) => it,
            Err(e) => {
                issues.push(CollectorIssue {
                    kind: "enumerate_failed".into(),
                    message: format!("{} {}: {}", host.id().name(), direction, e),
                    path: None,
                });
                return;
            }
        };

        for dev in iter {
            // pcm_id (cpal `name()`) is the ALSA hw identifier on Linux
            // (e.g. `plughw:CARD=Foo,DEV=0`). On macOS / Windows the
            // recommendation heuristic does not apply and `recommended`
            // stays false.
            //
            // Skip devices that fail to report a pcm_id, matching the
            // modem-side `collect_devices`: a phantom `<unknown>` row
            // confuses operator triage when comparing the live UI to
            // the flare bundle.
            let pcm_id = match dev.name() {
                Ok(id) => id,
                Err(_) => continue,
            };
            // Windows: build a unique display `name` from the WASAPI
            // FriendlyName (`description().extended()[0]`, e.g.
            // `"Speakers (Realtek(R) Audio)"`) or the interface friendly
            // name (`description().driver()`, e.g. `"USB PnP Sound
            // Device"`). cpal's `description().name()` returns only the
            // class label (e.g. `"Speakers"`), shared by every endpoint
            // of that class — two cards of the same class would
            // otherwise produce identical rows in the flare bundle.
            // `is_default` is keyed on the IMMDevice endpoint id
            // surfaced by `Device::id()`, matching the
            // `default_input`/`default_output` source above. Issue #100.
            #[cfg(target_os = "windows")]
            let (name, is_default) = {
                let desc = dev.description().ok();
                let class = desc.as_ref().map(|d| d.name().to_string());
                let friendly = desc
                    .as_ref()
                    .and_then(|d| d.extended().first().cloned())
                    .filter(|s| !s.is_empty())
                    .or_else(|| {
                        desc.as_ref()
                            .and_then(|d| d.driver().map(|s| s.to_string()))
                            .filter(|s| !s.is_empty())
                    });
                let endpoint_id = dev.id().ok().map(|id| id.1);
                let display = friendly
                    .or_else(|| {
                        // Fall back to "ClassName (endpoint id)" so two
                        // identical class labels still differ.
                        match (class, endpoint_id.as_ref()) {
                            (Some(c), Some(id)) => Some(format!("{} ({})", c, id)),
                            (Some(c), None) => Some(c),
                            (None, Some(id)) => Some(id.clone()),
                            (None, None) => None,
                        }
                    })
                    .unwrap_or_else(|| pcm_id.clone());
                let is_default = match (default_name, endpoint_id.as_deref()) {
                    (Some(d), Some(id)) => d == id,
                    _ => false,
                };
                (display, is_default)
            };
            #[cfg(not(target_os = "windows"))]
            let (name, is_default) = {
                let display = dev
                    .description()
                    .map(|d| d.name().to_string())
                    .unwrap_or_else(|_| pcm_id.clone());
                let is_default = default_name.map(|d| d == display).unwrap_or(false);
                (display, is_default)
            };
            let recommended = super::is_recommended_pcm_id(&pcm_id);

            let configs = match direction {
                "input" => dev.supported_input_configs().map(|i| i.collect::<Vec<_>>()),
                "output" => dev.supported_output_configs().map(|i| i.collect::<Vec<_>>()),
                _ => unreachable!(),
            };
            let configs = configs.unwrap_or_default();

            let mut ranges = Vec::with_capacity(configs.len());
            for c in configs {
                ranges.push(AudioStreamConfigRange {
                    channels: c.channels(),
                    min_sample_rate_hz: c.min_sample_rate(),
                    max_sample_rate_hz: c.max_sample_rate(),
                    sample_format: format_sample_format(c.sample_format()),
                });
            }

            devices.push(AudioDevice {
                name,
                direction: direction.to_string(),
                is_default,
                recommended,
                supported_configs: ranges,
            });
        }
    }

    fn format_sample_format(f: cpal::SampleFormat) -> String {
        match f {
            cpal::SampleFormat::I16 => "i16",
            cpal::SampleFormat::U16 => "u16",
            cpal::SampleFormat::F32 => "f32",
            other => return format!("{:?}", other).to_lowercase(),
        }
        .to_string()
    }
}
