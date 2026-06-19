//! Top-level modem orchestration: glues audio sources, demodulators, and
//! the IPC server into a single process. Consumed by `bin/graywolf_modem.rs`.
//!
//! Supports multiple audio devices, multiple channels per device, and
//! multiple demodulator types (AFSK, PSK, 9600).

pub(crate) mod tx_worker;
// Re-export so the android module can implement TxSink on AndroidTxSink
// without exposing the private tx_worker module path externally. Only
// needed when the android audio path is compiled in.
#[cfg(any(target_os = "android", feature = "android-test-stub"))]
pub(crate) use tx_worker::TxSink;

use std::collections::{HashMap, HashSet};
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::mpsc::{sync_channel, Receiver, RecvTimeoutError, SyncSender};
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use crate::audio::{self, AudioChunk, AudioSource, CHUNK_QUEUE_DEPTH};
use crate::demod_afsk::AfskDemodulator;
use crate::demod_afsk_multi::{
    MultiAfskDemodulator, MultiConfig, RECOMMENDED_2DEMOD, RECOMMENDED_3DEMOD,
};
use crate::hdlc::DecodedFrame;
use crate::ipc::proto::{
    ipc_message::Payload, AudioDeviceInfo, AudioDeviceKind, AudioDeviceList, ConfigureChannel,
    ConfigurePtt, DcdChange, DeviceLevelUpdate, EnumerateAudioDevices, InputDeviceLevel,
    InputLevelScanResult, IpcMessage, ReceivedFrame, ScanInputLevels, StatusUpdate, TransmitFrame,
};
use crate::ipc::server::{IpcHandle, IpcInbound};
use crate::modem_9600::Demod9600;
use crate::modem_psk::PskDemodulator;
use crate::tx::ptt::PortRegistry;
use crate::types::{AfskProfile, RetryType, V26Alternative};

/// Current configured state for a single channel.
#[derive(Clone, Debug)]
pub struct ChannelConfig {
    pub channel: u32,
    pub input_device_id: u32,
    pub input_channel: u32,
    pub output_device_id: u32,
    pub output_channel: u32,
    pub baud: u32,
    pub mark_freq: u32,
    pub space_freq: u32,
    pub modem_type: String,
    pub profile: AfskProfile,
    pub num_slicers: usize,
    pub fix_bits: RetryType,
    pub num_decoders: u32,
    pub decoder_offset: i32,
    pub fx25_encode: bool,
    pub il2p_encode: bool,
    /// Multi-demodulator ensemble preset; overrides `profile` and `num_slicers`
    /// when non-empty. Supported values: "" (single demod), "dual", "triple".
    pub demod_ensemble: String,
}

impl Default for ChannelConfig {
    fn default() -> Self {
        Self {
            channel: 0,
            input_device_id: 0,
            input_channel: 0,
            output_device_id: 0,
            output_channel: 0,
            baud: 1200,
            mark_freq: 1200,
            space_freq: 2200,
            modem_type: "afsk".into(),
            profile: AfskProfile::A,
            num_slicers: 1,
            fix_bits: RetryType::None,
            num_decoders: 1,
            decoder_offset: 0,
            fx25_encode: false,
            il2p_encode: false,
            demod_ensemble: String::new(),
        }
    }
}

#[derive(Clone, Debug)]
pub struct AudioConfig {
    pub device_id: u32,
    pub device_name: String,
    pub sample_rate: u32,
    pub channels: u32,
    pub source_type: String,
    pub format: String,
    pub gain_db: f32,
}

impl Default for AudioConfig {
    fn default() -> Self {
        Self {
            device_id: 0,
            device_name: String::new(),
            sample_rate: 44100,
            channels: 1,
            source_type: "soundcard".into(),
            format: "s16le".into(),
            gain_db: 0.0,
        }
    }
}

/// Per-device active audio pipeline.
struct DevicePipeline {
    source: AudioSource,
    sample_rx: Receiver<AudioChunk>,
    channels: Vec<ChannelPipeline>,
    gain: Arc<AtomicU32>, // f32 gain_db stored via f32::to_bits/from_bits
}

/// Per-channel demodulator (within a device pipeline).
enum ChannelDemod {
    Afsk(Box<AfskDemodulator>),
    AfskMulti(Box<MultiAfskDemodulator>),
    Psk(PskDemodulator),
    Baseband9600(Demod9600),
}

struct ChannelPipeline {
    channel_id: u32,
    #[allow(dead_code)]
    input_channel: u32,
    demod: ChannelDemod,
    // Multi-modem: parallel demodulators with frequency offsets
    extra_demods: Vec<ChannelDemod>,
    prev_dcd_any: bool,
    latest_mark: f32,
    latest_space: f32,
    latest_peak: f32,
}

pub struct Modem {
    handle: IpcHandle,
    inbound: Receiver<IpcInbound>,

    // Configuration storage (may have multiple audio devices and channels)
    audio_configs: HashMap<u32, AudioConfig>,
    channel_configs: HashMap<u32, ChannelConfig>,
    ptt_cfgs: HashMap<u32, ConfigurePtt>,
    // Rigctld channels whose driver build failed (daemon wasn't running).
    // Re-attempted lazily on the next TransmitFrame for the channel.
    ptt_rigctld_pending: HashSet<u32>,

    // Shared cache of open PTT hardware (serial fds today, HID handles
    // and gpiochip handles in later phases). One port is opened at most
    // once per device path; see `src/tx/ptt.rs` for why.
    ptt_registry: PortRegistry,

    // Live gain atomics, keyed by device_id (shared with DevicePipeline)
    gain_atoms: HashMap<u32, Arc<AtomicU32>>,

    // Pre-resolved cpal output device handles, keyed by device_id.
    // Populated in start_audio() before input streams open and reused
    // by the TX worker for all transmit playback. On Linux/ALSA,
    // cpal's device enumeration fails once a capture stream holds the
    // hardware, so these must be resolved while the device is idle.
    output_devices: HashMap<u32, cpal::Device>,

    // Pre-negotiated output stream params (channels + sample_format),
    // cached at start_audio time when the device is idle. The TX
    // path uses these to avoid calling supported_output_configs()
    // on a device cpal has lost the ability to enumerate (e.g. while a
    // capture stream is active on the same AIOC hardware, or after a
    // USB-EMI hub event invalidates the cached handle).
    output_configs_cache: HashMap<u32, (u16, cpal::SampleFormat)>,

    // Active audio pipelines, keyed by device_id
    active_devices: HashMap<u32, DevicePipeline>,

    // Dedicated TX worker: owns every output sink and every PTT driver,
    // serializes TX across all channels, and keeps audio drain off the
    // IPC thread.
    tx_worker: tx_worker::TxWorker,

    // Per-channel counters. Previously these were process-wide u64s and
    // emit_status attributed the totals to the first configured channel
    // only, so multi-channel dashboards showed all activity on CH0 and
    // zero on the rest.
    rx_frames: HashMap<u32, u64>,
    tx_frames: HashMap<u32, u64>,
    rx_bad_fcs: HashMap<u32, u64>,
    dcd_transitions: HashMap<u32, u64>,
    last_status_tx: Instant,
    last_level_tx: HashMap<u32, Instant>,
}

impl Modem {
    pub fn new(handle: IpcHandle, inbound: Receiver<IpcInbound>) -> Result<Self, String> {
        let tx_worker = tx_worker::TxWorker::spawn()?;
        Ok(Self {
            handle,
            inbound,
            audio_configs: HashMap::new(),
            channel_configs: HashMap::new(),
            ptt_cfgs: HashMap::new(),
            ptt_rigctld_pending: HashSet::new(),
            ptt_registry: PortRegistry::new(),
            gain_atoms: HashMap::new(),
            output_devices: HashMap::new(),
            active_devices: HashMap::new(),
            tx_worker,
            rx_frames: HashMap::new(),
            tx_frames: HashMap::new(),
            rx_bad_fcs: HashMap::new(),
            dcd_transitions: HashMap::new(),
            last_status_tx: Instant::now(),
            last_level_tx: HashMap::new(),
            output_configs_cache: HashMap::new(),
        })
    }

    /// Main loop: multiplex IPC control messages with audio sample chunks.
    pub fn run(mut self) {
        let status_interval = Duration::from_millis(500);
        loop {
            // Drain pending IPC messages
            loop {
                match self.inbound.try_recv() {
                    Ok(IpcInbound::Message(m)) => {
                        if self.handle_ipc(m) {
                            return;
                        }
                    }
                    Ok(IpcInbound::Disconnected) => {
                        eprintln!("graywolf-modem: peer disconnected, exiting");
                        return;
                    }
                    Ok(IpcInbound::ReadError(e)) => {
                        eprintln!("graywolf-modem: ipc read error: {}, exiting", e);
                        return;
                    }
                    Err(std::sync::mpsc::TryRecvError::Empty) => break,
                    Err(std::sync::mpsc::TryRecvError::Disconnected) => {
                        eprintln!("graywolf-modem: ipc channel closed, exiting");
                        return;
                    }
                }
            }

            // Process audio from all active devices
            let got_audio = self.pump_all_audio();

            // Periodic status push
            if self.last_status_tx.elapsed() >= status_interval {
                self.emit_status(false);
                self.emit_idle_output_levels();
                self.last_status_tx = Instant::now();
            }

            if !got_audio {
                match self.inbound.recv_timeout(Duration::from_millis(20)) {
                    Ok(IpcInbound::Message(m)) => {
                        if self.handle_ipc(m) {
                            return;
                        }
                    }
                    Ok(IpcInbound::Disconnected) | Ok(IpcInbound::ReadError(_)) => return,
                    Err(RecvTimeoutError::Timeout) => {}
                    Err(RecvTimeoutError::Disconnected) => return,
                }
            }
        }
    }

    fn handle_ipc(&mut self, msg: IpcMessage) -> bool {
        match msg.payload {
            Some(Payload::ConfigureAudio(c)) => {
                let gain_db = c.gain_db.clamp(-60.0, 12.0);
                self.audio_configs.insert(c.device_id, AudioConfig {
                    device_id: c.device_id,
                    device_name: c.device_name,
                    sample_rate: c.sample_rate,
                    channels: c.channels,
                    source_type: c.source_type,
                    format: c.format,
                    gain_db,
                });
                // Update or create the gain atomic
                let atom = self.gain_atoms
                    .entry(c.device_id)
                    .or_insert_with(|| Arc::new(AtomicU32::new(0f32.to_bits())));
                atom.store(gain_db.to_bits(), Ordering::Relaxed);
            }
            Some(Payload::ConfigureChannel(c)) => {
                self.channel_configs.insert(c.channel, parse_channel(&c));
            }
            Some(Payload::ConfigurePtt(p)) => {
                self.apply_ptt_config(p);
            }
            Some(Payload::StartAudio(_)) => {
                // start_audio synchronously joins audio threads, so the
                // device should be free immediately. Retry once with a
                // delay as a safety net for external races (e.g. systemd
                // restart where the previous process hasn't fully exited).
                match self.start_audio() {
                    Ok(()) => {}
                    Err(e) => {
                        eprintln!(
                            "graywolf-modem: start_audio failed ({}), retrying in 500ms", e
                        );
                        std::thread::sleep(std::time::Duration::from_millis(500));
                        if let Err(e) = self.start_audio() {
                            eprintln!("graywolf-modem: start_audio failed: {}", e);
                        }
                    }
                }
            }
            Some(Payload::StopAudio(_)) => {
                self.stop_all_audio();
            }
            Some(Payload::EnumerateAudioDevices(req)) => {
                self.handle_enumerate_devices(req);
            }
            Some(Payload::ScanInputLevels(req)) => {
                self.handle_scan_input_levels(req);
            }
            Some(Payload::SetDeviceGain(g)) => {
                let gain_db = g.gain_db.clamp(-60.0, 12.0);
                if let Some(acfg) = self.audio_configs.get_mut(&g.device_id) {
                    acfg.gain_db = gain_db;
                }
                let atom = self.gain_atoms
                    .entry(g.device_id)
                    .or_insert_with(|| Arc::new(AtomicU32::new(0f32.to_bits())));
                atom.store(gain_db.to_bits(), Ordering::Relaxed);
            }
            Some(Payload::TransmitFrame(tf)) => {
                self.handle_transmit_frame(tf);
            }
            Some(Payload::ManualPtt(mp)) => {
                if let Err(e) = self.tx_worker.manual_key(mp.channel, mp.keyed) {
                    eprintln!(
                        "graywolf-modem: ManualPtt channel={} keyed={}: {}",
                        mp.channel, mp.keyed, e
                    );
                }
            }
            Some(Payload::Shutdown(_)) => {
                self.graceful_shutdown();
                return true;
            }
            Some(Payload::TransmitTestSignal(req)) => {
                self.handle_transmit_test_signal(req);
            }
            Some(Payload::ReceivedFrame(_))
            | Some(Payload::DcdChange(_))
            | Some(Payload::StatusUpdate(_))
            | Some(Payload::ModemReady(_))
            | Some(Payload::AudioDeviceList(_))
            | Some(Payload::DeviceLevelUpdate(_))
            | Some(Payload::InputLevelScanResult(_))
            | Some(Payload::TestSignalResult(_)) => {
                // Rust → Go only; ignore if echoed back.
            }
            None => {}
        }
        false
    }

    fn start_audio(&mut self) -> Result<(), String> {
        // Stop existing pipelines first
        self.stop_all_audio();

        // Group channels by input_device_id
        let mut channels_by_device: HashMap<u32, Vec<ChannelConfig>> = HashMap::new();
        for ccfg in self.channel_configs.values() {
            channels_by_device
                .entry(ccfg.input_device_id)
                .or_default()
                .push(ccfg.clone());
        }

        // If no channels configured, use defaults
        if channels_by_device.is_empty() {
            let default_ccfg = ChannelConfig::default();
            channels_by_device.entry(0).or_default().push(default_ccfg);
        }

        // Pre-resolve output devices before opening input streams.
        // This lets the TX worker use cached Device handles without
        // enumerating at transmit time. Also pre-negotiate channel
        // count + sample format while the device is idle, so later
        // transmits don't have to call
        // supported_output_configs() (which fails once an input capture
        // stream holds the same hardware).
        self.output_devices.clear();
        self.output_configs_cache.clear();
        let mut seen_outputs = std::collections::HashSet::new();
        for ccfg in self.channel_configs.values() {
            let dev_id = ccfg.output_device_id;
            if dev_id == 0 || !seen_outputs.insert(dev_id) {
                continue;
            }
            if let Some(acfg) = self.audio_configs.get(&dev_id) {
                match audio::soundcard::resolve_output_device(&acfg.device_name) {
                    Ok(device) => {
                        // Pre-negotiate the (channels, sample_format) pair
                        // while the device is idle. supported_output_configs()
                        // tends to fail later when the AIOC's input PCM is
                        // actively captured. negotiate_channels() falls back
                        // gracefully if the configured channel count isn't
                        // supported (e.g. stereo-only USB cards).
                        let preferred_ch = acfg.channels.max(1) as u16;
                        let neg = audio::soundcard::negotiate_channels(
                            &device, acfg.sample_rate, preferred_ch, "output",
                            |d| { use cpal::traits::DeviceTrait; d.supported_output_configs() },
                        );
                        let fmt = {
                            use cpal::traits::DeviceTrait;
                            device.default_output_config().ok().map(|c| c.sample_format())
                        };
                        if let (Ok(ch), Some(f)) = (neg, fmt) {
                            self.output_configs_cache.insert(dev_id, (ch, f));
                        } else {
                            eprintln!(
                                "graywolf-modem: failed to pre-negotiate output configs for device_id={} ({}); TX may fail later if device gets busy",
                                dev_id, acfg.device_name
                            );
                        }
                        self.tx_worker.prepare_output(dev_id, device.clone());
                        self.output_devices.insert(dev_id, device);
                    }
                    Err(e) => {
                        eprintln!(
                            "graywolf-modem: failed to resolve output device_id={} ({}): {}",
                            dev_id, acfg.device_name, e
                        );
                    }
                }
            }
        }

        // Start one pipeline per audio device
        for (device_id, channel_cfgs) in &channels_by_device {
            let acfg = self.audio_configs.get(device_id)
                .cloned()
                .unwrap_or_default();

            let (tx, rx): (SyncSender<AudioChunk>, Receiver<AudioChunk>) =
                sync_channel(CHUNK_QUEUE_DEPTH);

            let source = match acfg.source_type.as_str() {
                "soundcard" => audio::soundcard::spawn(
                    audio::soundcard::SoundcardConfig {
                        device_name: acfg.device_name.clone(),
                        sample_rate: acfg.sample_rate,
                        channels: acfg.channels,
                        audio_channel: channel_cfgs.first().map(|c| c.input_channel).unwrap_or(0),
                    },
                    tx,
                )?,
                "flac" => audio::flac::spawn(
                    audio::flac::FlacConfig {
                        path: acfg.device_name.clone(),
                        rate_override: 0,
                        audio_channel: channel_cfgs.first().map(|c| c.input_channel).unwrap_or(0),
                    },
                    tx,
                )?,
                "flac_fast" => audio::flac::spawn_fast(
                    &acfg.device_name,
                    channel_cfgs.first().map(|c| c.input_channel).unwrap_or(0),
                    tx,
                )?,
                "stdin" => audio::stdin_raw::spawn(acfg.sample_rate, tx)?,
                "sdr_udp" => {
                    let udp_cfg = crate::sdr::parse_config(
                        &acfg.device_name, acfg.sample_rate, &acfg.format,
                    );
                    crate::sdr::spawn(udp_cfg, tx)?
                }
                other => return Err(format!("unknown source_type: {}", other)),
            };

            let sample_rate = source.sample_rate;

            // Build channel pipelines
            let mut chan_pipelines = Vec::new();
            for ccfg in channel_cfgs {
                let demod = create_demod(ccfg, sample_rate);
                let mut extra_demods = Vec::new();

                // Multi-modem parallel processing
                if ccfg.num_decoders > 1 && ccfg.decoder_offset != 0 {
                    for d in 1..ccfg.num_decoders {
                        let offset = ccfg.decoder_offset * d as i32;
                        let mut offset_cfg = ccfg.clone();
                        if offset_cfg.modem_type == "afsk" {
                            offset_cfg.mark_freq = (offset_cfg.mark_freq as i32 + offset) as u32;
                            offset_cfg.space_freq = (offset_cfg.space_freq as i32 + offset) as u32;
                        }
                        extra_demods.push(create_demod(&offset_cfg, sample_rate));
                    }
                }

                chan_pipelines.push(ChannelPipeline {
                    channel_id: ccfg.channel,
                    input_channel: ccfg.input_channel,
                    demod,
                    extra_demods,
                    prev_dcd_any: false,
                    latest_mark: 0.0,
                    latest_space: 0.0,
                    latest_peak: 0.0,
                });
            }

            let gain_atom = self.gain_atoms
                .entry(*device_id)
                .or_insert_with(|| Arc::new(AtomicU32::new(acfg.gain_db.to_bits())))
                .clone();
            self.active_devices.insert(*device_id, DevicePipeline {
                source,
                sample_rx: rx,
                channels: chan_pipelines,
                gain: gain_atom,
            });
        }

        Ok(())
    }

    fn stop_all_audio(&mut self) {
        for (_, mut pipe) in self.active_devices.drain() {
            pipe.source.stop_and_join();
        }
        // Symmetry with the input side: stop means stop. Tell the TX
        // worker to drop its cached sinks so a subsequent ConfigureAudio
        // re-opens the device with the new settings instead of reusing a
        // sink built from stale config.
        self.tx_worker.release_sinks();
    }

    fn pump_all_audio(&mut self) -> bool {
        let mut got_any = false;
        let device_ids: Vec<u32> = self.active_devices.keys().cloned().collect();
        let now = Instant::now();

        for device_id in device_ids {
            if let Some(pipe) = self.active_devices.get_mut(&device_id) {
                match pipe.sample_rx.recv_timeout(Duration::from_millis(1)) {
                    Ok(mut chunk) => {
                        got_any = true;

                        // Apply software gain
                        let gain_db = f32::from_bits(pipe.gain.load(Ordering::Relaxed));
                        let mut clipping = false;
                        if gain_db.abs() > f32::EPSILON {
                            let gain_linear = 10f32.powf(gain_db / 20.0);
                            for s in &mut chunk {
                                let amplified = (*s as f32) * gain_linear;
                                let limited = if gain_db > 0.0 {
                                    (amplified / 32768.0).tanh() * 32768.0
                                } else {
                                    amplified
                                };
                                if limited.abs() > 32000.0 {
                                    clipping = true;
                                }
                                *s = limited.clamp(-32767.0, 32767.0) as i16;
                            }
                        }

                        // Compute per-device peak and RMS (post-gain)
                        let mut peak_abs: f32 = 0.0;
                        let mut sum_sq: f64 = 0.0;
                        for &s in &chunk {
                            let a = (s as f32).abs();
                            if a > peak_abs { peak_abs = a; }
                            sum_sq += (s as f64) * (s as f64);
                        }
                        let peak_linear = peak_abs / 32768.0;
                        let rms_linear = if !chunk.is_empty() {
                            ((sum_sq / chunk.len() as f64).sqrt() / 32768.0) as f32
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

                        // Emit DeviceLevelUpdate at ~5 Hz
                        let last = self.last_level_tx.entry(device_id).or_insert(now - Duration::from_secs(1));
                        if now.duration_since(*last) >= Duration::from_millis(200) {
                            let msg = IpcMessage::device_level_update(DeviceLevelUpdate {
                                device_id,
                                peak_dbfs,
                                rms_dbfs,
                                clipping,
                            });
                            let _ = self.handle.send(&msg);
                            *last = now;
                        }

                        // Feed chunk to all channels on this device
                        for chan_pipe in &mut pipe.channels {
                            chan_pipe.latest_peak = peak_linear;

                            // Feed primary demod
                            for s in &chunk {
                                process_demod_sample(&mut chan_pipe.demod, *s as i32);
                            }

                            // Feed extra (multi-modem) demods
                            for extra in &mut chan_pipe.extra_demods {
                                for s in &chunk {
                                    process_demod_sample(extra, *s as i32);
                                }
                            }

                            // Collect frames
                            let mut all_frames = take_demod_frames(&mut chan_pipe.demod);
                            for extra in &mut chan_pipe.extra_demods {
                                all_frames.extend(take_demod_frames(extra));
                            }

                            // Drain bad-FCS counts from every decoder this
                            // channel owns (primary + any extra demods) and
                            // fold into the per-channel counter. Must run
                            // every pump tick so the counter tracks decoder
                            // state rather than drifting.
                            let mut bad = take_demod_bad_fcs(&mut chan_pipe.demod);
                            for extra in &mut chan_pipe.extra_demods {
                                bad = bad.saturating_add(take_demod_bad_fcs(extra));
                            }
                            if bad > 0 {
                                *self.rx_bad_fcs.entry(chan_pipe.channel_id).or_default() += bad;
                            }

                            for f in all_frames {
                                *self.rx_frames.entry(chan_pipe.channel_id).or_default() += 1;
                                let msg = IpcMessage::received_frame(build_received(&f));
                                if let Err(e) = self.handle.send(&msg) {
                                    eprintln!("graywolf-modem: ipc send failed: {}", e);
                                }
                            }

                            // DCD changes from primary demod
                            let chan_id = chan_pipe.channel_id as usize;
                            let dcd_changes = take_demod_dcd_changes(
                                &mut chan_pipe.demod,
                                &mut chan_pipe.prev_dcd_any,
                                chan_id,
                                0,
                            );
                            for c in dcd_changes {
                                *self.dcd_transitions.entry(chan_pipe.channel_id).or_default() += 1;
                                let msg = IpcMessage::dcd_change(DcdChange {
                                    channel: c.chan as u32,
                                    subchan: c.subchan as u32,
                                    slice: c.slice as u32,
                                    detected: c.data_detect,
                                    timestamp_ns: now_ns(),
                                });
                                let _ = self.handle.send(&msg);
                            }
                        }
                    }
                    Err(RecvTimeoutError::Timeout) => {}
                    Err(RecvTimeoutError::Disconnected) => {
                        eprintln!("graywolf-modem: audio source {} ended", device_id);
                        if let Some(pipe) = self.active_devices.remove(&device_id) {
                            pipe.source.stop();
                        }
                    }
                }
            }
        }
        got_any
    }

    fn handle_enumerate_devices(&self, req: EnumerateAudioDevices) {
        // Snapshot the pcm_ids currently held open by live capture
        // streams, with the rate/channels they are running at. The
        // enumerator surfaces these from cache rather than probing
        // in-use hardware — probing a device a running radio holds can
        // disrupt it, and cpal stops listing a held raw hw: device so a
        // naive rescan loses it entirely.
        let cfg_for = |id: &u32| -> Option<(String, u32, u32)> {
            self.audio_configs.get(id).and_then(|acfg| {
                if acfg.source_type == "soundcard" && !acfg.device_name.is_empty() {
                    Some((acfg.device_name.clone(), acfg.sample_rate, acfg.channels))
                } else {
                    None
                }
            })
        };
        let mut in_use_input: HashMap<String, (u32, u32)> = HashMap::new();
        for id in self.active_devices.keys() {
            if let Some((name, sr, ch)) = cfg_for(id) {
                in_use_input.insert(name, (sr, ch));
            }
        }
        // Output devices held by the TX worker. The AIOC shares one PCM
        // for capture + playback, so once RX holds it the output path's
        // supported_output_configs() fails and the old enumeration drops
        // the device. Surface configured/in-use outputs from cache too.
        let mut in_use_output: HashMap<String, (u32, u32)> = HashMap::new();
        for id in self.output_devices.keys() {
            if let Some((name, sr, ch)) = cfg_for(id) {
                in_use_output.insert(name, (sr, ch));
            }
        }

        // Probing candidate PCMs takes up to a few hundred ms per idle
        // card; run off the IPC thread so control messages keep flowing
        // (mirrors handle_scan_input_levels).
        let handle = self.handle.clone();
        std::thread::Builder::new()
            .name("enumerate-devices".into())
            .spawn(move || {
                let devices = enumerate_audio_devices(
                    req.include_output,
                    &in_use_input,
                    &in_use_output,
                );
                let msg = IpcMessage {
                    payload: Some(Payload::AudioDeviceList(AudioDeviceList {
                        request_id: req.request_id,
                        devices,
                    })),
                };
                if let Err(e) = handle.send(&msg) {
                    eprintln!("graywolf-modem: send AudioDeviceList failed: {}", e);
                }
            })
            .ok();
    }

    fn handle_scan_input_levels(&self, req: ScanInputLevels) {
        // pcm_ids held open by a running channel — the scanner reports
        // these as "in use" instead of failing to open a busy device.
        let mut in_use_input: HashMap<String, (u32, u32)> = HashMap::new();
        for id in self.active_devices.keys() {
            if let Some(acfg) = self.audio_configs.get(id) {
                if acfg.source_type == "soundcard" && !acfg.device_name.is_empty() {
                    in_use_input.insert(
                        acfg.device_name.clone(),
                        (acfg.sample_rate, acfg.channels),
                    );
                }
            }
        }
        let handle = self.handle.clone();
        std::thread::Builder::new()
            .name("scan-input-levels".into())
            .spawn(move || {
                let duration_ms = if req.duration_ms == 0 { 500 } else { req.duration_ms };
                let results = scan_input_levels(duration_ms, &in_use_input);
                let msg = IpcMessage::input_level_scan_result(InputLevelScanResult {
                    request_id: req.request_id,
                    devices: results,
                });
                let _ = handle.send(&msg);
            })
            .ok();
    }

    fn emit_status(&mut self, final_: bool) {
        // One StatusUpdate per configured channel so the dashboard can show
        // correct per-channel RX/TX counters in multi-channel setups. Audio
        // levels are read from the matching ChannelPipeline if the device
        // is active, else zero.
        let mut channels: Vec<u32> = self.channel_configs.keys().copied().collect();
        channels.sort();

        // Prune counters for channels that no longer exist in config so the
        // maps can't drift out of sync if a future RemoveChannel path lands.
        // No-op today (channels are never removed) but keeps the invariant
        // local to emit_status rather than scattered across call sites.
        let live: HashSet<u32> = self.channel_configs.keys().copied().collect();
        self.rx_frames.retain(|k, _| live.contains(k));
        self.tx_frames.retain(|k, _| live.contains(k));
        self.rx_bad_fcs.retain(|k, _| live.contains(k));
        self.dcd_transitions.retain(|k, _| live.contains(k));

        if channels.is_empty() {
            // Preserve the shutdown handshake when no channels are
            // configured: the peer waits for a StatusUpdate with
            // shutdown_complete=true before half-closing the socket.
            if final_ {
                let s = StatusUpdate {
                    channel: 0,
                    rx_frames: 0,
                    rx_bad_fcs: 0,
                    tx_frames: 0,
                    dcd_transitions: 0,
                    audio_level_mark: 0.0,
                    audio_level_space: 0.0,
                    audio_level_peak: 0.0,
                    dcd_state: false,
                    shutdown_complete: true,
                    timestamp_ns: now_ns(),
                };
                let _ = self.handle.send(&IpcMessage::status_update(s));
            }
            return;
        }

        let last_idx = channels.len() - 1;
        for (i, ch) in channels.iter().enumerate() {
            let (mark, space, peak, dcd_state) = self.channel_audio_state(*ch);
            let s = StatusUpdate {
                channel: *ch,
                rx_frames: self.rx_frames.get(ch).copied().unwrap_or(0),
                rx_bad_fcs: self.rx_bad_fcs.get(ch).copied().unwrap_or(0),
                tx_frames: self.tx_frames.get(ch).copied().unwrap_or(0),
                dcd_transitions: self.dcd_transitions.get(ch).copied().unwrap_or(0),
                audio_level_mark: mark,
                audio_level_space: space,
                audio_level_peak: peak,
                dcd_state,
                shutdown_complete: final_ && i == last_idx,
                timestamp_ns: now_ns(),
            };
            let _ = self.handle.send(&IpcMessage::status_update(s));
        }
    }

    /// Emit a -60 dBFS DeviceLevelUpdate for each configured output
    /// device that hasn't had a TX-driven update in the last ~750ms.
    /// The TX path emits a real peak per transmission and bumps
    /// last_level_tx for the output device; this idle-silence ticker
    /// fills the gap between transmissions so the operator's per-device
    /// meter on the Audio Devices page falls back to silence when the
    /// radio is idle instead of staying at the last TX peak forever.
    /// Without this the output bar shows stale data after each beacon
    /// or test tone -- operator-misleading.
    fn emit_idle_output_levels(&mut self) {
        let now = Instant::now();
        let stale_after = Duration::from_millis(750);
        let mut output_ids: HashSet<u32> = HashSet::new();
        for ccfg in self.channel_configs.values() {
            if ccfg.output_device_id != 0 {
                output_ids.insert(ccfg.output_device_id);
            }
        }
        for id in output_ids {
            let stale = self.last_level_tx
                .get(&id)
                .map(|t| now.duration_since(*t) >= stale_after)
                .unwrap_or(true);
            if !stale {
                continue;
            }
            let msg = IpcMessage::device_level_update(DeviceLevelUpdate {
                device_id: id,
                peak_dbfs: -60.0,
                rms_dbfs: -60.0,
                clipping: false,
            });
            let _ = self.handle.send(&msg);
            self.last_level_tx.insert(id, now);
        }
    }

    fn channel_audio_state(&self, channel: u32) -> (f32, f32, f32, bool) {
        for pipe in self.active_devices.values() {
            for cp in &pipe.channels {
                if cp.channel_id == channel {
                    return (cp.latest_mark, cp.latest_space, cp.latest_peak, cp.prev_dcd_any);
                }
            }
        }
        (0.0, 0.0, 0.0, false)
    }

    fn graceful_shutdown(&mut self) {
        self.stop_all_audio();
        self.emit_status(true);
        let _ = self.handle.shutdown_write();
    }

    /// Persist the latest `ConfigurePtt` for a channel and (re)build the
    /// corresponding PTT driver on the TX worker. Opening the serial
    /// port (or later the HID / gpiochip handle) happens here on the
    /// IPC thread; the resulting `Box<dyn PttDriver>` is shipped across
    /// the channel to the worker, which owns it for the channel's
    /// lifetime. A driver build failure leaves the channel registered
    /// in the worker as-absent, so the next `TransmitFrame` logs a
    /// missing-driver error rather than silently keying nothing.
    fn apply_ptt_config(&mut self, cfg: ConfigurePtt) {
        let channel = cfg.channel;
        let is_rigctld = cfg.method == "rigctld";
        match self.ptt_registry.build_driver(&cfg) {
            Ok(driver) => {
                if let Err(e) = self.tx_worker.register_driver(channel, driver) {
                    eprintln!(
                        "graywolf-modem: ConfigurePtt: register driver for channel {}: {}",
                        channel, e
                    );
                } else {
                    self.ptt_rigctld_pending.remove(&channel);
                    eprintln!(
                        "graywolf-modem: PTT configured for channel {} ({})",
                        channel, cfg.method
                    );
                }
            }
            Err(e) => {
                eprintln!(
                    "graywolf-modem: ConfigurePtt: build driver for channel {} ({}): {}",
                    channel, cfg.method, e
                );
                if is_rigctld {
                    self.ptt_rigctld_pending.insert(channel);
                }
                self.tx_worker.release_driver(channel);
            }
        }
        self.ptt_cfgs.insert(channel, cfg);
    }

    /// Dispatch a single TransmitFrame: build AFSK samples on the IPC
    /// thread (pure DSP, sub-millisecond) and hand off to the TX worker
    /// for the slow I/O (sink creation, sample play-out, PTT sequencing,
    /// drain). This returns immediately; audio drain does not block the
    /// IPC loop and RX audio keeps flowing through `pump_all_audio`.
    fn handle_transmit_frame(&mut self, tf: TransmitFrame) {
        // Lazy retry for rigctld: if the daemon wasn't running when the
        // driver was first built, re-attempt now before dropping the frame.
        // Only rigctld benefits — it's the only networked driver where late
        // availability is expected. Serial/CM108 failures are config errors.
        if self.ptt_rigctld_pending.contains(&tf.channel) {
            if let Some(cfg) = self.ptt_cfgs.get(&tf.channel).cloned() {
                self.apply_ptt_config(cfg);
            }
        }

        let ccfg = match self.channel_configs.get(&tf.channel) {
            Some(c) => c,
            None => {
                eprintln!(
                    "graywolf-modem: TransmitFrame: unknown channel {}",
                    tf.channel
                );
                return;
            }
        };

        let acfg = match self.audio_configs.get(&ccfg.output_device_id) {
            Some(a) => a,
            None => {
                eprintln!(
                    "graywolf-modem: TransmitFrame: no audio config for output_device_id {}",
                    ccfg.output_device_id
                );
                return;
            }
        };

        let ptt_cfg = self.ptt_cfgs.get(&tf.channel);
        let txdelay_ms = effective_ms(tf.txdelay_override_ms, ptt_cfg.map(|p| p.txdelay_ms), 300);
        let txtail_ms = effective_ms(tf.txtail_override_ms, ptt_cfg.map(|p| p.txtail_ms), 100);

        // AFSK params come from ChannelConfig so TX matches RX on every
        // channel — HF APRS at 300 baud was previously encoded as 1200
        // baud Bell 202 because these values were ignored. See issue #22.
        let mut samples = match crate::tx::build_samples(
            &tf.data,
            txdelay_ms,
            txtail_ms,
            acfg.sample_rate,
            ccfg.baud,
            ccfg.mark_freq,
            ccfg.space_freq,
        ) {
            Ok(s) => s,
            Err(e) => {
                eprintln!("graywolf-modem: TransmitFrame: build_samples failed: {}", e);
                return;
            }
        };

        // VOX-keyed radios have no PTT line — the radio keys on audio
        // alone, and its VOX relay takes time to close. Without a lead-in
        // the start of the HDLC preamble is clipped while the relay is
        // still engaging, which can truncate or corrupt the frame
        // (graywolf#220). Prepend a steady tone so the radio is fully
        // keyed before any packet data goes out; the tone is part of the
        // TX buffer, so the output-gain multiplier below is applied to it
        // too. See [`vox_lead_in`] for the tone shape and level rationale.
        if let Some(mut lead) = vox_lead_in(ptt_cfg, acfg.sample_rate, ccfg.mark_freq) {
            lead.extend_from_slice(&samples);
            samples = lead;
        }

        // Digirig Lite tone PTT keys the radio via a tone on the companion
        // (right) channel, emitted by the audio sink for the whole buffer.
        // Prepend a short window of *silence* on this (AFSK / left) channel
        // so the companion tone leads the packet — giving the Digirig's
        // tone detector and the radio's TX switch time to engage before any
        // HDLC data goes out. Unlike VOX (whose lead-in is itself the tone
        // on the same mono channel), here the lead-in is silent because the
        // keying tone rides the other channel. See [`digirig_tone_lead_in`].
        if let Some(mut lead) = digirig_tone_lead_in(ptt_cfg, acfg.sample_rate) {
            lead.extend_from_slice(&samples);
            samples = lead;
        }

        // Apply output device gain to TX audio
        let mut clipping = false;
        if let Some(gain_atom) = self.gain_atoms.get(&ccfg.output_device_id) {
            let gain_db = f32::from_bits(gain_atom.load(std::sync::atomic::Ordering::Relaxed));
            if gain_db.abs() > f32::EPSILON {
                let gain_linear = 10f32.powf(gain_db / 20.0);
                for s in samples.iter_mut() {
                    let amplified = (*s as f32) * gain_linear;
                    if amplified.abs() > 32000.0 {
                        clipping = true;
                    }
                    *s = amplified.clamp(-32767.0, 32767.0) as i16;
                }
            }
        }

        // Emit DeviceLevelUpdate for the output device
        {
            let mut peak_abs: f32 = 0.0;
            let mut sum_sq: f64 = 0.0;
            for &s in &samples {
                let a = (s as f32).abs();
                if a > peak_abs { peak_abs = a; }
                sum_sq += (s as f64) * (s as f64);
            }
            let peak_linear = peak_abs / 32768.0;
            let rms_linear = if !samples.is_empty() {
                ((sum_sq / samples.len() as f64).sqrt() / 32768.0) as f32
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
            let msg = IpcMessage::device_level_update(DeviceLevelUpdate {
                device_id: ccfg.output_device_id,
                peak_dbfs,
                rms_dbfs,
                clipping,
            });
            let _ = self.handle.send(&msg);
            // Mark this output device's level as freshly emitted so the
            // idle-silence ticker doesn't immediately overwrite this real
            // TX peak with -60 dBFS on its next pass.
            self.last_level_tx.insert(ccfg.output_device_id, Instant::now());
        }

        let job = tx_worker::TxJob {
            channel: tf.channel,
            samples,
            sample_rate: acfg.sample_rate,
            output_device_id: ccfg.output_device_id,
            sink_config: audio::soundcard::SoundcardOutputConfig {
                device_name: acfg.device_name.clone(),
                sample_rate: acfg.sample_rate,
                channels: acfg.channels,
                audio_channel: ccfg.output_channel,
                ptt_tone_hz: digirig_tone_hz(ptt_cfg, ccfg.mark_freq),
            },
        };

        if let Err(e) = self.tx_worker.transmit(job) {
            eprintln!("graywolf-modem: TransmitFrame: {}", e);
            return;
        }
        *self.tx_frames.entry(tf.channel).or_default() += 1;
    }

    /// Render a TX test signal (CW callsign, steady tone, or alternating tones)
    /// and submit it to the TX worker, mirroring handle_transmit_frame's pattern.
    /// Send a TestSignalResult reply to Go. Borrows only `self.handle`, so it
    /// composes with the surrounding `&mut self` work in the handler below.
    fn reply_test_signal(&self, request_id: u32, success: bool, error: String) {
        let _ = self.handle.send(&IpcMessage::test_signal_result(
            crate::ipc::proto::TestSignalResult { request_id, success, error },
        ));
    }

    fn handle_transmit_test_signal(&mut self, req: crate::ipc::proto::TransmitTestSignal) {
        // Lazy rigctld retry, same as handle_transmit_frame.
        if self.ptt_rigctld_pending.contains(&req.channel) {
            if let Some(cfg) = self.ptt_cfgs.get(&req.channel).cloned() {
                self.apply_ptt_config(cfg);
            }
        }

        let (output_device_id, output_channel) = match self.channel_configs.get(&req.channel) {
            Some(c) => (c.output_device_id, c.output_channel),
            None => {
                self.reply_test_signal(req.request_id, false, format!("unknown channel {}", req.channel));
                return;
            }
        };

        let (device_name, sample_rate, channels) = match self.audio_configs.get(&output_device_id) {
            Some(a) => (a.device_name.clone(), a.sample_rate, a.channels),
            None => {
                self.reply_test_signal(
                    req.request_id,
                    false,
                    format!("no audio config for output device {}", output_device_id),
                );
                return;
            }
        };

        let mut samples = match req.kind {
            0 => {
                let s = crate::txtest::cw_samples(
                    &req.callsign,
                    sample_rate,
                    req.cw_wpm.max(1),
                    req.freq_a_hz as f32,
                );
                if s.is_empty() {
                    self.reply_test_signal(req.request_id, false, "callsign produced no CW symbols".to_string());
                    return;
                }
                s
            }
            1 => crate::txtest::tone_samples(sample_rate, req.freq_a_hz as f32, req.duration_ms),
            2 => crate::txtest::alternating_samples(
                sample_rate,
                req.freq_a_hz as f32,
                req.freq_b_hz as f32,
                req.duration_ms,
                req.alt_period_ms,
            ),
            other => {
                self.reply_test_signal(req.request_id, false, format!("unknown test signal kind {}", other));
                return;
            }
        };

        // Apply output device gain, matching handle_transmit_frame. Live level
        // metering / DeviceLevelUpdate is intentionally omitted for test signals.
        if let Some(gain_atom) = self.gain_atoms.get(&output_device_id) {
            let gain_db = f32::from_bits(gain_atom.load(std::sync::atomic::Ordering::Relaxed));
            if gain_db.abs() > f32::EPSILON {
                let gain_linear = 10f32.powf(gain_db / 20.0);
                for s in samples.iter_mut() {
                    let amplified = (*s as f32) * gain_linear;
                    *s = amplified.clamp(-32767.0, 32767.0) as i16;
                }
            }
        }

        let job = tx_worker::TxJob {
            channel: req.channel,
            samples,
            sample_rate,
            output_device_id,
            sink_config: audio::soundcard::SoundcardOutputConfig {
                device_name,
                sample_rate,
                channels,
                audio_channel: output_channel,
                // Test signals never carry a PTT keying tone — the operator
                // is exercising the audio path or a hardware PTT line, not
                // Digirig tone keying.
                ptt_tone_hz: 0,
            },
        };

        match self.tx_worker.transmit(job) {
            Ok(()) => {
                *self.tx_frames.entry(req.channel).or_default() += 1;
                self.reply_test_signal(req.request_id, true, String::new());
            }
            Err(e) => self.reply_test_signal(req.request_id, false, e),
        }
    }
}

/// Duration of the steady lead-in tone prepended to a VOX-keyed channel's
/// TX audio, in milliseconds. Gives the radio's VOX circuit time to engage
/// before the HDLC preamble so the start of the packet isn't clipped
/// (graywolf#220). Fixed at 500 ms, the value requested in the issue and a
/// comfortable margin over typical VOX attack times.
const VOX_LEAD_TONE_MS: u32 = 500;

/// True when a channel's PTT config selects VOX keying — either the desktop
/// `vox` method or the Android USB-PTT VOX transport (`method == "android"`
/// with `ptt_method == PTT_METHOD_VOX`). Both flavours key the radio on
/// audio alone, so the TX builder prepends a [`VOX_LEAD_TONE_MS`] lead-in
/// tone for either one.
fn ptt_uses_vox(cfg: &ConfigurePtt) -> bool {
    cfg.method == "vox"
        || (cfg.method == "android"
            && cfg.ptt_method as i32 == crate::tx::ptt_android_consts::PTT_METHOD_VOX)
}

/// Build the VOX lead-in tone for a channel, or `None` when the channel
/// doesn't use VOX (see [`ptt_uses_vox`]). The tone is a steady sine at the
/// channel's `mark_freq` — the same passband the AFSK that follows lives in,
/// so any radio/filter that passes the packet passes the keying tone. Its
/// length is exactly `sample_rate * VOX_LEAD_TONE_MS / 1000` samples.
///
/// The tone is rendered at [`crate::txtest`]'s standard level (~0.6 FS),
/// deliberately a touch hotter than the AFSK frame (~0.5 FS): the only job
/// of this tone is to trip the VOX relay reliably, and a stronger keying
/// burst does that better. The output peak meter will therefore read this
/// tone rather than the slightly quieter frame — an accepted trade-off.
///
/// Factored out of [`Modem::handle_transmit_frame`] so the prepend behaviour
/// is unit-testable without an audio device: the caller just prepends the
/// returned buffer to the frame samples.
fn vox_lead_in(
    ptt_cfg: Option<&ConfigurePtt>,
    sample_rate: u32,
    mark_freq: u32,
) -> Option<Vec<i16>> {
    if ptt_cfg.is_some_and(ptt_uses_vox) {
        Some(crate::txtest::tone_samples(
            sample_rate,
            mark_freq as f32,
            VOX_LEAD_TONE_MS,
        ))
    } else {
        None
    }
}

/// Duration of the silent lead-in prepended to a Digirig-Lite-tone-PTT
/// channel's TX audio, in milliseconds. During this window the AFSK
/// (left) channel is silent while the audio sink emits the keying tone on
/// the companion (right) channel, so the Digirig's tone detector and the
/// radio's TX switch are fully engaged before the HDLC preamble starts.
/// Matches [`VOX_LEAD_TONE_MS`]: a comfortable margin over typical relay
/// and detector engage times.
const DIGIRIG_TONE_LEAD_MS: u32 = 500;

/// True when a channel's PTT config selects Digirig Lite tone keying.
/// Unlike VOX there is no Android flavour — the Digirig Lite is a desktop
/// USB sound device, so only the `digirig_tone` method string matches.
fn ptt_uses_digirig_tone(cfg: &ConfigurePtt) -> bool {
    cfg.method == "digirig_tone"
}

/// Frequency (Hz) of the PTT keying tone the audio sink should emit on the
/// companion channel, or `0` when the channel does not use Digirig tone
/// keying. The tone reuses the channel's `mark_freq` (same passband as the
/// AFSK, as VOX does) so any radio/adapter filtering that passes the packet
/// passes the keying tone too.
fn digirig_tone_hz(ptt_cfg: Option<&ConfigurePtt>, mark_freq: u32) -> u32 {
    if ptt_cfg.is_some_and(ptt_uses_digirig_tone) {
        mark_freq
    } else {
        0
    }
}

/// Build the silent lead-in buffer for a Digirig-Lite-tone-PTT channel, or
/// `None` when the channel doesn't use it. The buffer is exactly
/// `sample_rate * DIGIRIG_TONE_LEAD_MS / 1000` samples of silence; the
/// companion-channel tone (emitted by the sink) plays over it so the radio
/// keys before the packet. Factored out so the prepend behaviour is
/// unit-testable without an audio device, mirroring [`vox_lead_in`].
fn digirig_tone_lead_in(ptt_cfg: Option<&ConfigurePtt>, sample_rate: u32) -> Option<Vec<i16>> {
    if ptt_cfg.is_some_and(ptt_uses_digirig_tone) {
        let n = (sample_rate as u64 * DIGIRIG_TONE_LEAD_MS as u64 / 1000) as usize;
        Some(vec![0i16; n])
    } else {
        None
    }
}

/// Resolve a TX timing parameter: explicit override wins, then the stored
/// PTT config, else the hardcoded default. Zero is treated as "not set" in
/// both the override and the stored config — callers set it to 0 to mean
/// "inherit".
fn effective_ms(override_ms: u32, configured_ms: Option<u32>, default_ms: u32) -> u32 {
    if override_ms != 0 {
        return override_ms;
    }
    match configured_ms {
        Some(v) if v != 0 => v,
        _ => default_ms,
    }
}

fn create_demod(ccfg: &ChannelConfig, sample_rate: u32) -> ChannelDemod {
    match ccfg.modem_type.as_str() {
        "psk" => {
            let carrier = (ccfg.mark_freq + ccfg.space_freq) / 2;
            let v26 = if ccfg.profile == AfskProfile::A {
                V26Alternative::A
            } else {
                V26Alternative::B
            };
            let mut demod = PskDemodulator::new(
                sample_rate, ccfg.baud, carrier, v26,
                ccfg.channel as usize, 0,
            );
            if ccfg.fix_bits != RetryType::None {
                demod.set_fix_bits(ccfg.fix_bits);
            }
            ChannelDemod::Psk(demod)
        }
        "9600" | "scramble" | "baseband" => {
            let mut demod = Demod9600::new(
                sample_rate, ccfg.baud,
                ccfg.channel as usize, 0,
            );
            if ccfg.fix_bits != RetryType::None {
                demod.set_fix_bits(ccfg.fix_bits);
            }
            ChannelDemod::Baseband9600(demod)
        }
        _ => {
            // Default: AFSK. The `demod_ensemble` field selects the demod
            // architecture. Recognized values:
            //   ""        → same as "triple" (default for new installs,
            //               picked because it matches or beats Direwolf
            //               -P AD+ on every WA8LMF reference track while
            //               costing ~1.6% of one CPU core)
            //   "triple"  → Profile A ×9 + A ×9 with hard-limiter + B ×9
            //   "dual"    → Profile A ×9, with and without hard-limiter
            //   "single"  → explicit single-demod (legacy, respects
            //               `profile` and `num_slicers`)
            let effective = if ccfg.demod_ensemble.is_empty() {
                "triple"
            } else {
                ccfg.demod_ensemble.as_str()
            };
            match effective {
                "dual" | "triple" => {
                    let preset: &[MultiConfig] = match effective {
                        "dual" => &RECOMMENDED_2DEMOD,
                        "triple" => &RECOMMENDED_3DEMOD,
                        _ => unreachable!(),
                    };
                    let multi = MultiAfskDemodulator::new(
                        sample_rate, ccfg.baud, ccfg.mark_freq, ccfg.space_freq,
                        ccfg.channel as usize,
                        preset,
                    );
                    // Note: fix_bits is intentionally not plumbed through the
                    // ensemble — Direwolf's best mode doesn't use bit-flipping
                    // either, and the ensemble's cross-demod diversity already
                    // covers the cases bit-flipping was designed to catch.
                    ChannelDemod::AfskMulti(Box::new(multi))
                }
                _ => {
                    // "single" or any unrecognized value falls back to the
                    // legacy single-demodulator path.
                    let mut demod = AfskDemodulator::new(
                        sample_rate, ccfg.baud, ccfg.mark_freq, ccfg.space_freq,
                        ccfg.profile, ccfg.channel as usize, 0,
                    );
                    if ccfg.num_slicers > 1 {
                        demod.set_num_slicers(ccfg.num_slicers);
                    }
                    if ccfg.fix_bits != RetryType::None {
                        demod.set_fix_bits(ccfg.fix_bits);
                    }
                    ChannelDemod::Afsk(Box::new(demod))
                }
            }
        }
    }
}

fn process_demod_sample(demod: &mut ChannelDemod, sample: i32) {
    match demod {
        ChannelDemod::Afsk(d) => d.process_sample(sample),
        ChannelDemod::AfskMulti(d) => d.process_sample(sample),
        ChannelDemod::Psk(d) => d.process_sample(sample),
        ChannelDemod::Baseband9600(d) => d.process_sample(sample),
    }
}

fn take_demod_frames(demod: &mut ChannelDemod) -> Vec<DecodedFrame> {
    match demod {
        ChannelDemod::Afsk(d) => d.take_frames(),
        ChannelDemod::AfskMulti(d) => d.take_frames(),
        ChannelDemod::Psk(d) => d.take_frames(),
        ChannelDemod::Baseband9600(d) => d.take_frames(),
    }
}

fn take_demod_bad_fcs(demod: &mut ChannelDemod) -> u64 {
    match demod {
        ChannelDemod::Afsk(d) => d.take_bad_fcs(),
        ChannelDemod::AfskMulti(d) => d.take_bad_fcs(),
        ChannelDemod::Psk(d) => d.take_bad_fcs(),
        ChannelDemod::Baseband9600(d) => d.take_bad_fcs(),
    }
}

fn take_demod_dcd_changes(
    demod: &mut ChannelDemod,
    prev_dcd: &mut bool,
    chan: usize,
    subchan: usize,
) -> Vec<crate::demod_afsk::DcdChange> {
    match demod {
        ChannelDemod::Afsk(d) => d.take_dcd_changes(),
        ChannelDemod::AfskMulti(d) => {
            // MultiAfskDemodulator reports any-demod-has-DCD as a coarse
            // boolean, translated into an edge-triggered event for the
            // channel. Finer per-slicer DCD is not exposed by the ensemble.
            let cur = d.data_detect_any();
            if cur != *prev_dcd {
                *prev_dcd = cur;
                vec![crate::demod_afsk::DcdChange {
                    chan,
                    subchan,
                    slice: 0,
                    data_detect: cur,
                }]
            } else {
                Vec::new()
            }
        }
        ChannelDemod::Psk(d) => {
            let cur = d.data_detect();
            if cur != *prev_dcd {
                *prev_dcd = cur;
                vec![crate::demod_afsk::DcdChange {
                    chan,
                    subchan,
                    slice: 0,
                    data_detect: cur,
                }]
            } else {
                Vec::new()
            }
        }
        ChannelDemod::Baseband9600(d) => {
            let cur = d.data_detect();
            if cur != *prev_dcd {
                *prev_dcd = cur;
                vec![crate::demod_afsk::DcdChange {
                    chan,
                    subchan,
                    slice: 0,
                    data_detect: cur,
                }]
            } else {
                Vec::new()
            }
        }
    }
}

/// Known USB vendor:product → friendly name for common ham radio audio devices.
#[cfg(target_os = "linux")]
fn known_usb_audio_name(vid: &str, pid: &str) -> Option<&'static str> {
    match (vid, pid) {
        ("0d8c", "000c") | ("0d8c", "000e") => Some("CM108 USB Audio (Digirig / generic)"),
        ("0d8c", "0008") => Some("CM108B USB Audio"),
        ("0d8c", "0012") | ("0d8c", "0014") => Some("CM108AH USB Audio"),
        ("0d8c", "013c") => Some("CM108 USB Audio"),
        ("0d8c", "0013") => Some("CM119 USB Audio"),
        ("0d8c", "0139") => Some("CM119A USB Audio"),
        ("08bb", "2912") => Some("Texas Instruments PCM2912A (SignaLink USB)"),
        ("08bb", "29b0") | ("08bb", "29b3") => Some("Texas Instruments PCM2900 USB Audio"),
        ("0bda", "4014") => Some("Realtek USB Audio"),
        _ => None,
    }
}

/// On Linux/ALSA, resolve a friendly description by reading /proc/asound/cards
/// and USB sysfs attributes. Returns an empty string on non-Linux.
#[cfg(target_os = "linux")]
fn alsa_card_description(cpal_name: &str) -> String {
    let card_id = cpal_name
        .split("CARD=")
        .nth(1)
        .and_then(|s| s.split([',', ':']).next())
        .unwrap_or("");
    if card_id.is_empty() {
        return String::new();
    }

    // Resolve card number from /proc/asound/cards.
    let mut card_num = String::new();
    let mut long_name = String::new();
    if let Ok(contents) = std::fs::read_to_string("/proc/asound/cards") {
        for line in contents.lines() {
            let trimmed = line.trim();
            if !trimmed.chars().next().is_some_and(|c| c.is_ascii_digit()) {
                continue;
            }
            let bracket_start = match trimmed.find('[') { Some(i) => i, None => continue };
            let bracket_end = match trimmed.find(']') { Some(i) => i, None => continue };
            let num = trimmed[..bracket_start].trim();
            let short = trimmed[bracket_start + 1..bracket_end].trim();

            if num == card_id || short == card_id {
                card_num = num.to_string();
                if let Some(pos) = trimmed.find(" - ") {
                    long_name = trimmed[pos + 3..].trim().to_string();
                }
                break;
            }
        }
    }

    // Try sysfs USB vendor:product lookup for a better name.
    if !card_num.is_empty() {
        let sysfs_dir = format!("/sys/class/sound/card{}/device", card_num);
        if let Ok(resolved) = std::fs::read_link(&sysfs_dir) {
            // Walk up to find USB device directory.
            let mut dir = std::path::PathBuf::from(&sysfs_dir);
            if resolved.is_relative() {
                dir = std::path::Path::new(&sysfs_dir).parent().unwrap_or(std::path::Path::new("/")).join(&resolved);
            }
            for _ in 0..6 {
                let vid_path = dir.join("idVendor");
                if vid_path.exists() {
                    let vid = read_sysfs(&vid_path);
                    let pid = read_sysfs(&dir.join("idProduct"));
                    if let Some(name) = known_usb_audio_name(&vid, &pid) {
                        return name.to_string();
                    }
                    // Use USB product string if it's more specific than the generic.
                    let usb_product = read_sysfs(&dir.join("product"));
                    if !usb_product.is_empty() && usb_product != "USB Audio Device"
                        && usb_product != "USB PnP Sound Device"
                    {
                        return usb_product;
                    }
                    break;
                }
                dir = match dir.parent() {
                    Some(p) => p.to_path_buf(),
                    None => break,
                };
            }
        }
    }

    long_name
}

#[cfg(target_os = "linux")]
fn read_sysfs(path: &std::path::Path) -> String {
    std::fs::read_to_string(path)
        .map(|s| s.trim().to_string())
        .unwrap_or_default()
}

#[cfg(not(target_os = "linux"))]
fn alsa_card_description(_cpal_name: &str) -> String {
    String::new()
}

/// On Windows, pull a device-specific friendly string out of cpal's
/// `DeviceDescription`. `description().name()` itself prefers
/// `DEVPKEY_Device_DeviceDesc` (the device class label, e.g.
/// `"Speakers"`), which is shared by every endpoint of that class —
/// so it can't disambiguate two soundcards. We try, in order:
///
/// 1. `extended()[0]` — cpal stores `DEVPKEY_Device_FriendlyName`
///    there when it differs from the class name (see
///    `cpal/host/wasapi/device.rs:419-423`), e.g.
///    `"Speakers (Realtek(R) Audio)"`.
/// 2. `driver()` — `DEVPKEY_DeviceInterface_FriendlyName`, e.g.
///    `"USB PnP Sound Device"`.
/// 3. empty string — caller's UI fallback (`description || name`)
///    will render the class label.
///
/// The result is surfaced as the proto `description` field. Issue #100.
#[cfg(target_os = "windows")]
fn windows_friendly_name(dev: &cpal::Device) -> String {
    use cpal::traits::DeviceTrait;
    let Ok(desc) = dev.description() else {
        return String::new();
    };
    if let Some(s) = desc.extended().first() {
        if !s.is_empty() {
            return s.clone();
        }
    }
    if let Some(driver) = desc.driver() {
        if !driver.is_empty() {
            return driver.to_string();
        }
    }
    String::new()
}

/// On Windows, return the IMMDevice endpoint id (a per-endpoint GUID
/// string like `{0.0.0.00000000}.{...}`) as the device's stable id.
/// This is what cpal's `Device::id()` exposes; unlike `name()` it is
/// guaranteed unique across endpoints of the same class. Issue #100.
#[cfg(target_os = "windows")]
fn windows_device_id(dev: &cpal::Device) -> Option<String> {
    use cpal::traits::DeviceTrait;
    dev.id().ok().map(|id| id.1)
}

/// dBFS floor representing silence — the theoretical dynamic range of
/// 16-bit audio (~96 dB). Used as the "no signal" value in level scans.
const NOISE_FLOOR_DBFS: f32 = -96.0;

/// dBFS threshold above which we consider a signal present during an
/// input level scan. Roughly -40 dBFS is well above thermal noise but
/// low enough to detect even a quiet radio receiver.
const SIGNAL_THRESHOLD_DBFS: f32 = -40.0;

/// On Linux/ALSA, only expose hw:, plughw:, and the system default.
/// This filters out the many virtual/plugin ALSA devices (sysdefault,
/// front, dmix, dsnoop, etc.) that clutter the device list and can
/// cause matching ambiguity.
#[cfg(target_os = "linux")]
fn is_useful_alsa_device(pcm_id: &str) -> bool {
    pcm_id == "default"
        || pcm_id.starts_with("hw:")
        || pcm_id.starts_with("plughw:")
}

#[cfg(not(target_os = "linux"))]
fn is_useful_alsa_device(_pcm_id: &str) -> bool {
    true
}

/// Collect [`AudioDeviceInfo`] entries from a cpal device iterator.
///
/// Shared between input and output enumeration — the only difference is
/// which config query method (`supported_input_configs` vs
/// `supported_output_configs`) the caller passes via `get_configs`.
///
/// Non-Linux only: Linux input/output go through
/// `collect_input_devices_linux` / `collect_output_devices_linux`, which
/// add physical-card dedup, capture probing, and the in-use cache.
#[cfg(not(target_os = "linux"))]
#[allow(deprecated)] // DeviceTrait::name() gives the raw pcm_id we need
fn collect_devices<I>(
    devices: impl Iterator<Item = cpal::Device>,
    kind: i32,
    default_display_name: Option<&str>,
    host_api: &str,
    get_configs: impl Fn(&cpal::Device) -> Result<I, cpal::SupportedStreamConfigsError>,
) -> Vec<AudioDeviceInfo>
where
    I: Iterator<Item = cpal::SupportedStreamConfigRange>,
{
    use cpal::traits::DeviceTrait;

    let mut out = Vec::new();
    for dev in devices {
        let pcm_id = match dev.name() {
            Ok(id) => id,
            Err(_) => continue,
        };
        if pcm_id == "null" || !is_useful_alsa_device(&pcm_id) {
            continue;
        }
        let display_name = dev.description()
            .map(|d| d.name().to_string())
            .unwrap_or_else(|_| pcm_id.clone());

        let mut sample_rates = Vec::new();
        let mut channel_counts = Vec::new();

        if let Ok(configs) = get_configs(&dev) {
            for cfg in configs {
                let min_rate = cfg.min_sample_rate();
                let max_rate = cfg.max_sample_rate();
                for &rate in audio::STANDARD_SAMPLE_RATES {
                    if rate >= min_rate && rate <= max_rate
                        && !sample_rates.contains(&rate)
                    {
                        sample_rates.push(rate);
                    }
                }
                let ch = cfg.channels() as u32;
                if !channel_counts.contains(&ch) {
                    channel_counts.push(ch);
                }
            }
        }

        // Skip devices with no supported configurations (e.g. HDMI
        // outputs on headless Pi) — unusable and cause null-array
        // issues downstream.
        if sample_rates.is_empty() || channel_counts.is_empty() {
            continue;
        }

        // On Windows, key both the stable id and the default-device
        // match on the IMMDevice endpoint id (cpal `Device::id()`).
        // `pcm_id` (cpal `name()`) is just the device class label there
        // and would flag every output of the class as default. The
        // proto `description` carries the WASAPI FriendlyName so the
        // UI's `description || name` fallback renders a disambiguated
        // string; `recommended` is always false on Windows (no plughw).
        // Issue #100.
        #[cfg(target_os = "windows")]
        let (stable_id, is_default, description, recommended) = {
            let id = match windows_device_id(&dev) {
                Some(id) => id,
                None => continue,
            };
            let is_default = default_display_name == Some(id.as_str());
            (id, is_default, windows_friendly_name(&dev), false)
        };
        #[cfg(not(target_os = "windows"))]
        let (stable_id, is_default, description, recommended) = (
            pcm_id.clone(),
            default_display_name == Some(display_name.as_str()),
            alsa_card_description(&pcm_id),
            // Prefer plughw: devices that use a stable card name (CARD=Foo)
            // rather than a numeric index (CARD=0) which can change across
            // reboots if USB devices enumerate in a different order.
            crate::audio::soundcard::is_recommended_pcm_id(&pcm_id),
        );

        out.push(AudioDeviceInfo {
            name: display_name,
            stable_id,
            kind,
            sample_rates,
            channel_counts,
            host_api: host_api.to_string(),
            is_default,
            description,
            recommended,
        });
    }
    out
}

/// Linux capture-device collection. Three behaviors:
///
/// 1. **Dedup** — cpal's ALSA backend reports the same physical card
///    under numeric (`hw:CARD=1`) and symbolic (`hw:CARD=Device`)
///    aliases. We canonicalize via `/proc/asound/cards` and emit one
///    entry per physical card.
/// 2. **Verified Recommended** — for an idle card we probe candidate
///    PCM forms in preference order (`plughw:` name > `plughw:` index >
///    `hw:` name > `hw:` index) and surface the first that actually
///    streams without POLLERR, badged Recommended. AIOC / DigiRig still
///    win on `plughw:CARD=<name>` exactly as before; cheap chips whose
///    plughw path fails fall through to the raw `hw:` form that works.
/// 3. **In-use stays visible** — a card held open by a live capture
///    stream is never probed (would disrupt the running radio) and
///    surfaced from `in_use` cache, so a rescan no longer loses it.
#[cfg(target_os = "linux")]
#[allow(deprecated)] // DeviceTrait::name() gives the raw pcm_id we need
fn collect_input_devices_linux(
    inputs: impl Iterator<Item = cpal::Device>,
    in_use: &HashMap<String, (u32, u32)>,
    host_api: &str,
    default_input_name: Option<&str>,
) -> Vec<AudioDeviceInfo> {
    use crate::audio::soundcard::{
        alsa_canonical_key, build_card_resolver, group_alsa_cards, parse_proc_asound_cards,
        pick_input_probe_config, probe_capture,
    };
    use cpal::traits::DeviceTrait;

    // (pcm_id, Device) for every useful ALSA capture node cpal reports.
    let mut devs: Vec<(String, cpal::Device)> = Vec::new();
    for dev in inputs {
        let pcm_id = match dev.name() {
            Ok(id) => id,
            Err(_) => continue,
        };
        if pcm_id == "null" || !is_useful_alsa_device(&pcm_id) {
            continue;
        }
        devs.push((pcm_id, dev));
    }

    let cards = std::fs::read_to_string("/proc/asound/cards")
        .map(|c| parse_proc_asound_cards(&c))
        .unwrap_or_default();
    let resolver = build_card_resolver(&cards);
    let canon = |pcm: &str| -> String {
        alsa_canonical_key(pcm, |t| resolver.get(t).copied())
    };

    // Seed grouping with in-use pcm_ids too: once a raw hw: stream
    // holds a card, cpal stops listing it, so it must still group.
    let mut all_pcms: Vec<String> = devs.iter().map(|(p, _)| p.clone()).collect();
    for pcm in in_use.keys() {
        if !all_pcms.contains(pcm) {
            all_pcms.push(pcm.clone());
        }
    }
    let groups = group_alsa_cards(&all_pcms, |t| resolver.get(t).copied());

    // canonical card key -> (in-use pcm_id, running sample_rate, channels)
    let mut in_use_by_card: HashMap<String, (String, u32, u32)> = HashMap::new();
    for (pcm, (sr, ch)) in in_use {
        in_use_by_card
            .entry(canon(pcm))
            .or_insert_with(|| (pcm.clone(), *sr, *ch));
    }

    let find_dev = |pcm: &str| devs.iter().find(|(p, _)| p == pcm).map(|(_, d)| d);
    let configs_for = |dev: &cpal::Device| -> (Vec<u32>, Vec<u32>) {
        let mut sample_rates = Vec::new();
        let mut channel_counts = Vec::new();
        if let Ok(configs) = dev.supported_input_configs() {
            for cfg in configs {
                let (min_rate, max_rate) = (cfg.min_sample_rate(), cfg.max_sample_rate());
                for &rate in audio::STANDARD_SAMPLE_RATES {
                    if rate >= min_rate && rate <= max_rate && !sample_rates.contains(&rate) {
                        sample_rates.push(rate);
                    }
                }
                let c = cfg.channels() as u32;
                if !channel_counts.contains(&c) {
                    channel_counts.push(c);
                }
            }
        }
        (sample_rates, channel_counts)
    };

    let probe_timeout = std::time::Duration::from_millis(250);
    let mut out = Vec::new();
    for group in groups {
        // Card held open by a live capture stream: never probe it.
        if let Some((pcm, sr, ch)) = in_use_by_card.get(&group.key) {
            out.push(AudioDeviceInfo {
                name: alsa_card_description(pcm),
                stable_id: pcm.clone(),
                kind: AudioDeviceKind::Input.into(),
                sample_rates: vec![*sr],
                channel_counts: vec![*ch],
                host_api: host_api.to_string(),
                is_default: false,
                description: alsa_card_description(pcm),
                recommended: true,
            });
            continue;
        }

        // Idle card: first candidate that actually streams wins the
        // Recommended badge. Falls back to the top-ranked candidate
        // (unbadged) so a card that fails every probe still appears.
        let mut chosen: Option<String> = None;
        for cand in &group.candidates {
            if let Some(dev) = find_dev(cand) {
                if probe_capture(dev, probe_timeout) {
                    chosen = Some(cand.clone());
                    break;
                }
            }
        }
        let (pcm, recommended) = match chosen {
            Some(c) => (c, true),
            None => match group.candidates.first() {
                Some(c) => (c.clone(), false),
                None => continue,
            },
        };

        let dev = find_dev(&pcm);
        let (mut sample_rates, mut channel_counts) =
            dev.map(&configs_for).unwrap_or_default();
        if sample_rates.is_empty() || channel_counts.is_empty() {
            if recommended {
                // Streamed fine but cpal won't report ranges: fall back
                // to the negotiated probe config so the row survives the
                // empty-array drop below.
                if let Some((_, cfg)) = dev.and_then(|d| pick_input_probe_config(d).ok()) {
                    sample_rates = vec![cfg.sample_rate];
                    channel_counts = vec![cfg.channels as u32];
                } else {
                    sample_rates = vec![48_000];
                    channel_counts = vec![1];
                }
            } else {
                // Never streamed and no config ranges: dead node (e.g.
                // headless HDMI). Match prior behavior — drop it.
                continue;
            }
        }

        let display_name = dev
            .and_then(|d| d.description().ok())
            .map(|d| d.name().to_string())
            .unwrap_or_else(|| alsa_card_description(&pcm));
        let is_default = default_input_name == Some(display_name.as_str());

        out.push(AudioDeviceInfo {
            name: display_name,
            stable_id: pcm.clone(),
            kind: AudioDeviceKind::Input.into(),
            sample_rates,
            channel_counts,
            host_api: host_api.to_string(),
            is_default,
            description: alsa_card_description(&pcm),
            recommended,
        });
    }
    out
}

/// Linux playback-device collection. Same physical-card dedup as the
/// capture path, but **no probe** (output is not stream-tested; cheap
/// chips don't exhibit the capture POLLERR and probing would make
/// noise). One entry per physical card: the in-use card is surfaced
/// from the cache snapshot (never opened — the AIOC's shared in/out PCM
/// fails `supported_output_configs()` while RX holds it), and an idle
/// card surfaces its top-ranked alias (`plughw:CARD=<name>` first).
/// `recommended` stays the string heuristic for outputs.
#[cfg(target_os = "linux")]
#[allow(deprecated)] // DeviceTrait::name() gives the raw pcm_id we need
fn collect_output_devices_linux(
    outputs: impl Iterator<Item = cpal::Device>,
    in_use: &HashMap<String, (u32, u32)>,
    host_api: &str,
    default_output_name: Option<&str>,
) -> Vec<AudioDeviceInfo> {
    use crate::audio::soundcard::{
        alsa_canonical_key, build_card_resolver, group_alsa_cards, is_recommended_pcm_id,
        parse_proc_asound_cards,
    };
    use cpal::traits::DeviceTrait;

    let mut devs: Vec<(String, cpal::Device)> = Vec::new();
    for dev in outputs {
        let pcm_id = match dev.name() {
            Ok(id) => id,
            Err(_) => continue,
        };
        if pcm_id == "null" || !is_useful_alsa_device(&pcm_id) {
            continue;
        }
        devs.push((pcm_id, dev));
    }

    let cards = std::fs::read_to_string("/proc/asound/cards")
        .map(|c| parse_proc_asound_cards(&c))
        .unwrap_or_default();
    let resolver = build_card_resolver(&cards);
    let canon = |pcm: &str| alsa_canonical_key(pcm, |t| resolver.get(t).copied());

    let mut all_pcms: Vec<String> = devs.iter().map(|(p, _)| p.clone()).collect();
    for pcm in in_use.keys() {
        if !all_pcms.contains(pcm) {
            all_pcms.push(pcm.clone());
        }
    }
    let groups = group_alsa_cards(&all_pcms, |t| resolver.get(t).copied());

    let mut in_use_by_card: HashMap<String, (String, u32, u32)> = HashMap::new();
    for (pcm, (sr, ch)) in in_use {
        in_use_by_card
            .entry(canon(pcm))
            .or_insert_with(|| (pcm.clone(), *sr, *ch));
    }

    let find_dev = |pcm: &str| devs.iter().find(|(p, _)| p == pcm).map(|(_, d)| d);
    let configs_for = |dev: &cpal::Device| -> (Vec<u32>, Vec<u32>) {
        let mut sample_rates = Vec::new();
        let mut channel_counts = Vec::new();
        if let Ok(configs) = dev.supported_output_configs() {
            for cfg in configs {
                let (min_rate, max_rate) = (cfg.min_sample_rate(), cfg.max_sample_rate());
                for &rate in audio::STANDARD_SAMPLE_RATES {
                    if rate >= min_rate && rate <= max_rate && !sample_rates.contains(&rate) {
                        sample_rates.push(rate);
                    }
                }
                let c = cfg.channels() as u32;
                if !channel_counts.contains(&c) {
                    channel_counts.push(c);
                }
            }
        }
        (sample_rates, channel_counts)
    };

    let kind: i32 = AudioDeviceKind::Output.into();
    let mut out = Vec::new();
    for group in groups {
        if let Some((pcm, sr, ch)) = in_use_by_card.get(&group.key) {
            out.push(AudioDeviceInfo {
                name: alsa_card_description(pcm),
                stable_id: pcm.clone(),
                kind,
                sample_rates: vec![*sr],
                channel_counts: vec![*ch],
                host_api: host_api.to_string(),
                is_default: false,
                description: alsa_card_description(pcm),
                recommended: is_recommended_pcm_id(pcm),
            });
            continue;
        }

        // Idle card: no probing for outputs — take the top-ranked
        // alias (plughw:<name> first). Drop a card with no usable
        // config ranges (matches prior behavior for dead nodes).
        let pcm = match group.candidates.first() {
            Some(c) => c.clone(),
            None => continue,
        };
        let dev = find_dev(&pcm);
        let (sample_rates, channel_counts) = dev.map(&configs_for).unwrap_or_default();
        if sample_rates.is_empty() || channel_counts.is_empty() {
            continue;
        }
        let display_name = dev
            .and_then(|d| d.description().ok())
            .map(|d| d.name().to_string())
            .unwrap_or_else(|| alsa_card_description(&pcm));
        let is_default = default_output_name == Some(display_name.as_str());

        out.push(AudioDeviceInfo {
            name: display_name,
            stable_id: pcm.clone(),
            kind,
            sample_rates,
            channel_counts,
            host_api: host_api.to_string(),
            is_default,
            description: alsa_card_description(&pcm),
            recommended: is_recommended_pcm_id(&pcm),
        });
    }
    out
}

/// `in_use_input` / `in_use_output` map each pcm_id currently held open
/// by a live capture / playback stream to its running `(sample_rate,
/// channels)`. Linux uses them to surface in-use cards from cache
/// instead of probing (input) or instead of letting a busy-device
/// `supported_*_configs()` failure drop them (output); other platforms
/// ignore them.
#[allow(deprecated)]
#[cfg_attr(not(target_os = "linux"), allow(unused_variables))]
fn enumerate_audio_devices(
    include_output: bool,
    in_use_input: &HashMap<String, (u32, u32)>,
    in_use_output: &HashMap<String, (u32, u32)>,
) -> Vec<AudioDeviceInfo> {
    use cpal::traits::{DeviceTrait, HostTrait};

    let host = cpal::default_host();
    let host_name = format!("{:?}", host.id());

    // Source the default-device identifier the same way `collect_devices`
    // keys its `is_default` comparison: IMMDevice endpoint id on
    // Windows (uniquely identifies the endpoint), `description().name()`
    // elsewhere. Issue #100.
    #[cfg(target_os = "windows")]
    let default_input_name = host.default_input_device()
        .and_then(|d| windows_device_id(&d));
    #[cfg(target_os = "windows")]
    let default_output_name = host.default_output_device()
        .and_then(|d| windows_device_id(&d));
    #[cfg(not(target_os = "windows"))]
    let default_input_name = host.default_input_device()
        .and_then(|d| d.description().ok().map(|desc| desc.name().to_string()));
    #[cfg(not(target_os = "windows"))]
    let default_output_name = host.default_output_device()
        .and_then(|d| d.description().ok().map(|desc| desc.name().to_string()));

    let mut devices = Vec::new();

    if let Ok(inputs) = host.input_devices() {
        #[cfg(target_os = "linux")]
        {
            devices.extend(collect_input_devices_linux(
                inputs,
                in_use_input,
                &host_name,
                default_input_name.as_deref(),
            ));
        }
        #[cfg(not(target_os = "linux"))]
        {
            devices.extend(collect_devices(
                inputs,
                AudioDeviceKind::Input.into(),
                default_input_name.as_deref(),
                &host_name,
                |d| d.supported_input_configs(),
            ));
        }
    }

    if include_output {
        if let Ok(outputs) = host.output_devices() {
            #[cfg(target_os = "linux")]
            {
                devices.extend(collect_output_devices_linux(
                    outputs,
                    in_use_output,
                    &host_name,
                    default_output_name.as_deref(),
                ));
            }
            #[cfg(not(target_os = "linux"))]
            {
                devices.extend(collect_devices(
                    outputs,
                    AudioDeviceKind::Output.into(),
                    default_output_name.as_deref(),
                    &host_name,
                    |d| d.supported_output_configs(),
                ));
            }
        }
    }

    devices
}

/// Open one input device, capture for `duration`, return its peak level.
/// Errors (busy device, bad config, unsupported format) come back inside
/// the `error` field rather than as a panic — the scanner reports them.
#[allow(deprecated)] // DeviceTrait::name() returns the raw pcm_id we need
fn measure_input_level(
    dev: &cpal::Device,
    pcm_id: &str,
    duration: std::time::Duration,
) -> InputDeviceLevel {
    use cpal::traits::{DeviceTrait, StreamTrait};
    use cpal::SampleFormat;
    use std::sync::atomic::{AtomicU32, Ordering};
    use std::sync::Arc;

    let err = |e: String| InputDeviceLevel {
        name: pcm_id.to_string(),
        peak_dbfs: NOISE_FLOOR_DBFS,
        has_signal: false,
        error: e,
    };

    // Negotiate the probe config the same way device detection does —
    // shared so the scanner and the detector can't drift apart.
    // default_input_config() is avoided: it can return parameters a raw
    // hw: ALSA device rejects with EINVAL.
    let (sample_format, stream_cfg) = match audio::soundcard::pick_input_probe_config(dev) {
        Ok(v) => v,
        Err(e) => return err(e),
    };

    let peak = Arc::new(AtomicU32::new(0f32.to_bits()));
    let stream = match sample_format {
        SampleFormat::F32 => {
            let pw = peak.clone();
            dev.build_input_stream(&stream_cfg,
                move |data: &[f32], _| update_peak_f32(&pw, data),
                |e| eprintln!("scan level error: {}", e), None)
        }
        SampleFormat::I16 => {
            let pw = peak.clone();
            dev.build_input_stream(&stream_cfg,
                move |data: &[i16], _| update_peak_i16(&pw, data),
                |e| eprintln!("scan level error: {}", e), None)
        }
        SampleFormat::U16 => {
            let pw = peak.clone();
            dev.build_input_stream(&stream_cfg,
                move |data: &[u16], _| update_peak_u16(&pw, data),
                |e| eprintln!("scan level error: {}", e), None)
        }
        other => return err(format!("unsupported format: {:?}", other)),
    };

    match stream {
        Ok(s) => {
            if s.play().is_ok() {
                std::thread::sleep(duration);
            }
            drop(s);
            let peak_lin = f32::from_bits(peak.load(Ordering::Relaxed));
            let peak_db = if peak_lin > 0.0 {
                20.0 * peak_lin.log10()
            } else {
                NOISE_FLOOR_DBFS
            };
            InputDeviceLevel {
                name: pcm_id.to_string(),
                peak_dbfs: peak_db,
                has_signal: peak_db > SIGNAL_THRESHOLD_DBFS,
                error: String::new(),
            }
        }
        Err(e) => err(format!("{}", e)),
    }
}

/// Measure peak input level on each capture device.
///
/// `in_use` lists pcm_ids held open by a running channel. On Linux the
/// list is collapsed to one row per physical card (same canonicalization
/// as device detection), and an in-use card reports a clear "busy"
/// status instead of cpal's misleading "device no longer available" —
/// you cannot independently scan a device a running channel holds; its
/// live level shows on the device card.
#[allow(deprecated)] // DeviceTrait::name() returns the raw pcm_id we need
#[cfg_attr(not(target_os = "linux"), allow(unused_variables))]
fn scan_input_levels(
    duration_ms: u32,
    in_use: &HashMap<String, (u32, u32)>,
) -> Vec<InputDeviceLevel> {
    use cpal::traits::{DeviceTrait, HostTrait};

    let host = cpal::default_host();
    let inputs = match host.input_devices() {
        Ok(i) => i.collect::<Vec<_>>(),
        Err(_) => return Vec::new(),
    };
    let duration = std::time::Duration::from_millis(duration_ms as u64);

    let mut devs: Vec<(String, cpal::Device)> = Vec::new();
    for dev in inputs {
        let pcm_id = match dev.name() {
            Ok(id) => id,
            Err(_) => continue,
        };
        // Skip virtual/plugin ALSA devices that can poison the ALSA
        // backend when their PCM open fails.
        if pcm_id == "null" || !is_useful_alsa_device(&pcm_id) {
            continue;
        }
        devs.push((pcm_id, dev));
    }

    let mut results: Vec<InputDeviceLevel> = Vec::new();

    #[cfg(target_os = "linux")]
    {
        use crate::audio::soundcard::{
            alsa_canonical_key, build_card_resolver, group_alsa_cards, parse_proc_asound_cards,
        };
        let cards = std::fs::read_to_string("/proc/asound/cards")
            .map(|c| parse_proc_asound_cards(&c))
            .unwrap_or_default();
        let resolver = build_card_resolver(&cards);
        let canon = |pcm: &str| alsa_canonical_key(pcm, |t| resolver.get(t).copied());

        let mut all_pcms: Vec<String> = devs.iter().map(|(p, _)| p.clone()).collect();
        for pcm in in_use.keys() {
            if !all_pcms.contains(pcm) {
                all_pcms.push(pcm.clone());
            }
        }
        let groups = group_alsa_cards(&all_pcms, |t| resolver.get(t).copied());

        // canonical card key -> the in-use pcm_id on that card
        let mut in_use_by_card: HashMap<String, String> = HashMap::new();
        for pcm in in_use.keys() {
            in_use_by_card.entry(canon(pcm)).or_insert_with(|| pcm.clone());
        }
        let find_dev = |pcm: &str| devs.iter().find(|(p, _)| p == pcm).map(|(_, d)| d);

        for group in groups {
            if let Some(pcm) = in_use_by_card.get(&group.key) {
                results.push(InputDeviceLevel {
                    name: pcm.clone(),
                    peak_dbfs: NOISE_FLOOR_DBFS,
                    has_signal: false,
                    error: "in use by a running channel — live level is shown on the device card"
                        .to_string(),
                });
                continue;
            }
            // Idle card: measure its top-ranked alias once (one row per
            // physical card, not per hw:/plughw: alias).
            let mut chosen: Option<String> = None;
            for cand in &group.candidates {
                if find_dev(cand).is_some() {
                    chosen = Some(cand.clone());
                    break;
                }
            }
            let pcm = match chosen {
                Some(c) => c,
                None => continue,
            };
            if let Some(dev) = find_dev(&pcm) {
                results.push(measure_input_level(dev, &pcm, duration));
            }
        }
    }

    #[cfg(not(target_os = "linux"))]
    {
        for (pcm_id, dev) in &devs {
            results.push(measure_input_level(dev, pcm_id, duration));
        }
    }

    results
}

// Peak-tracking callbacks for each sample format.
fn update_peak_f32(peak: &std::sync::Arc<std::sync::atomic::AtomicU32>, data: &[f32]) {
    let mut local_peak: f32 = 0.0;
    for &s in data {
        let abs = s.abs();
        if abs > local_peak { local_peak = abs; }
    }
    atomic_max_f32(peak, local_peak);
}

fn update_peak_i16(peak: &std::sync::Arc<std::sync::atomic::AtomicU32>, data: &[i16]) {
    let mut local_peak: f32 = 0.0;
    for &s in data {
        let v = (s as f32 / 32768.0).abs();
        if v > local_peak { local_peak = v; }
    }
    atomic_max_f32(peak, local_peak);
}

fn update_peak_u16(peak: &std::sync::Arc<std::sync::atomic::AtomicU32>, data: &[u16]) {
    let mut local_peak: f32 = 0.0;
    for &s in data {
        let v = ((s as f32 - 32768.0) / 32768.0).abs();
        if v > local_peak { local_peak = v; }
    }
    atomic_max_f32(peak, local_peak);
}

fn atomic_max_f32(atom: &std::sync::atomic::AtomicU32, val: f32) {
    use std::sync::atomic::Ordering;
    loop {
        let old_bits = atom.load(Ordering::Relaxed);
        let old = f32::from_bits(old_bits);
        if val <= old { break; }
        if atom.compare_exchange_weak(
            old_bits, val.to_bits(),
            Ordering::Relaxed, Ordering::Relaxed,
        ).is_ok() { break; }
    }
}

fn parse_channel(c: &ConfigureChannel) -> ChannelConfig {
    let profile = match c.profile.as_str() {
        "B" | "b" => AfskProfile::B,
        _ => AfskProfile::A,
    };
    let fix_bits = match c.fix_bits.as_str() {
        "single" => RetryType::InvertSingle,
        "double" => RetryType::InvertDouble,
        _ => RetryType::None,
    };
    // Prefer new split fields; fall back to legacy device_id/audio_channel
    let input_device_id = if c.input_device_id != 0 { c.input_device_id } else { c.device_id };
    let input_channel = if c.input_device_id != 0 { c.input_channel } else { c.audio_channel };
    ChannelConfig {
        channel: c.channel,
        input_device_id,
        input_channel,
        output_device_id: c.output_device_id,
        output_channel: c.output_channel,
        baud: if c.baud == 0 { 1200 } else { c.baud },
        mark_freq: if c.mark_freq == 0 { 1200 } else { c.mark_freq },
        space_freq: if c.space_freq == 0 { 2200 } else { c.space_freq },
        modem_type: if c.modem_type.is_empty() { "afsk".into() } else { c.modem_type.clone() },
        profile,
        num_slicers: c.num_slicers.max(1) as usize,
        fix_bits,
        num_decoders: c.num_decoders.max(1),
        decoder_offset: c.decoder_offset,
        fx25_encode: c.fx25_encode,
        il2p_encode: c.il2p_encode,
        demod_ensemble: c.demod_ensemble.clone(),
    }
}

fn build_received(f: &DecodedFrame) -> ReceivedFrame {
    ReceivedFrame {
        channel: f.chan as u32,
        subchan: f.subchan as u32,
        slice: f.slice as u32,
        data: f.data.clone(),
        quality: f.quality,
        audio_level_mark: f.audio_level_mark,
        audio_level_space: f.audio_level_space,
        speed_error: f.speed_error,
        retry: match f.retry {
            RetryType::None => "none".into(),
            RetryType::InvertSingle => "single".into(),
            RetryType::InvertDouble => "double".into(),
            RetryType::InvertTriple => "triple".into(),
            RetryType::InvertTwoSep => "two_sep".into(),
        },
        timestamp_ns: now_ns(),
    }
}

fn now_ns() -> u64 {
    SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .map(|d| d.as_nanos() as u64)
        .unwrap_or(0)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ipc::proto::TransmitFrame;
    use crate::ipc::server::IpcHandle;


    #[test]
    fn transmit_frame_for_unknown_channel_returns_without_panicking() {
        let (handle, _peer) = IpcHandle::test_pair();
        let (_ipc_tx, ipc_rx) = std::sync::mpsc::channel::<IpcInbound>();
        let mut modem = Modem::new(handle, ipc_rx).expect("Modem::new");
        let msg = IpcMessage {
            payload: Some(Payload::TransmitFrame(TransmitFrame {
                channel: 99,
                data: vec![0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0x60, 0x03, 0xf0, b'x'],
                txdelay_override_ms: 0,
                txtail_override_ms: 0,
                priority: 0,
                frame_id: 0,
            })),
        };
        let should_exit = modem.handle_ipc(msg);
        assert!(!should_exit);
    }

    #[test]
    fn transmit_frame_with_channel_but_no_audio_config_returns_without_panicking() {
        let (handle, _peer) = IpcHandle::test_pair();
        let (_ipc_tx, ipc_rx) = std::sync::mpsc::channel::<IpcInbound>();
        let mut modem = Modem::new(handle, ipc_rx).expect("Modem::new");
        modem.channel_configs.insert(
            0,
            ChannelConfig {
                channel: 0,
                output_device_id: 7,
                output_channel: 0,
                ..ChannelConfig::default()
            },
        );
        let msg = IpcMessage {
            payload: Some(Payload::TransmitFrame(TransmitFrame {
                channel: 0,
                data: vec![0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0x60, 0x03, 0xf0, b'x'],
                txdelay_override_ms: 0,
                txtail_override_ms: 0,
                priority: 0,
                frame_id: 0,
            })),
        };
        let should_exit = modem.handle_ipc(msg);
        assert!(!should_exit);
    }

    #[test]
    fn effective_ms_prefers_override_then_config_then_default() {
        assert_eq!(effective_ms(250, Some(500), 100), 250);
        assert_eq!(effective_ms(0, Some(500), 100), 500);
        assert_eq!(effective_ms(0, Some(0), 100), 100);
        assert_eq!(effective_ms(0, None, 100), 100);
    }

    #[test]
    fn ptt_uses_vox_detects_desktop_and_android_vox_only() {
        let cfg = |method: &str, ptt_method: u32| ConfigurePtt {
            channel: 0,
            method: method.into(),
            device: String::new(),
            txdelay_ms: 0,
            txtail_ms: 0,
            slottime_ms: 0,
            persist: 0,
            dwait_ms: 0,
            invert: false,
            gpio_pin: 0,
            gpio_line: 0,
            ptt_method,
        };
        // Desktop vox method → VOX.
        assert!(ptt_uses_vox(&cfg("vox", 0)));
        // Android USB-PTT with the VOX transport (ptt_method 4) → VOX.
        assert!(ptt_uses_vox(&cfg("android", 4)));
        // Android with a wired transport (RTS/CM108/DTR) is NOT VOX.
        assert!(!ptt_uses_vox(&cfg("android", 1)));
        assert!(!ptt_uses_vox(&cfg("android", 2)));
        assert!(!ptt_uses_vox(&cfg("android", 3)));
        // Plain "none" is a VOX rig with no lead tone, so it is NOT
        // treated as VOX for the lead-in purpose.
        assert!(!ptt_uses_vox(&cfg("none", 0)));
        assert!(!ptt_uses_vox(&cfg("serial_rts", 0)));
    }

    #[test]
    fn vox_lead_in_prepends_a_500ms_audible_tone_only_for_vox() {
        let cfg = |method: &str, ptt_method: u32| ConfigurePtt {
            channel: 0,
            method: method.into(),
            device: String::new(),
            txdelay_ms: 0,
            txtail_ms: 0,
            slottime_ms: 0,
            persist: 0,
            dwait_ms: 0,
            invert: false,
            gpio_pin: 0,
            gpio_line: 0,
            ptt_method,
        };
        let sample_rate = 48_000u32;
        let expected_len =
            (sample_rate as u64 * VOX_LEAD_TONE_MS as u64 / 1000) as usize;

        // Desktop vox and Android VOX both get a lead-in of exactly
        // VOX_LEAD_TONE_MS worth of samples...
        for c in [cfg("vox", 0), cfg("android", 4)] {
            let lead = vox_lead_in(Some(&c), sample_rate, 1200)
                .expect("vox channel must get a lead-in tone");
            assert_eq!(
                lead.len(),
                expected_len,
                "lead-in must be {} ms at {} Hz",
                VOX_LEAD_TONE_MS,
                sample_rate
            );
            // ...and it must be actual audio, not silence — a silent
            // lead-in would never key the VOX relay.
            assert!(
                lead.iter().any(|&s| s != 0),
                "lead-in tone must be audible"
            );
        }

        // Wired / none / no-config channels get no lead-in (None), so
        // handle_transmit_frame leaves their TX buffer untouched.
        assert!(vox_lead_in(Some(&cfg("none", 0)), sample_rate, 1200).is_none());
        assert!(vox_lead_in(Some(&cfg("serial_rts", 0)), sample_rate, 1200).is_none());
        assert!(vox_lead_in(Some(&cfg("android", 1)), sample_rate, 1200).is_none());
        assert!(vox_lead_in(None, sample_rate, 1200).is_none());
    }

    #[test]
    fn digirig_tone_helpers_only_fire_for_the_digirig_tone_method() {
        let cfg = |method: &str| ConfigurePtt {
            channel: 0,
            method: method.into(),
            device: String::new(),
            txdelay_ms: 0,
            txtail_ms: 0,
            slottime_ms: 0,
            persist: 0,
            dwait_ms: 0,
            invert: false,
            gpio_pin: 0,
            gpio_line: 0,
            ptt_method: 0,
        };
        let sample_rate = 48_000u32;
        let expected_len = (sample_rate as u64 * DIGIRIG_TONE_LEAD_MS as u64 / 1000) as usize;

        // digirig_tone → companion-channel tone at mark_freq + a silent
        // lead-in of exactly DIGIRIG_TONE_LEAD_MS.
        assert!(ptt_uses_digirig_tone(&cfg("digirig_tone")));
        assert_eq!(digirig_tone_hz(Some(&cfg("digirig_tone")), 1200), 1200);
        let lead = digirig_tone_lead_in(Some(&cfg("digirig_tone")), sample_rate)
            .expect("digirig_tone channel must get a lead-in");
        assert_eq!(lead.len(), expected_len);
        // The AFSK-channel lead-in is SILENT (the keying tone rides the
        // companion channel, emitted by the sink) — unlike VOX, whose
        // lead-in is the tone itself.
        assert!(
            lead.iter().all(|&s| s == 0),
            "digirig lead-in must be silent on the AFSK channel"
        );

        // Every other method: no tone, no lead-in.
        for m in ["none", "vox", "serial_rts", "cm108", "android"] {
            assert!(!ptt_uses_digirig_tone(&cfg(m)), "{} is not digirig_tone", m);
            assert_eq!(digirig_tone_hz(Some(&cfg(m)), 1200), 0, "{} tone hz", m);
            assert!(digirig_tone_lead_in(Some(&cfg(m)), sample_rate).is_none(), "{} lead", m);
        }
        assert_eq!(digirig_tone_hz(None, 1200), 0);
        assert!(digirig_tone_lead_in(None, sample_rate).is_none());
    }

    #[test]
    fn configure_ptt_with_method_vox_registers_a_no_op_driver_on_the_worker() {
        let (handle, _peer) = IpcHandle::test_pair();
        let (_ipc_tx, ipc_rx) = std::sync::mpsc::channel::<IpcInbound>();
        let mut modem = Modem::new(handle, ipc_rx).expect("Modem::new");
        let msg = IpcMessage {
            payload: Some(Payload::ConfigurePtt(ConfigurePtt {
                channel: 5,
                method: "vox".into(),
                device: String::new(),
                txdelay_ms: 300,
                txtail_ms: 100,
                slottime_ms: 10,
                persist: 63,
                dwait_ms: 0,
                invert: false,
                gpio_pin: 0,
                gpio_line: 0,
                ptt_method: 0,
            })),
        };
        assert!(!modem.handle_ipc(msg));
        let stored = modem.ptt_cfgs.get(&5).expect("PttConfig stored");
        assert_eq!(stored.method, "vox");
        // vox builds a no-op driver just like none — verify it actually
        // landed on the worker rather than failing the build.
        assert_eq!(
            modem.tx_worker.driver_count(),
            1,
            "worker should have one no-op VOX driver registered"
        );
    }

    #[test]
    fn configure_ptt_with_method_none_registers_a_no_op_driver_on_the_worker() {
        let (handle, _peer) = IpcHandle::test_pair();
        let (_ipc_tx, ipc_rx) = std::sync::mpsc::channel::<IpcInbound>();
        let mut modem = Modem::new(handle, ipc_rx).expect("Modem::new");
        let msg = IpcMessage {
            payload: Some(Payload::ConfigurePtt(ConfigurePtt {
                channel: 3,
                method: "none".into(),
                device: String::new(),
                txdelay_ms: 250,
                txtail_ms: 80,
                slottime_ms: 10,
                persist: 63,
                dwait_ms: 0,
                invert: false,
                gpio_pin: 3,
                gpio_line: 0,
                ptt_method: 0,
            })),
        };
        assert!(!modem.handle_ipc(msg));
        let stored = modem.ptt_cfgs.get(&3).expect("PttConfig stored");
        assert_eq!(stored.method, "none");
        assert_eq!(stored.txdelay_ms, 250);
        assert_eq!(stored.txtail_ms, 80);
        // Verify the driver actually landed on the worker thread —
        // previously we only asserted the local `ptt_cfgs` map, which
        // would have passed even if `register_driver` silently no-opped.
        assert_eq!(
            modem.tx_worker.driver_count(),
            1,
            "worker should have one NonePtt driver registered"
        );
    }

    #[test]
    fn configure_ptt_with_serial_rts_and_empty_device_stores_config_without_registering_driver() {
        // A misconfigured serial PTT should fail loudly (driver build
        // returns Err) but must still persist the ConfigurePtt so that
        // txdelay_ms / txtail_ms flow through to the next
        // `TransmitFrame`. The next TX attempt on this channel will
        // then log a missing-driver error, which is the observable
        // behaviour the user needs to debug their config.
        let (handle, _peer) = IpcHandle::test_pair();
        let (_ipc_tx, ipc_rx) = std::sync::mpsc::channel::<IpcInbound>();
        let mut modem = Modem::new(handle, ipc_rx).expect("Modem::new");
        let msg = IpcMessage {
            payload: Some(Payload::ConfigurePtt(ConfigurePtt {
                channel: 0,
                method: "serial_rts".into(),
                device: String::new(),
                txdelay_ms: 300,
                txtail_ms: 100,
                slottime_ms: 10,
                persist: 63,
                dwait_ms: 0,
                invert: false,
                gpio_pin: 3,
                gpio_line: 0,
                ptt_method: 0,
            })),
        };
        assert!(!modem.handle_ipc(msg));
        let stored = modem.ptt_cfgs.get(&0).expect("PttConfig stored");
        assert_eq!(stored.method, "serial_rts");
        assert_eq!(
            modem.tx_worker.driver_count(),
            0,
            "driver build failed → worker must have no driver registered"
        );
    }

    #[test]
    fn configure_ptt_with_unknown_method_leaves_worker_without_a_driver() {
        // A typo in the method string ("serial-rts" with a dash
        // instead of an underscore) must surface as an explicit error
        // at build_driver time, not silently map to NonePtt. The
        // observable behaviour is: config stored, no driver on the
        // worker, next TX attempt logs "no PTT driver registered".
        let (handle, _peer) = IpcHandle::test_pair();
        let (_ipc_tx, ipc_rx) = std::sync::mpsc::channel::<IpcInbound>();
        let mut modem = Modem::new(handle, ipc_rx).expect("Modem::new");
        let msg = IpcMessage {
            payload: Some(Payload::ConfigurePtt(ConfigurePtt {
                channel: 2,
                method: "serial-rts".into(),
                device: "/dev/null".into(),
                txdelay_ms: 300,
                txtail_ms: 100,
                slottime_ms: 10,
                persist: 63,
                dwait_ms: 0,
                invert: false,
                gpio_pin: 3,
                gpio_line: 0,
                ptt_method: 0,
            })),
        };
        assert!(!modem.handle_ipc(msg));
        let stored = modem.ptt_cfgs.get(&2).expect("PttConfig stored");
        assert_eq!(stored.method, "serial-rts");
        assert_eq!(
            modem.tx_worker.driver_count(),
            0,
            "unknown method should not silently register a NonePtt driver"
        );
    }

    #[test]
    fn emit_status_fans_out_one_message_per_configured_channel() {
        // Pins the multi-channel attribution fix: before, all counters
        // were summed onto the first configured channel. Now emit_status
        // must produce exactly one StatusUpdate per configured channel,
        // each carrying that channel's own counters, with the shutdown
        // flag riding only on the last message when final_ is true.
        use crate::ipc::framing::read_frame;

        let (handle, mut peer) = IpcHandle::test_pair();
        peer.set_read_timeout(Some(std::time::Duration::from_secs(1)))
            .expect("set_read_timeout");
        let (_ipc_tx, ipc_rx) = std::sync::mpsc::channel::<IpcInbound>();
        let mut modem = Modem::new(handle, ipc_rx).expect("Modem::new");

        modem.channel_configs.insert(
            1,
            ChannelConfig { channel: 1, ..ChannelConfig::default() },
        );
        modem.channel_configs.insert(
            2,
            ChannelConfig { channel: 2, ..ChannelConfig::default() },
        );

        modem.rx_frames.insert(1, 10);
        modem.rx_frames.insert(2, 20);
        modem.tx_frames.insert(1, 3);
        modem.tx_frames.insert(2, 7);
        modem.dcd_transitions.insert(1, 5);
        modem.dcd_transitions.insert(2, 9);
        // Stale counter for a channel no longer in config — the prune in
        // emit_status should drop it before sending.
        modem.rx_frames.insert(99, 42);

        modem.emit_status(true);

        let mut statuses = Vec::new();
        while let Ok(Some(msg)) = read_frame(&mut peer) {
            if let Some(Payload::StatusUpdate(s)) = msg.payload {
                statuses.push(s);
                if statuses.len() == 2 {
                    break;
                }
            }
        }

        assert_eq!(statuses.len(), 2, "expected one StatusUpdate per channel");

        let s1 = statuses.iter().find(|s| s.channel == 1).expect("channel 1 status");
        assert_eq!(s1.rx_frames, 10);
        assert_eq!(s1.tx_frames, 3);
        assert_eq!(s1.dcd_transitions, 5);
        assert!(!s1.shutdown_complete, "shutdown_complete must only ride on the last channel");

        let s2 = statuses.iter().find(|s| s.channel == 2).expect("channel 2 status");
        assert_eq!(s2.rx_frames, 20);
        assert_eq!(s2.tx_frames, 7);
        assert_eq!(s2.dcd_transitions, 9);
        assert!(s2.shutdown_complete, "final=true + sorted channels: last message carries the flag");

        assert!(!modem.rx_frames.contains_key(&99), "stale counter for removed channel must be pruned");
    }
}
