//! POC-A Android NativeActivity entry. RX-only.
//!
//! Audio path uses ndk::audio (AAudio) directly rather than cpal because
//! cpal 0.17 takes the AAudio default input preset (`GENERIC`), which
//! enables AGC + noise suppression and crushes the modulated 1200/2200 Hz
//! AFSK tones APRS depends on. The `Unprocessed` preset gives raw line
//! audio; we apply the same -35 dB attenuation graywolf-modem uses on
//! ALSA so the radio + DAC level conventions transfer unchanged.

#![cfg(target_os = "android")]

use std::os::raw::c_void;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{sync_channel, TryRecvError};
use std::sync::Arc;
use std::time::Duration;

use android_activity::{AndroidApp, MainEvent, PollEvent};
use chrono::Utc;
use graywolf_demod::demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD};
use graywolf_demod::rxonly::{feed_chunk, format_ax25_ui_frame};
use log::{error, info, warn};
use ndk::audio::{
    AudioCallbackResult, AudioDirection, AudioFormat, AudioInputPreset, AudioStreamBuilder,
};

mod usb;

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

    // Conservative starting target: 0 dB nominal pin so the device path
    // is restored from prior debug-run residue. Once we confirm audio
    // returns, the target dB knob can be lowered for the actual
    // calibration sweep.
    if let Err(e) = usb::enumerate_and_set_volume(&app, 0.0) {
        warn!("USB capture-gain setup failed: {}", e);
    }

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
    let (tx, rx) = sync_channel::<Vec<i16>>(64);

    // Hardware FU_VOLUME is now applied via JNI before AAudio opens; no
    // post-ADC software attenuation needed in steady-state. Keep the
    // multiply path (gain_q15 = unity) so the per-sample loop and
    // amplitude diagnostic are unchanged.
    let gain_db: f32 = 0.0;
    let gain_lin: f32 = 1.0;
    let gain_q15: i32 = 1 << 15;
    info!("software input gain: {:.1} dB (passthrough)", gain_db);

    let tx_cb = tx.clone();
    let cb_count = std::sync::Arc::new(std::sync::atomic::AtomicU32::new(0));
    let cb_count_cb = cb_count.clone();
    let stream = AudioStreamBuilder::new()
        .map_err(|e| format!("AudioStreamBuilder::new: {:?}", e))?
        .direction(AudioDirection::Input)
        .sample_rate(TARGET_SAMPLE_RATE as i32)
        .channel_count(1)
        .format(AudioFormat::PCM_I16)
        // VoiceRecognition matches MediaRecorder.AudioSource.VOICE_RECOGNITION,
        // which is what aprsdroid (the established Android AFSK app) uses
        // via AudioRecord. The HAL applies modest gain shaping suitable
        // for mid-amplitude voice/audio sources, which keeps the modulated
        // tones inside the i16 range better than Unprocessed does on this
        // hardware combo (UV5R + Digirig CM108).
        .input_preset(AudioInputPreset::VoiceRecognition)
        .data_callback(Box::new(
            move |_stream, data: *mut c_void, num_frames: i32| {
                let n = num_frames.max(0) as usize;
                if n == 0 {
                    return AudioCallbackResult::Continue;
                }
                let raw = unsafe { std::slice::from_raw_parts(data as *const i16, n) };
                let cnt = cb_count_cb.fetch_add(1, std::sync::atomic::Ordering::Relaxed);
                if cnt < 5 {
                    let mut raw_min = i16::MAX;
                    let mut raw_max = i16::MIN;
                    for &s in raw {
                        if s < raw_min { raw_min = s; }
                        if s > raw_max { raw_max = s; }
                    }
                    let head: Vec<i16> = raw.iter().take(8).copied().collect();
                    info!(
                        "RAW cb#{} n={} min={} max={} head={:?}",
                        cnt, n, raw_min, raw_max, head
                    );
                }
                let mut out: Vec<i16> = Vec::with_capacity(n);
                for &s in raw {
                    let scaled = (s as i32 * gain_q15) >> 15;
                    out.push(scaled.clamp(i16::MIN as i32, i16::MAX as i32) as i16);
                }
                let _ = tx_cb.try_send(out);
                AudioCallbackResult::Continue
            },
        ))
        .open_stream()
        .map_err(|e| format!("open_stream: {:?}", e))?;
    drop(tx); // only the callback should hold a sender now.

    let actual_rate = stream.sample_rate() as u32;
    let actual_channels = stream.channel_count();
    info!(
        "AAudio stream open: {} Hz, {} ch, preset Unprocessed",
        actual_rate, actual_channels
    );
    stream
        .request_start()
        .map_err(|e| format!("request_start: {:?}", e))?;
    info!("audio stream started");

    let mut demod = MultiAfskDemodulator::new(actual_rate, 1200, 1200, 2200, 0, &RECOMMENDED_3DEMOD);
    let mut chunks_seen: u64 = 0;
    let mut frames_seen: u64 = 0;
    let mut peak_abs: i32 = 0;
    let mut sumsq: u64 = 0;
    let mut window_samples: u64 = 0;
    let mut clipped: u64 = 0;
    let clip_threshold = (i16::MAX as f32 * 0.9) as i32;
    let mut last_heartbeat = std::time::Instant::now();
    let heartbeat_period = Duration::from_secs(10);
    while !stop.load(Ordering::Relaxed) {
        if last_heartbeat.elapsed() >= heartbeat_period {
            let peak_pct = peak_abs as f32 * 100.0 / i16::MAX as f32;
            let rms = if window_samples > 0 {
                ((sumsq as f64 / window_samples as f64).sqrt()) as f32
            } else { 0.0 };
            let rms_pct = rms * 100.0 / i16::MAX as f32;
            let clip_pct = if window_samples > 0 {
                clipped as f32 * 100.0 / window_samples as f32
            } else { 0.0 };
            // Healthy APRS: rms ~5-20%, peak 30-70%, clip <0.1%.
            // rms 50%+ or clip >5% means turn level down.
            // rms <1% means turn level up.
            info!(
                "heartbeat: chunks={} frames={} peak={:.1}% rms={:.1}% clip={:.2}% elapsed={}s",
                chunks_seen, frames_seen, peak_pct, rms_pct, clip_pct,
                last_heartbeat.elapsed().as_secs()
            );
            last_heartbeat = std::time::Instant::now();
            peak_abs = 0;
            sumsq = 0;
            window_samples = 0;
            clipped = 0;
        }
        match rx.recv_timeout(Duration::from_millis(250)) {
            Ok(chunk) => {
                chunks_seen += 1;
                window_samples += chunk.len() as u64;
                for &s in &chunk {
                    let a = (s as i32).abs();
                    if a > peak_abs { peak_abs = a; }
                    sumsq += (s as i64 * s as i64) as u64;
                    if a >= clip_threshold { clipped += 1; }
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
    let _ = stream.request_stop();
    drop(stream); // releases the AAudio callback closure (and its tx).
    while let Ok(_) | Err(TryRecvError::Empty) = rx.try_recv() {
        if matches!(rx.try_recv(), Err(TryRecvError::Disconnected)) {
            break;
        }
    }
    Ok(())
}
