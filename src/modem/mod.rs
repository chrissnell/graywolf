//! Top-level modem orchestration: glues audio sources, the demodulator, and
//! the IPC server into a single process. Consumed by `bin/graywolf_modem.rs`.
//!
//! Phase 1 scope: single logical channel, AFSK demod only, RX only (no TX
//! path yet). Multi-channel and TX are follow-up work.

use std::sync::mpsc::{sync_channel, Receiver, RecvTimeoutError, SyncSender};
use std::time::{Duration, Instant, SystemTime, UNIX_EPOCH};

use crate::audio::{self, AudioChunk, AudioSource, CHUNK_QUEUE_DEPTH};
use crate::demod_afsk::AfskDemodulator;
use crate::hdlc::DecodedFrame;
use crate::ipc::proto::{
    ipc_message::Payload, ConfigureChannel, ConfigurePtt, DcdChange, IpcMessage, ReceivedFrame,
    StatusUpdate,
};
use crate::ipc::server::{IpcHandle, IpcInbound};
use crate::types::{AfskProfile, RetryType};

/// Current configured state for a single channel. Phase 1 assumes channel 0.
#[derive(Clone, Debug)]
pub struct ChannelConfig {
    pub channel: u32,
    pub device_id: u32,
    pub audio_channel: u32,
    pub baud: u32,
    pub mark_freq: u32,
    pub space_freq: u32,
    pub profile: AfskProfile,
    pub num_slicers: usize,
    pub fix_bits: RetryType,
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
            profile: AfskProfile::A,
            num_slicers: 1,
            fix_bits: RetryType::None,
        }
    }
}

#[derive(Clone, Debug)]
pub struct AudioConfig {
    pub device_id: u32,
    pub device_name: String,
    pub sample_rate: u32,
    pub channels: u32,
    pub source_type: String, // "soundcard" | "stdin" | "flac"
}

impl Default for AudioConfig {
    fn default() -> Self {
        Self {
            device_id: 0,
            device_name: String::new(),
            sample_rate: 44100,
            channels: 1,
            source_type: "soundcard".into(),
        }
    }
}

pub struct Modem {
    handle: IpcHandle,
    inbound: Receiver<IpcInbound>,
    audio_cfg: Option<AudioConfig>,
    channel_cfg: Option<ChannelConfig>,
    _ptt_cfg: Option<ConfigurePtt>,

    // Active audio pipeline (Some while running).
    active: Option<ActivePipeline>,

    // Metrics counters (aggregated across lifetime).
    rx_frames: u64,
    rx_bad_fcs: u64,
    dcd_transitions: u64,
    last_status_tx: Instant,
}

struct ActivePipeline {
    source: AudioSource,
    sample_rx: Receiver<AudioChunk>,
    demod: AfskDemodulator,
    prev_dcd_any: bool,
    latest_mark: f32,
    latest_space: f32,
    latest_peak: f32,
}

impl Modem {
    pub fn new(handle: IpcHandle, inbound: Receiver<IpcInbound>) -> Self {
        Self {
            handle,
            inbound,
            audio_cfg: None,
            channel_cfg: None,
            _ptt_cfg: None,
            active: None,
            rx_frames: 0,
            rx_bad_fcs: 0,
            dcd_transitions: 0,
            last_status_tx: Instant::now(),
        }
    }

    /// Main loop: multiplex IPC control messages with audio sample chunks.
    /// Returns when Shutdown is received or the peer disconnects.
    pub fn run(mut self) {
        let status_interval = Duration::from_millis(500);
        loop {
            // Drain any pending IPC messages (non-blocking).
            loop {
                match self.inbound.try_recv() {
                    Ok(IpcInbound::Message(m)) => {
                        if self.handle_ipc(m) {
                            return; // shutdown requested
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

            // Process audio if a pipeline is active.
            let got_audio = if self.active.is_some() {
                self.pump_audio()
            } else {
                false
            };

            // Periodic status push.
            if self.last_status_tx.elapsed() >= status_interval {
                self.emit_status(false);
                self.last_status_tx = Instant::now();
            }

            if !got_audio {
                // Block briefly waiting for either IPC or audio — avoids a
                // busy loop when idle.
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
                self.audio_cfg = Some(AudioConfig {
                    device_id: c.device_id,
                    device_name: c.device_name,
                    sample_rate: c.sample_rate,
                    channels: c.channels,
                    source_type: c.source_type,
                });
            }
            Some(Payload::ConfigureChannel(c)) => {
                self.channel_cfg = Some(parse_channel(&c));
            }
            Some(Payload::ConfigurePtt(p)) => {
                self._ptt_cfg = Some(p);
            }
            Some(Payload::StartAudio(_)) => {
                if let Err(e) = self.start_audio() {
                    eprintln!("graywolf-modem: start_audio failed: {}", e);
                }
            }
            Some(Payload::StopAudio(_)) => {
                self.stop_audio();
            }
            Some(Payload::TransmitFrame(_)) => {
                // Phase 1: TX not implemented. Log and drop.
                eprintln!("graywolf-modem: TransmitFrame ignored (not implemented)");
            }
            Some(Payload::Shutdown(_)) => {
                self.graceful_shutdown();
                return true;
            }
            Some(Payload::ReceivedFrame(_))
            | Some(Payload::DcdChange(_))
            | Some(Payload::StatusUpdate(_))
            | Some(Payload::ModemReady(_)) => {
                // These are Rust → Go only; ignore if echoed back.
            }
            None => {}
        }
        false
    }

    fn start_audio(&mut self) -> Result<(), String> {
        let acfg = self
            .audio_cfg
            .clone()
            .ok_or_else(|| "no ConfigureAudio received".to_string())?;
        let ccfg = self.channel_cfg.clone().unwrap_or_default();

        let (tx, rx): (SyncSender<AudioChunk>, Receiver<AudioChunk>) =
            sync_channel(CHUNK_QUEUE_DEPTH);

        let source = match acfg.source_type.as_str() {
            "soundcard" => audio::soundcard::spawn(
                audio::soundcard::SoundcardConfig {
                    device_name: acfg.device_name.clone(),
                    sample_rate: acfg.sample_rate,
                    channels: acfg.channels,
                    audio_channel: ccfg.audio_channel,
                },
                tx,
            )?,
            "flac" => audio::flac::spawn(
                audio::flac::FlacConfig {
                    path: acfg.device_name.clone(),
                    rate_override: 0,
                    audio_channel: ccfg.audio_channel,
                },
                tx,
            )?,
            "stdin" => audio::stdin_raw::spawn(acfg.sample_rate, tx)?,
            other => return Err(format!("unknown source_type: {}", other)),
        };

        let sample_rate = source.sample_rate;
        let mut demod = AfskDemodulator::new(
            sample_rate,
            ccfg.baud,
            ccfg.mark_freq,
            ccfg.space_freq,
            ccfg.profile,
            ccfg.channel as usize,
            0,
        );
        if ccfg.num_slicers > 1 {
            demod.set_num_slicers(ccfg.num_slicers);
        }
        if ccfg.fix_bits != RetryType::None {
            demod.set_fix_bits(ccfg.fix_bits);
        }

        self.active = Some(ActivePipeline {
            source,
            sample_rx: rx,
            demod,
            prev_dcd_any: false,
            latest_mark: 0.0,
            latest_space: 0.0,
            latest_peak: 0.0,
        });
        Ok(())
    }

    fn stop_audio(&mut self) {
        if let Some(pipe) = self.active.take() {
            pipe.source.stop();
            // Drop sample_rx; the audio thread will notice on next send.
        }
    }

    /// Drain one chunk from the audio channel, feed it through the demod,
    /// and emit any decoded frames / DCD changes over IPC. Returns `true` if
    /// at least one chunk was processed.
    fn pump_audio(&mut self) -> bool {
        let pipe = match self.active.as_mut() {
            Some(p) => p,
            None => return false,
        };
        let chunk = match pipe.sample_rx.recv_timeout(Duration::from_millis(5)) {
            Ok(c) => c,
            Err(RecvTimeoutError::Timeout) => return false,
            Err(RecvTimeoutError::Disconnected) => {
                // Source finished / died; stop the pipeline but keep the
                // modem running so a new StartAudio can recover.
                eprintln!("graywolf-modem: audio source ended");
                self.stop_audio();
                return false;
            }
        };

        // Update peak level for status. Mark/space live inside the demod
        // state; we snapshot them after processing.
        let mut peak = pipe.latest_peak;
        for &s in &chunk {
            let a = (s as i32).unsigned_abs() as f32 / 32768.0;
            if a > peak {
                peak = a;
            }
        }
        pipe.latest_peak = peak * 0.95; // slow decay

        for s in &chunk {
            pipe.demod.process_sample(*s as i32);
        }

        pipe.latest_mark = pipe.demod.state.alevel_mark_peak.max(0.0);
        pipe.latest_space = pipe.demod.state.alevel_space_peak.max(0.0);

        let frames = pipe.demod.take_frames();
        for f in frames {
            self.rx_frames += 1;
            let msg = IpcMessage::received_frame(build_received(&f));
            if let Err(e) = self.handle.send(&msg) {
                eprintln!("graywolf-modem: ipc send failed: {}", e);
            }
        }

        // Propagate any DCD changes the demod buffered.
        for c in pipe.demod.take_dcd_changes() {
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

        let dcd_any = pipe.demod.data_detect_any();
        if dcd_any != pipe.prev_dcd_any {
            pipe.prev_dcd_any = dcd_any;
        }
        true
    }

    fn emit_status(&mut self, final_: bool) {
        let (mark, space, peak, dcd_state) = if let Some(p) = &self.active {
            (p.latest_mark, p.latest_space, p.latest_peak, p.prev_dcd_any)
        } else {
            (0.0, 0.0, 0.0, false)
        };
        let channel = self
            .channel_cfg
            .as_ref()
            .map(|c| c.channel)
            .unwrap_or(0);
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
        // No TX queue to drain in phase 1; just stop audio and emit a final
        // status with the shutdown flag set.
        self.stop_audio();
        self.emit_status(true);
        let _ = self.handle.shutdown_write();
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
    ChannelConfig {
        channel: c.channel,
        device_id: c.device_id,
        audio_channel: c.audio_channel,
        baud: if c.baud == 0 { 1200 } else { c.baud },
        mark_freq: if c.mark_freq == 0 { 1200 } else { c.mark_freq },
        space_freq: if c.space_freq == 0 { 2200 } else { c.space_freq },
        profile,
        num_slicers: c.num_slicers.max(1) as usize,
        fix_bits,
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
