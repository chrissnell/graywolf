//! UDP audio stream listener for SDR sources (gqrx, rtl_fm piped over socat, etc).
//!
//! Receives raw PCM samples as UDP datagrams on a configurable host:port.
//! Supports s16le and f32le sample formats.

use std::net::UdpSocket;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;

use crate::audio::AudioSource;

pub struct UdpSourceConfig {
    pub host: String,
    pub port: u16,
    pub sample_rate: u32,
    pub format: String, // "s16le" or "f32le"
}

impl Default for UdpSourceConfig {
    fn default() -> Self {
        Self {
            host: "127.0.0.1".into(),
            port: 7355,
            sample_rate: 48000,
            format: "s16le".into(),
        }
    }
}

/// Parse "host:port" from device_name, with format from ConfigureAudio.format.
pub fn parse_config(device_name: &str, sample_rate: u32, format: &str) -> UdpSourceConfig {
    let (host, port) = if let Some(idx) = device_name.rfind(':') {
        let h = &device_name[..idx];
        let p = device_name[idx + 1..].parse::<u16>().unwrap_or(7355);
        (h.to_string(), p)
    } else {
        ("127.0.0.1".to_string(), 7355)
    };

    UdpSourceConfig {
        host,
        port,
        sample_rate,
        format: if format.is_empty() { "s16le".into() } else { format.into() },
    }
}

pub fn spawn(
    cfg: UdpSourceConfig,
    sink: std::sync::mpsc::SyncSender<crate::audio::AudioChunk>,
) -> Result<AudioSource, String> {
    let bind_addr = format!("{}:{}", cfg.host, cfg.port);
    let socket = UdpSocket::bind(&bind_addr)
        .map_err(|e| format!("bind UDP {}: {}", bind_addr, e))?;

    // Non-blocking so we can check the stop flag periodically.
    socket
        .set_read_timeout(Some(std::time::Duration::from_millis(100)))
        .map_err(|e| format!("set_read_timeout: {}", e))?;

    let stop = Arc::new(AtomicBool::new(false));
    let stop_clone = stop.clone();
    let is_f32 = cfg.format == "f32le" || cfg.format == "f32";

    let join = thread::Builder::new()
        .name("audio-sdr-udp".into())
        .spawn(move || recv_loop(socket, sink, stop_clone, is_f32))
        .map_err(|e| format!("spawn UDP thread: {}", e))?;

    Ok(AudioSource {
        sample_rate: cfg.sample_rate,
        _join: Some(join),
        stop,
    })
}

fn recv_loop(
    socket: UdpSocket,
    sink: std::sync::mpsc::SyncSender<crate::audio::AudioChunk>,
    stop: Arc<AtomicBool>,
    is_f32: bool,
) {
    // Max UDP datagram: 65535 bytes. Typical gqrx sends ~4096 bytes.
    let mut buf = [0u8; 65536];

    while !stop.load(Ordering::Relaxed) {
        let n = match socket.recv(&mut buf) {
            Ok(n) => n,
            Err(ref e) if e.kind() == std::io::ErrorKind::WouldBlock
                || e.kind() == std::io::ErrorKind::TimedOut =>
            {
                continue;
            }
            Err(_) => return,
        };

        let chunk = if is_f32 {
            decode_f32le(&buf[..n])
        } else {
            decode_s16le(&buf[..n])
        };

        if !chunk.is_empty() && sink.send(chunk).is_err() {
            return;
        }
    }
}

fn decode_s16le(data: &[u8]) -> Vec<i16> {
    let n = data.len() / 2;
    let mut out = Vec::with_capacity(n);
    for pair in data[..n * 2].chunks_exact(2) {
        out.push(i16::from_le_bytes([pair[0], pair[1]]));
    }
    out
}

fn decode_f32le(data: &[u8]) -> Vec<i16> {
    let n = data.len() / 4;
    let mut out = Vec::with_capacity(n);
    for quad in data[..n * 4].chunks_exact(4) {
        let f = f32::from_le_bytes([quad[0], quad[1], quad[2], quad[3]]);
        out.push((f.clamp(-1.0, 1.0) * 32767.0) as i16);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn decode_s16le_basic() {
        let mut data = Vec::new();
        data.extend_from_slice(&100i16.to_le_bytes());
        data.extend_from_slice(&(-200i16).to_le_bytes());
        let result = decode_s16le(&data);
        assert_eq!(result, vec![100, -200]);
    }

    #[test]
    fn decode_f32le_basic() {
        let mut data = Vec::new();
        data.extend_from_slice(&0.5f32.to_le_bytes());
        data.extend_from_slice(&(-0.5f32).to_le_bytes());
        let result = decode_f32le(&data);
        assert_eq!(result, vec![16383, -16383]);
    }

    #[test]
    fn parse_config_host_port() {
        let cfg = parse_config("192.168.1.10:8000", 44100, "f32le");
        assert_eq!(cfg.host, "192.168.1.10");
        assert_eq!(cfg.port, 8000);
        assert_eq!(cfg.sample_rate, 44100);
        assert_eq!(cfg.format, "f32le");
    }

    #[test]
    fn parse_config_default() {
        let cfg = parse_config("", 48000, "");
        assert_eq!(cfg.host, "127.0.0.1");
        assert_eq!(cfg.port, 7355);
        assert_eq!(cfg.format, "s16le");
    }
}
