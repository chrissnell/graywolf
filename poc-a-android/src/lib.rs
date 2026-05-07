//! POC-A Android NativeActivity entry. RX-only.
//!
//! Audio path uses ndk::audio (AAudio) directly rather than cpal because
//! cpal 0.17 takes the AAudio default input preset (`GENERIC`), which
//! enables AGC + noise suppression and crushes the modulated 1200/2200 Hz
//! AFSK tones APRS depends on. The `Unprocessed` preset gives raw line
//! audio; we apply the same -35 dB attenuation graywolf-modem uses on
//! ALSA so the radio + DAC level conventions transfer unchanged.

#![cfg(target_os = "android")]

use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{sync_channel, TryRecvError};
use std::sync::Arc;
use std::time::Duration;

use android_activity::{AndroidApp, MainEvent, PollEvent};
use chrono::Utc;
use graywolf_demod::demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD};
use graywolf_demod::rxonly::{feed_chunk, format_ax25_ui_frame};
use jni::JavaVM;
use log::{error, info, warn};

mod audio_record;
mod usb;

// 22050 Hz matches the rate aprsdroid runs against its demod on the same
// Baofeng + CMedia hardware. Keeps mark/space well above the 4400 Hz
// Nyquist floor while staying inside the audio HAL's "voice/recognition"
// gain envelope, which is the routing path that doesn't rail the input.
const TARGET_SAMPLE_RATE: u32 = 22_050;
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
    let stop_for_demod = stop.clone();

    // Pull the JavaVM out of AndroidApp once; both the AudioRecord pump
    // thread and the demod thread will take Arc clones.
    let vm: Arc<JavaVM> = match unsafe { JavaVM::from_raw(app.vm_as_ptr() as *mut _) } {
        Ok(v) => Arc::new(v),
        Err(e) => {
            error!("JavaVM::from_raw: {}", e);
            return;
        }
    };

    // Diagnostic-only USB enumeration on a worker thread. Used to live
    // on the main thread, but the permission-grant polling loop pushed
    // it past Android's ANR ceiling.
    let vm_for_usb = vm.clone();
    let activity_ptr_usize = app.activity_as_ptr() as usize;
    std::thread::spawn(move || {
        if let Err(e) = usb::enumerate_only(vm_for_usb, activity_ptr_usize) {
            warn!("USB enumeration failed: {}", e);
        }
    });

    std::thread::spawn(move || {
        if let Err(e) = run_demod(vm, stop_for_demod) {
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

fn run_demod(vm: Arc<JavaVM>, stop: Arc<AtomicBool>) -> Result<(), String> {
    let (tx, rx) = sync_channel::<Vec<i16>>(64);
    let actual_rate = audio_record::spawn(vm, TARGET_SAMPLE_RATE, stop.clone(), tx)?;
    info!(
        "AudioRecord pump up @ {} Hz, MIC source, mono PCM16",
        actual_rate
    );

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
    while let Ok(_) | Err(TryRecvError::Empty) = rx.try_recv() {
        if matches!(rx.try_recv(), Err(TryRecvError::Disconnected)) {
            break;
        }
    }
    Ok(())
}
