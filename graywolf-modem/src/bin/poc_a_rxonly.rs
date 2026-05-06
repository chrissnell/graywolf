//! POC-A: cross-compile target for proving the Rust modem can demod AX.25
//! on Android via CPAL's NDK/AAudio backend. RX-only. No IPC, no TX, no UI.
//!
//! Lifecycle:
//!  1. Open the default cpal input device at 48 kHz mono.
//!  2. Drain audio chunks into a triple-ensemble AFSK demod (1200 baud,
//!     mark 1200, space 2200 — the APRS standard).
//!  3. Print each successfully-decoded UI frame to stdout, one per line,
//!     stamped with UTC ISO8601 to wall-clock the comparison against the
//!     reference receiver.
//!  4. Exit cleanly on SIGINT (Ctrl+C from `adb shell`).
//!
//! Error and lifecycle events go to stderr, prefix-tagged so post-run
//! grep is trivial: `INFO:`, `WARN:`, `ERR:`.

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::sync_channel;
use std::sync::Arc;
use std::time::Duration;

use chrono::Utc;
use graywolf_demod::audio::soundcard::{spawn as soundcard_spawn, SoundcardConfig};
use graywolf_demod::audio::{AudioChunk, CHUNK_QUEUE_DEPTH};
use graywolf_demod::demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD};
use graywolf_demod::rxonly::{feed_chunk, format_ax25_ui_frame};

const SAMPLE_RATE_HZ: u32 = 48_000;

fn main() -> std::process::ExitCode {
    eprintln!(
        "INFO: poc_a_rxonly {} starting",
        graywolf_demod::full_version()
    );
    eprintln!(
        "INFO: opening default input device @ {} Hz mono",
        SAMPLE_RATE_HZ
    );

    let stop = Arc::new(AtomicBool::new(false));
    install_sigint_handler(stop.clone());

    let (tx, rx) = sync_channel::<AudioChunk>(CHUNK_QUEUE_DEPTH);

    let mut source = match soundcard_spawn(
        SoundcardConfig {
            device_name: String::new(), // default device
            sample_rate: SAMPLE_RATE_HZ,
            channels: 1,
            audio_channel: 0,
        },
        tx,
    ) {
        Ok(s) => s,
        Err(e) => {
            eprintln!("ERR: open input device: {}", e);
            return std::process::ExitCode::from(1);
        }
    };
    eprintln!(
        "INFO: input opened, native rate = {} Hz",
        source.sample_rate
    );

    let mut demod = MultiAfskDemodulator::new(
        source.sample_rate,
        1200,
        1200,
        2200,
        0,
        &RECOMMENDED_3DEMOD,
    );

    let mut chunks_seen: u64 = 0;
    let mut frames_seen: u64 = 0;
    while !stop.load(Ordering::Relaxed) {
        match rx.recv_timeout(Duration::from_millis(250)) {
            Ok(chunk) => {
                chunks_seen += 1;
                for frame in feed_chunk(&mut demod, &chunk) {
                    let stamp = Utc::now().format("%Y-%m-%dT%H:%M:%S%.3fZ");
                    match format_ax25_ui_frame(&frame.data) {
                        Some(s) => println!("{} {}", stamp, s),
                        None => eprintln!(
                            "WARN: undecodable frame (len={}, chan={}, sub={}, slice={})",
                            frame.data.len(),
                            frame.chan,
                            frame.subchan,
                            frame.slice,
                        ),
                    }
                    frames_seen += 1;
                }
            }
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => continue,
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => {
                eprintln!("ERR: audio source disconnected");
                break;
            }
        }
    }

    eprintln!(
        "INFO: shutdown requested, draining; chunks={} frames={}",
        chunks_seen, frames_seen,
    );
    source.stop_and_join();
    eprintln!("INFO: clean exit");
    std::process::ExitCode::SUCCESS
}

#[cfg(unix)]
fn install_sigint_handler(stop: Arc<AtomicBool>) {
    // Direct libc::signal call avoids pulling in `signal-hook` or `ctrlc`
    // crates for what is a one-line Android requirement. SIGINT is the only
    // signal `adb shell` sends on Ctrl+C; the handler stores into the atomic
    // and the main loop's recv_timeout polls it within 250 ms.
    use std::sync::OnceLock;
    static FLAG: OnceLock<Arc<AtomicBool>> = OnceLock::new();
    FLAG.set(stop).ok();
    extern "C" fn handler(_sig: libc::c_int) {
        if let Some(f) = FLAG.get() {
            f.store(true, Ordering::Relaxed);
        }
    }
    unsafe {
        libc::signal(libc::SIGINT, handler as libc::sighandler_t);
        libc::signal(libc::SIGTERM, handler as libc::sighandler_t);
    }
}

#[cfg(not(unix))]
fn install_sigint_handler(_stop: Arc<AtomicBool>) {
    // Windows desktop bring-up: Ctrl+C will just kill the process; that's
    // acceptable for the POC since the only target that matters is Android.
}
