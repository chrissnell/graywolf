//! Top-level modem orchestration: glues audio sources, demodulators, and
//! the IPC server into a single process. Consumed by `bin/graywolf_modem.rs`.
//!
//! Supports multiple audio devices, multiple channels per device, and
//! multiple demodulator types (AFSK, PSK, 9600).

mod tx_worker;

use std::collections::HashMap;
use std::sync::atomic::{AtomicU32, Ordering};
use std::sync::mpsc::{sync_channel, Receiver, RecvTimeoutError, SyncSender};
use std::sync::Arc;
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use crate::audio::{self, AudioChunk, AudioSource, CHUNK_QUEUE_DEPTH};
use crate::demod_afsk::AfskDemodulator;
use crate::hdlc::DecodedFrame;
use crate::ipc::proto::{
    ipc_message::Payload, AudioDeviceInfo, AudioDeviceKind, AudioDeviceList, ConfigureChannel,
    ConfigurePtt, DcdChange, DeviceLevelUpdate, EnumerateAudioDevices, InputDeviceLevel,
    InputLevelScanResult, IpcMessage, ReceivedFrame, ScanInputLevels, StatusUpdate, TestToneResult,
    TransmitFrame,
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

    // Shared cache of open PTT hardware (serial fds today, HID handles
    // and gpiochip handles in later phases). One port is opened at most
    // once per device path; see `src/tx/ptt.rs` for why.
    ptt_registry: PortRegistry,

    // Live gain atomics, keyed by device_id (shared with DevicePipeline)
    gain_atoms: HashMap<u32, Arc<AtomicU32>>,

    // Active audio pipelines, keyed by device_id
    active_devices: HashMap<u32, DevicePipeline>,

    // Dedicated TX worker: owns every output sink and every PTT driver,
    // serializes TX across all channels, and keeps audio drain off the
    // IPC thread.
    tx_worker: tx_worker::TxWorker,

    // Metrics
    rx_frames: u64,
    tx_frames: u64,
    rx_bad_fcs: u64,
    dcd_transitions: u64,
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
            ptt_registry: PortRegistry::new(),
            gain_atoms: HashMap::new(),
            active_devices: HashMap::new(),
            tx_worker,
            rx_frames: 0,
            tx_frames: 0,
            rx_bad_fcs: 0,
            dcd_transitions: 0,
            last_status_tx: Instant::now(),
            last_level_tx: HashMap::new(),
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
                if let Err(e) = self.start_audio() {
                    eprintln!("graywolf-modem: start_audio failed: {}", e);
                }
            }
            Some(Payload::StopAudio(_)) => {
                self.stop_all_audio();
            }
            Some(Payload::EnumerateAudioDevices(req)) => {
                self.handle_enumerate_devices(req);
            }
            Some(Payload::PlayTestTone(req)) => {
                self.handle_play_test_tone(req);
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
            Some(Payload::Shutdown(_)) => {
                self.graceful_shutdown();
                return true;
            }
            Some(Payload::ReceivedFrame(_))
            | Some(Payload::DcdChange(_))
            | Some(Payload::StatusUpdate(_))
            | Some(Payload::ModemReady(_))
            | Some(Payload::AudioDeviceList(_))
            | Some(Payload::TestToneResult(_))
            | Some(Payload::DeviceLevelUpdate(_))
            | Some(Payload::InputLevelScanResult(_)) => {
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

        // Resolve output devices BEFORE opening any input streams.
        // On Linux/ALSA, cpal's output device enumeration can fail once
        // a capture stream holds the hardware device exclusively. By
        // resolving here we grab the cpal Device handle while the
        // hardware is still idle, then hand it to the TX worker.
        let mut seen_outputs = std::collections::HashSet::new();
        for ccfg in self.channel_configs.values() {
            let dev_id = ccfg.output_device_id;
            if dev_id == 0 || !seen_outputs.insert(dev_id) {
                continue;
            }
            if let Some(acfg) = self.audio_configs.get(&dev_id) {
                match audio::soundcard::resolve_output_device(&acfg.device_name) {
                    Ok(device) => {
                        self.tx_worker.prepare_output(dev_id, device);
                        eprintln!(
                            "graywolf-modem: pre-resolved output device_id={} ({})",
                            dev_id, acfg.device_name
                        );
                    }
                    Err(e) => {
                        eprintln!(
                            "graywolf-modem: failed to pre-resolve output device_id={} ({}): {}",
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
        for (_, pipe) in self.active_devices.drain() {
            pipe.source.stop();
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

                            for f in all_frames {
                                self.rx_frames += 1;
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
                                self.dcd_transitions += 1;
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
        let devices = enumerate_audio_devices(req.include_output);
        let msg = IpcMessage {
            payload: Some(Payload::AudioDeviceList(AudioDeviceList {
                request_id: req.request_id,
                devices,
            })),
        };
        if let Err(e) = self.handle.send(&msg) {
            eprintln!("graywolf-modem: send AudioDeviceList failed: {}", e);
        }
    }

    fn handle_scan_input_levels(&self, req: ScanInputLevels) {
        let handle = self.handle.clone();
        std::thread::Builder::new()
            .name("scan-input-levels".into())
            .spawn(move || {
                let duration_ms = if req.duration_ms == 0 { 500 } else { req.duration_ms };
                let results = scan_input_levels(duration_ms);
                let msg = IpcMessage::input_level_scan_result(InputLevelScanResult {
                    request_id: req.request_id,
                    devices: results,
                });
                let _ = handle.send(&msg);
            })
            .ok();
    }

    fn handle_play_test_tone(&self, req: crate::ipc::proto::PlayTestTone) {
        let device_id = req.device_id;
        let gain_atom = self.gain_atoms
            .get(&device_id)
            .cloned()
            .unwrap_or_else(|| Arc::new(AtomicU32::new(0f32.to_bits())));
        let handle = self.handle.clone();
        std::thread::Builder::new()
            .name("test-tone".into())
            .spawn(move || {
                let result = play_test_tone_blocking(&req, &handle, device_id, &gain_atom);
                let msg = IpcMessage::test_tone_result(result);
                let _ = handle.send(&msg);
            })
            .ok();
    }

    fn emit_status(&mut self, final_: bool) {
        // Emit status for the first configured channel (or channel 0)
        let (mark, space, peak, dcd_state, channel) =
            if let Some(pipe) = self.active_devices.values().next() {
                if let Some(cp) = pipe.channels.first() {
                    (cp.latest_mark, cp.latest_space, cp.latest_peak,
                     cp.prev_dcd_any, cp.channel_id)
                } else {
                    (0.0, 0.0, 0.0, false, 0)
                }
            } else {
                (0.0, 0.0, 0.0, false,
                 self.channel_configs.keys().next().copied().unwrap_or(0))
            };

        let s = StatusUpdate {
            channel,
            rx_frames: self.rx_frames,
            rx_bad_fcs: self.rx_bad_fcs,
            tx_frames: self.tx_frames,
            dcd_transitions: self.dcd_transitions,
            audio_level_mark: mark,
            audio_level_space: space,
            audio_level_peak: peak,
            dcd_state,
            shutdown_complete: final_,
            timestamp_ns: now_ns(),
        };
        let _ = self.handle.send(&IpcMessage::status_update(s));
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
        match self.ptt_registry.build_driver(&cfg) {
            Ok(driver) => {
                if let Err(e) = self.tx_worker.register_driver(channel, driver) {
                    eprintln!(
                        "graywolf-modem: ConfigurePtt: register driver for channel {}: {}",
                        channel, e
                    );
                } else {
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

        let samples =
            match crate::tx::build_samples(&tf.data, txdelay_ms, txtail_ms, acfg.sample_rate) {
                Ok(s) => s,
                Err(e) => {
                    eprintln!("graywolf-modem: TransmitFrame: build_samples failed: {}", e);
                    return;
                }
            };

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
            },
        };

        if let Err(e) = self.tx_worker.transmit(job) {
            eprintln!("graywolf-modem: TransmitFrame: {}", e);
            return;
        }
        self.tx_frames += 1;
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
            // Default: AFSK
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

fn process_demod_sample(demod: &mut ChannelDemod, sample: i32) {
    match demod {
        ChannelDemod::Afsk(d) => d.process_sample(sample),
        ChannelDemod::Psk(d) => d.process_sample(sample),
        ChannelDemod::Baseband9600(d) => d.process_sample(sample),
    }
}

fn take_demod_frames(demod: &mut ChannelDemod) -> Vec<DecodedFrame> {
    match demod {
        ChannelDemod::Afsk(d) => d.take_frames(),
        ChannelDemod::Psk(d) => d.take_frames(),
        ChannelDemod::Baseband9600(d) => d.take_frames(),
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

fn enumerate_audio_devices(include_output: bool) -> Vec<AudioDeviceInfo> {
    use cpal::traits::{DeviceTrait, HostTrait};

    let mut devices = Vec::new();
    let host = cpal::default_host();
    let host_name = format!("{:?}", host.id());

    let default_input_name = host.default_input_device()
        .and_then(|d| d.name().ok());
    let default_output_name = host.default_output_device()
        .and_then(|d| d.name().ok());

    // Input devices
    if let Ok(inputs) = host.input_devices() {
        for dev in inputs {
            if let Ok(name) = dev.name() {
                let mut sample_rates = Vec::new();
                let mut channel_counts = Vec::new();

                if let Ok(configs) = dev.supported_input_configs() {
                    for cfg in configs {
                        let min_rate = cfg.min_sample_rate().0;
                        let max_rate = cfg.max_sample_rate().0;
                        for &rate in &[8000, 11025, 16000, 22050, 44100, 48000, 96000] {
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

                let is_default = default_input_name.as_deref() == Some(&name);
                let description = alsa_card_description(&name);

                devices.push(AudioDeviceInfo {
                    name: name.clone(),
                    stable_id: name.clone(),
                    kind: AudioDeviceKind::Input.into(),
                    sample_rates,
                    channel_counts,
                    host_api: host_name.clone(),
                    is_default,
                    description,
                });
            }
        }
    }

    // Output devices
    if include_output {
        if let Ok(outputs) = host.output_devices() {
            for dev in outputs {
                if let Ok(name) = dev.name() {
                    let mut sample_rates = Vec::new();
                    let mut channel_counts = Vec::new();

                    if let Ok(configs) = dev.supported_output_configs() {
                        for cfg in configs {
                            let min_rate = cfg.min_sample_rate().0;
                            let max_rate = cfg.max_sample_rate().0;
                            for &rate in &[8000, 11025, 16000, 22050, 44100, 48000, 96000] {
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

                    let is_default = default_output_name.as_deref() == Some(&name);
                    let description = alsa_card_description(&name);

                    devices.push(AudioDeviceInfo {
                        name: name.clone(),
                        stable_id: name.clone(),
                        kind: AudioDeviceKind::Output.into(),
                        sample_rates,
                        channel_counts,
                        host_api: host_name.clone(),
                        is_default,
                        description,
                    });
                }
            }
        }
    }

    devices
}

/// Briefly open each available input device and measure peak level.
fn scan_input_levels(duration_ms: u32) -> Vec<InputDeviceLevel> {
    use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
    use cpal::SampleFormat;
    use std::sync::atomic::{AtomicU32, Ordering};
    use std::sync::Arc;

    let host = cpal::default_host();
    let inputs = match host.input_devices() {
        Ok(i) => i.collect::<Vec<_>>(),
        Err(_) => return Vec::new(),
    };

    let duration = std::time::Duration::from_millis(duration_ms as u64);
    let mut results = Vec::new();

    for dev in inputs {
        let name = match dev.name() {
            Ok(n) => n,
            Err(_) => continue,
        };
        let supported = match dev.default_input_config() {
            Ok(c) => c,
            Err(e) => {
                results.push(InputDeviceLevel {
                    name,
                    peak_dbfs: -96.0,
                    has_signal: false,
                    error: format!("{}", e),
                });
                continue;
            }
        };

        let peak = Arc::new(AtomicU32::new(0f32.to_bits()));
        let sample_format = supported.sample_format();

        let stream_cfg = cpal::StreamConfig {
            channels: supported.channels(),
            sample_rate: supported.sample_rate(),
            buffer_size: cpal::BufferSize::Default,
        };

        // Build stream using the device's native sample format.
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
            _ => {
                results.push(InputDeviceLevel {
                    name,
                    peak_dbfs: -96.0,
                    has_signal: false,
                    error: format!("unsupported format: {:?}", sample_format),
                });
                continue;
            }
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
                    -96.0
                };

                results.push(InputDeviceLevel {
                    name,
                    peak_dbfs: peak_db,
                    has_signal: peak_db > -40.0,
                    error: String::new(),
                });
            }
            Err(e) => {
                results.push(InputDeviceLevel {
                    name,
                    peak_dbfs: -96.0,
                    has_signal: false,
                    error: format!("{}", e),
                });
            }
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

fn play_test_tone_blocking(
    req: &crate::ipc::proto::PlayTestTone,
    handle: &IpcHandle,
    device_id: u32,
    gain_atom: &Arc<AtomicU32>,
) -> TestToneResult {
    use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
    use std::sync::atomic::{AtomicBool, AtomicU64};

    let host = cpal::default_host();
    let device = match host.output_devices() {
        Ok(mut devs) => devs.find(|d| d.name().map(|n| n == req.device_name).unwrap_or(false)),
        Err(e) => {
            return TestToneResult {
                request_id: req.request_id,
                success: false,
                error: format!("enumerate output devices: {}", e),
            };
        }
    };
    let device = match device {
        Some(d) => d,
        None => {
            return TestToneResult {
                request_id: req.request_id,
                success: false,
                error: format!("output device not found: {}", req.device_name),
            };
        }
    };

    let sample_rate = if req.sample_rate > 0 { req.sample_rate } else { 48000 };
    let channels = req.channels.max(1) as u16;
    let config = cpal::StreamConfig {
        channels,
        sample_rate: cpal::SampleRate(sample_rate),
        buffer_size: cpal::BufferSize::Default,
    };

    let tone_freq = 1000.0_f64;
    let amplitude = 0.5_f64; // -6 dBFS
    let duration_samples = (sample_rate as u64) * 3 / 2; // 1.5 seconds
    let phase_inc = tone_freq * 2.0 * std::f64::consts::PI / sample_rate as f64;

    let phase_counter = Arc::new(AtomicU64::new(0));
    let done = Arc::new(AtomicBool::new(false));

    let pc = phase_counter.clone();
    let d = done.clone();
    let ch = channels as usize;
    let dur = duration_samples;
    let ga = gain_atom.clone();

    let stream = match device.build_output_stream(
        &config,
        move |data: &mut [f32], _: &cpal::OutputCallbackInfo| {
            let gain_db = f32::from_bits(ga.load(Ordering::Relaxed));
            let gain_linear = if gain_db.abs() > f32::EPSILON {
                10f32.powf(gain_db / 20.0)
            } else {
                1.0
            };
            for frame in data.chunks_mut(ch) {
                let sample_idx = pc.fetch_add(1, Ordering::Relaxed);
                let raw = if sample_idx < dur {
                    (sample_idx as f64 * phase_inc).sin() * amplitude
                } else {
                    d.store(true, Ordering::Relaxed);
                    0.0
                };
                let gained = (raw as f32) * gain_linear;
                let out = gained.clamp(-1.0, 1.0);
                for s in frame.iter_mut() {
                    *s = out;
                }
            }
        },
        move |e| eprintln!("graywolf-modem: test tone stream error: {}", e),
        None,
    ) {
        Ok(s) => s,
        Err(e) => {
            return TestToneResult {
                request_id: req.request_id,
                success: false,
                error: format!("build output stream: {}", e),
            };
        }
    };

    if let Err(e) = stream.play() {
        return TestToneResult {
            request_id: req.request_id,
            success: false,
            error: format!("play stream: {}", e),
        };
    }

    // Wait for tone to finish, emitting level updates at ~5 Hz
    while !done.load(Ordering::Relaxed) {
        let gain_db = f32::from_bits(gain_atom.load(Ordering::Relaxed));
        let gain_linear = 10f32.powf(gain_db / 20.0);
        let effective_amp = (amplitude as f32) * gain_linear;
        let peak_dbfs = if effective_amp > 0.0 {
            (20.0 * effective_amp.log10()).clamp(-60.0, 0.0)
        } else {
            -60.0
        };
        let rms_dbfs = if effective_amp > 0.0 {
            // RMS of sine = peak / sqrt(2), i.e. -3.01 dB below peak
            (peak_dbfs - 3.01).max(-60.0)
        } else {
            -60.0
        };
        let _ = handle.send(&IpcMessage::device_level_update(DeviceLevelUpdate {
            device_id,
            peak_dbfs,
            rms_dbfs,
            clipping: effective_amp > 1.0,
        }));
        std::thread::sleep(Duration::from_millis(200));
    }

    // Final silent level after tone ends
    let _ = handle.send(&IpcMessage::device_level_update(DeviceLevelUpdate {
        device_id,
        peak_dbfs: -60.0,
        rms_dbfs: -60.0,
        clipping: false,
    }));

    std::thread::sleep(Duration::from_millis(100));
    drop(stream);

    TestToneResult {
        request_id: req.request_id,
        success: true,
        error: String::new(),
    }
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
}
