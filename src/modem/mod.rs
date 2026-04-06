//! Top-level modem orchestration: glues audio sources, demodulators, and
//! the IPC server into a single process. Consumed by `bin/graywolf_modem.rs`.
//!
//! Supports multiple audio devices, multiple channels per device, and
//! multiple demodulator types (AFSK, PSK, 9600).

use std::collections::HashMap;
use std::sync::mpsc::{sync_channel, Receiver, RecvTimeoutError, SyncSender};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use crate::audio::{self, AudioChunk, AudioSource, CHUNK_QUEUE_DEPTH};
use crate::demod_afsk::AfskDemodulator;
use crate::hdlc::DecodedFrame;
use crate::ipc::proto::{
    ipc_message::Payload, ConfigureChannel, ConfigurePtt, DcdChange, IpcMessage, ReceivedFrame,
    StatusUpdate, AudioDeviceList, AudioDeviceInfo, AudioDeviceKind,
    EnumerateAudioDevices,
};
use crate::ipc::server::{IpcHandle, IpcInbound};
use crate::modem_9600::Demod9600;
use crate::modem_psk::PskDemodulator;
use crate::types::{AfskProfile, RetryType, V26Alternative};

/// Current configured state for a single channel.
#[derive(Clone, Debug)]
pub struct ChannelConfig {
    pub channel: u32,
    pub device_id: u32,
    pub audio_channel: u32,
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
            device_id: 0,
            audio_channel: 0,
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
        }
    }
}

/// Per-device active audio pipeline.
struct DevicePipeline {
    source: AudioSource,
    sample_rx: Receiver<AudioChunk>,
    channels: Vec<ChannelPipeline>,
}

/// Per-channel demodulator (within a device pipeline).
enum ChannelDemod {
    Afsk(AfskDemodulator),
    Psk(PskDemodulator),
    Baseband9600(Demod9600),
}

struct ChannelPipeline {
    channel_id: u32,
    #[allow(dead_code)]
    audio_channel: u32,
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
    _ptt_cfgs: HashMap<u32, ConfigurePtt>,

    // Active audio pipelines, keyed by device_id
    active_devices: HashMap<u32, DevicePipeline>,

    // Metrics
    rx_frames: u64,
    rx_bad_fcs: u64,
    dcd_transitions: u64,
    last_status_tx: Instant,
}

impl Modem {
    pub fn new(handle: IpcHandle, inbound: Receiver<IpcInbound>) -> Self {
        Self {
            handle,
            inbound,
            audio_configs: HashMap::new(),
            channel_configs: HashMap::new(),
            _ptt_cfgs: HashMap::new(),
            active_devices: HashMap::new(),
            rx_frames: 0,
            rx_bad_fcs: 0,
            dcd_transitions: 0,
            last_status_tx: Instant::now(),
        }
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
                self.audio_configs.insert(c.device_id, AudioConfig {
                    device_id: c.device_id,
                    device_name: c.device_name,
                    sample_rate: c.sample_rate,
                    channels: c.channels,
                    source_type: c.source_type,
                    format: c.format,
                });
            }
            Some(Payload::ConfigureChannel(c)) => {
                self.channel_configs.insert(c.channel, parse_channel(&c));
            }
            Some(Payload::ConfigurePtt(p)) => {
                self._ptt_cfgs.insert(p.channel, p);
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
            Some(Payload::TransmitFrame(_)) => {
                eprintln!("graywolf-modem: TransmitFrame ignored (not implemented)");
            }
            Some(Payload::Shutdown(_)) => {
                self.graceful_shutdown();
                return true;
            }
            Some(Payload::ReceivedFrame(_))
            | Some(Payload::DcdChange(_))
            | Some(Payload::StatusUpdate(_))
            | Some(Payload::ModemReady(_))
            | Some(Payload::AudioDeviceList(_)) => {
                // Rust → Go only; ignore if echoed back.
            }
            None => {}
        }
        false
    }

    fn start_audio(&mut self) -> Result<(), String> {
        // Stop existing pipelines first
        self.stop_all_audio();

        // Group channels by device_id
        let mut channels_by_device: HashMap<u32, Vec<ChannelConfig>> = HashMap::new();
        for ccfg in self.channel_configs.values() {
            channels_by_device
                .entry(ccfg.device_id)
                .or_default()
                .push(ccfg.clone());
        }

        // If no channels configured, use defaults
        if channels_by_device.is_empty() {
            let default_ccfg = ChannelConfig::default();
            channels_by_device.entry(0).or_default().push(default_ccfg);
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
                        audio_channel: channel_cfgs.first().map(|c| c.audio_channel).unwrap_or(0),
                    },
                    tx,
                )?,
                "flac" => audio::flac::spawn(
                    audio::flac::FlacConfig {
                        path: acfg.device_name.clone(),
                        rate_override: 0,
                        audio_channel: channel_cfgs.first().map(|c| c.audio_channel).unwrap_or(0),
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
                let demod = create_demod(&ccfg, sample_rate);
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
                    audio_channel: ccfg.audio_channel,
                    demod,
                    extra_demods,
                    prev_dcd_any: false,
                    latest_mark: 0.0,
                    latest_space: 0.0,
                    latest_peak: 0.0,
                });
            }

            self.active_devices.insert(*device_id, DevicePipeline {
                source,
                sample_rx: rx,
                channels: chan_pipelines,
            });
        }

        Ok(())
    }

    fn stop_all_audio(&mut self) {
        for (_, pipe) in self.active_devices.drain() {
            pipe.source.stop();
        }
    }

    fn pump_all_audio(&mut self) -> bool {
        let mut got_any = false;
        let device_ids: Vec<u32> = self.active_devices.keys().cloned().collect();

        for device_id in device_ids {
            if let Some(pipe) = self.active_devices.get_mut(&device_id) {
                match pipe.sample_rx.recv_timeout(Duration::from_millis(1)) {
                    Ok(chunk) => {
                        got_any = true;
                        // Feed chunk to all channels on this device
                        for chan_pipe in &mut pipe.channels {
                            // Update peak level
                            let mut peak = chan_pipe.latest_peak;
                            for &s in &chunk {
                                let a = (s as i32).unsigned_abs() as f32 / 32768.0;
                                if a > peak { peak = a; }
                            }
                            chan_pipe.latest_peak = peak * 0.95;

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
                            let dcd_changes = take_demod_dcd_changes(&mut chan_pipe.demod);
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
            tx_frames: 0,
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
            ChannelDemod::Afsk(demod)
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

fn take_demod_dcd_changes(demod: &mut ChannelDemod) -> Vec<crate::demod_afsk::DcdChange> {
    match demod {
        ChannelDemod::Afsk(d) => d.take_dcd_changes(),
        // PSK and 9600 don't produce DcdChange events in the same way
        ChannelDemod::Psk(_) | ChannelDemod::Baseband9600(_) => Vec::new(),
    }
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

                devices.push(AudioDeviceInfo {
                    name: name.clone(),
                    stable_id: name.clone(),
                    kind: AudioDeviceKind::Input.into(),
                    sample_rates,
                    channel_counts,
                    host_api: host_name.clone(),
                    is_default,
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

                    devices.push(AudioDeviceInfo {
                        name: name.clone(),
                        stable_id: name.clone(),
                        kind: AudioDeviceKind::Output.into(),
                        sample_rates,
                        channel_counts,
                        host_api: host_name.clone(),
                        is_default,
                    });
                }
            }
        }
    }

    devices
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
    ChannelConfig {
        channel: c.channel,
        device_id: c.device_id,
        audio_channel: c.audio_channel,
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
