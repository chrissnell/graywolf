//! POC-A Android NativeActivity entry. RX-only.
//!
//! Lifecycle:
//!  1. Bridge `android-activity`'s `AndroidApp` (which holds the JavaVM
//!     and Activity context pointers) into the global `ndk_context` slot
//!     that cpal's Android backend reads at first stream-build.
//!  2. Initialize `android_logger` so `log::info!` / `eprintln!`-style
//!     messages route to logcat under tag `poc_a_rxonly`.
//!  3. Open the default cpal input stream at 48 kHz mono via cpal's
//!     Android backend (AAudio under the hood).
//!  4. Push samples into a triple-ensemble AFSK demodulator and log
//!     each decoded UI frame as one line, prefixed with UTC ISO8601.
//!  5. Park `android_main` polling lifecycle events; exit when the OS
//!     destroys the activity (back press, swipe-away, system-kill).

#![cfg(target_os = "android")]

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{sync_channel, TryRecvError};
use std::sync::Arc;
use std::time::Duration;

use android_activity::{AndroidApp, MainEvent, PollEvent};
use chrono::Utc;
use cpal::traits::{DeviceTrait, HostTrait, StreamTrait};
use cpal::{SampleFormat, StreamConfig};
use graywolf_demod::demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD};
use graywolf_demod::rxonly::{feed_chunk, format_ax25_ui_frame};
use log::{error, info, warn};

const TARGET_SAMPLE_RATE: u32 = 48_000;
const LOG_TAG: &str = "poc_a_rxonly";

#[no_mangle]
fn android_main(app: AndroidApp) {
    android_logger::init_once(
        android_logger::Config::default()
            .with_max_level(log::LevelFilter::Info)
            .with_tag(LOG_TAG),
    );

    // android-activity 0.6.1 already calls
    // `ndk_context::initialize_android_context()` from its NativeActivity
    // glue (init.rs:285) before invoking us. Calling it again would panic
    // — ndk-context's slot is OnceCell-backed. cpal's Android backend
    // reads the same global slot and now finds it populated, so the
    // `android context was not initialized` panic seen on raw exec
    // is resolved by virtue of running inside an APK with this glue.
    info!(
        "poc_a_rxonly {} starting (NativeActivity)",
        graywolf_demod::full_version()
    );

    let stop = Arc::new(AtomicBool::new(false));
    let stop_clone = stop.clone();
    std::thread::spawn(move || {
        if let Err(e) = run_demod(stop_clone) {
            error!("demod thread exited: {}", e);
        }
    });

    // Park the main thread on the activity event loop. cpal's audio stream
    // runs on its own thread (managed by AAudio); this loop is just here
    // to satisfy NativeActivity's lifecycle contract and trigger orderly
    // shutdown when the OS asks for it.
    loop {
        let mut should_exit = false;
        app.poll_events(Some(Duration::from_millis(500)), |event| match event {
            PollEvent::Main(MainEvent::Destroy) => should_exit = true,
            PollEvent::Main(MainEvent::Resume { .. }) => info!("activity resumed"),
            PollEvent::Main(MainEvent::Pause) => info!("activity paused"),
            _ => {}
        });
        if should_exit {
            info!("Destroy event received; signalling demod thread");
            stop.store(true, Ordering::Relaxed);
            break;
        }
    }
}

fn run_demod(stop: Arc<AtomicBool>) -> Result<(), String> {
    let host = cpal::default_host();
    let device = host
        .default_input_device()
        .ok_or_else(|| "no default input device".to_string())?;
    info!(
        "input device: {:?}",
        device.name().unwrap_or_else(|_| "<unknown>".into())
    );

    // Pick an input config close to 48 kHz mono i16. AAudio on Android
    // typically returns f32 streams; we convert in-callback.
    let supported = device
        .default_input_config()
        .map_err(|e| format!("default_input_config: {}", e))?;
    let sample_format = supported.sample_format();
    let sample_rate = pick_sample_rate(&device, TARGET_SAMPLE_RATE);
    let channels = supported.channels();
    info!(
        "stream config: {} Hz, {} ch, format {:?}",
        sample_rate, channels, sample_format
    );

    let config = StreamConfig {
        channels,
        sample_rate,
        buffer_size: cpal::BufferSize::Default,
    };

    let (tx, rx) = sync_channel::<Vec<i16>>(64);
    let want_channel = 0usize;
    let n_channels = channels as usize;

    let err_fn = |e| error!("cpal stream error: {}", e);
    let stream = match sample_format {
        SampleFormat::F32 => device
            .build_input_stream(
                &config,
                move |data: &[f32], _| {
                    let mono: Vec<i16> = data
                        .chunks(n_channels)
                        .map(|frame| {
                            let s = frame[want_channel.min(frame.len() - 1)];
                            (s.clamp(-1.0, 1.0) * i16::MAX as f32) as i16
                        })
                        .collect();
                    let _ = tx.try_send(mono);
                },
                err_fn,
                None,
            )
            .map_err(|e| format!("build_input_stream f32: {}", e))?,
        SampleFormat::I16 => device
            .build_input_stream(
                &config,
                move |data: &[i16], _| {
                    let mono: Vec<i16> = data
                        .chunks(n_channels)
                        .map(|frame| frame[want_channel.min(frame.len() - 1)])
                        .collect();
                    let _ = tx.try_send(mono);
                },
                err_fn,
                None,
            )
            .map_err(|e| format!("build_input_stream i16: {}", e))?,
        other => return Err(format!("unsupported sample format: {:?}", other)),
    };
    stream.play().map_err(|e| format!("stream play: {}", e))?;
    info!("audio stream started");

    let mut demod = MultiAfskDemodulator::new(sample_rate, 1200, 1200, 2200, 0, &RECOMMENDED_3DEMOD);
    let mut chunks_seen: u64 = 0;
    let mut frames_seen: u64 = 0;
    let mut samples_total: u64 = 0;
    let mut peak_abs: i32 = 0;
    let mut last_heartbeat = std::time::Instant::now();
    let heartbeat_period = Duration::from_secs(10);
    while !stop.load(Ordering::Relaxed) {
        if last_heartbeat.elapsed() >= heartbeat_period {
            // Peak amplitude as % of i16 full-scale tells us if real audio
            // is reaching the DSP. <2% = silence (likely opened wrong device
            // or cable disconnected). 30-60% = healthy APRS RX level. >85%
            // = clipping; demod will fail on edges.
            let peak_pct = peak_abs as f32 * 100.0 / i16::MAX as f32;
            info!(
                "heartbeat: chunks={} frames={} samples={} peak={:.1}% elapsed={}s",
                chunks_seen,
                frames_seen,
                samples_total,
                peak_pct,
                last_heartbeat.elapsed().as_secs()
            );
            last_heartbeat = std::time::Instant::now();
            peak_abs = 0;
        }
        match rx.recv_timeout(Duration::from_millis(250)) {
            Ok(chunk) => {
                chunks_seen += 1;
                samples_total += chunk.len() as u64;
                for &s in &chunk {
                    let a = (s as i32).abs();
                    if a > peak_abs { peak_abs = a; }
                }
                for frame in feed_chunk(&mut demod, &chunk) {
                    let stamp = Utc::now().format("%Y-%m-%dT%H:%M:%S%.3fZ");
                    match format_ax25_ui_frame(&frame.data) {
                        Some(s) => info!("FRAME {} {}", stamp, s),
                        None => warn!(
                            "undecodable frame len={} chan={} sub={} slice={}",
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
                error!("audio channel disconnected");
                break;
            }
        }
    }

    info!(
        "demod loop exiting; chunks={} frames={}",
        chunks_seen, frames_seen
    );
    drop(stream);
    // Drain anything left so we don't leak heap on next run.
    while let Err(TryRecvError::Empty) | Ok(_) = rx.try_recv() {
        if let Err(TryRecvError::Disconnected) = rx.try_recv() {
            break;
        }
    }
    Ok(())
}

fn pick_sample_rate(device: &cpal::Device, target: u32) -> u32 {
    if let Ok(configs) = device.supported_input_configs() {
        for cfg in configs {
            let min = cfg.min_sample_rate();
            let max = cfg.max_sample_rate();
            if min <= target && target <= max {
                return target;
            }
        }
    }
    device
        .default_input_config()
        .map(|c| c.sample_rate())
        .unwrap_or(target)
}
