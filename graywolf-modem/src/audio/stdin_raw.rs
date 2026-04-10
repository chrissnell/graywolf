//! Raw little-endian i16 PCM samples on stdin.
//!
//! Used for test harnesses and SDR bridges that pipe samples over a pipe.
//! The sample rate must be supplied out-of-band via ConfigureAudio since raw
//! PCM carries no header.

use std::io::{self, Read};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::thread;

use super::AudioSource;

const CHUNK_BYTES: usize = 960 * 2; // 960 samples * 2 bytes

pub fn spawn(
    sample_rate: u32,
    sink: std::sync::mpsc::SyncSender<super::AudioChunk>,
) -> Result<AudioSource, String> {
    let stop = Arc::new(AtomicBool::new(false));
    let stop_clone = stop.clone();

    let join = thread::Builder::new()
        .name("audio-stdin".into())
        .spawn(move || read_loop(sink, stop_clone))
        .map_err(|e| format!("spawn stdin thread: {}", e))?;

    Ok(AudioSource {
        sample_rate,
        _join: Some(join),
        stop,
    })
}

fn read_loop(
    sink: std::sync::mpsc::SyncSender<super::AudioChunk>,
    stop: Arc<AtomicBool>,
) {
    let stdin = io::stdin();
    let mut handle = stdin.lock();
    let mut buf = vec![0u8; CHUNK_BYTES];

    while !stop.load(Ordering::Relaxed) {
        match handle.read(&mut buf) {
            Ok(0) => return, // EOF
            Ok(n) => {
                let n = n & !1; // drop trailing half-sample on odd read
                let mut chunk: Vec<i16> = Vec::with_capacity(n / 2);
                for pair in buf[..n].chunks_exact(2) {
                    chunk.push(i16::from_le_bytes([pair[0], pair[1]]));
                }
                if sink.send(chunk).is_err() {
                    return;
                }
            }
            Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
            Err(_) => return,
        }
    }
}
