//! Android JNI surface for the modem cdylib.

#![cfg(target_os = "android")]

use std::ffi::c_void;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{sync_channel, SyncSender};
use std::sync::{Arc, Mutex, OnceLock};
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use chrono::Utc;
use jni::objects::{JClass, JString};
use jni::sys::{jboolean, jfloat, jint, jlong, jshortArray, jsize, jstring, JNI_FALSE, JNI_TRUE, JNI_VERSION_1_6};
use jni::{JNIEnv, JavaVM};
use log::{error, info, warn};

use crate::demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD};
use crate::ipc::proto::{ipc_message, DeviceLevelUpdate, IpcMessage, ReceivedFrame, StatusUpdate};
use crate::ipc::server::{IpcInbound, IpcServer};
use crate::rxonly::feed_chunk;

pub mod audio;
pub mod config_state;
#[cfg(any(target_os = "android", feature = "android-test-stub"))]
pub mod upcall;

const LOG_TAG: &str = "graywolfmodem";
const TARGET_SAMPLE_RATE: u32 = 22_050;

struct ModemState {
    audio_tx: SyncSender<Vec<i16>>,
    stop: Arc<AtomicBool>,
    ready: Arc<AtomicBool>,
    demod_join: Option<JoinHandle<()>>,
}

static STATE: OnceLock<Mutex<Option<ModemState>>> = OnceLock::new();

fn state_slot() -> &'static Mutex<Option<ModemState>> {
    STATE.get_or_init(|| Mutex::new(None))
}

#[no_mangle]
pub extern "system" fn JNI_OnLoad(vm: JavaVM, _reserved: *mut c_void) -> jint {
    android_logger::init_once(
        android_logger::Config::default()
            .with_max_level(log::LevelFilter::Info)
            .with_tag(LOG_TAG),
    );
    info!("JNI_OnLoad: {}", crate::full_version());
    let raw_vm = vm.get_java_vm_pointer() as *mut c_void;
    unsafe {
        ndk_context::initialize_android_context(raw_vm, std::ptr::null_mut());
    }
    JNI_VERSION_1_6
}

#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_modemVersion<'local>(
    env: JNIEnv<'local>,
    _class: JClass<'local>,
) -> jstring {
    env.new_string(crate::full_version())
        .expect("alloc version string")
        .into_raw()
}

/// Boot the modem. Binds the existing IPC server at `socket_path`, applies
/// initial gain (dB), spawns the demod worker.
///
/// Return values (Kotlin caller):
/// - `0`  — success
/// - `-1` — already running (refused)
/// - `-2` — invalid `socket_path` argument
/// - `-3` — thread spawn failure
#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_modemStart<'local>(
    mut env: JNIEnv<'local>,
    _class: JClass<'local>,
    socket_path: JString<'local>,
    initial_gain_db: jfloat,
) -> jint {
    let mut slot = state_slot().lock().unwrap();
    if slot.is_some() {
        warn!("modemStart called while already running; refusing");
        return -1;
    }
    let path: String = match env.get_string(&socket_path) {
        Ok(s) => s.into(),
        Err(e) => {
            error!("get socket_path: {}", e);
            return -2;
        }
    };
    audio::set_gain_db(initial_gain_db as f32);

    let (tx, rx) = sync_channel::<Vec<i16>>(64);
    let stop = Arc::new(AtomicBool::new(false));
    let ready = Arc::new(AtomicBool::new(false));
    let stop_for_thread = stop.clone();
    let ready_for_thread = ready.clone();

    let join = thread::Builder::new()
        .name("graywolfmodem-demod".into())
        .spawn(move || {
            if let Err(e) = run_demod(path, rx, stop_for_thread, ready_for_thread) {
                error!("demod thread exited: {}", e);
            }
        });
    let join = match join {
        Ok(j) => j,
        Err(e) => {
            error!("spawn demod thread: {}", e);
            return -3;
        }
    };

    *slot = Some(ModemState {
        audio_tx: tx,
        stop,
        ready,
        demod_join: Some(join),
    });
    0
}

/// Block up to `timeout_ms` for the IPC server to be bound and accepting.
#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_modemAwaitReady<'local>(
    _env: JNIEnv<'local>,
    _class: JClass<'local>,
    timeout_ms: jlong,
) -> jboolean {
    let deadline = Instant::now() + Duration::from_millis(timeout_ms as u64);
    let ready = {
        let slot = state_slot().lock().unwrap();
        match slot.as_ref() {
            Some(s) => s.ready.clone(),
            None => return JNI_FALSE,
        }
    };
    loop {
        if ready.load(Ordering::Acquire) {
            return JNI_TRUE;
        }
        if Instant::now() >= deadline {
            return JNI_FALSE;
        }
        thread::sleep(Duration::from_millis(50));
    }
}

#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_modemPushSamples<'local>(
    env: JNIEnv<'local>,
    _class: JClass<'local>,
    buf: jshortArray,
    len: jint,
) {
    if len <= 0 {
        return;
    }
    // SAFETY: jshortArray is a borrow we read region-wise; Kotlin retains
    // ownership of the underlying short[].
    let arr = unsafe { jni::objects::JShortArray::from_raw(buf) };
    let mut scratch = vec![0i16; len as usize];
    if let Err(e) = env.get_short_array_region(&arr, 0, &mut scratch) {
        error!("get_short_array_region: {}", e);
        return;
    }
    let tx = {
        let slot = state_slot().lock().unwrap();
        match slot.as_ref() {
            Some(s) => s.audio_tx.clone(),
            None => return,
        }
    };
    let _ = audio::ingest(&scratch, &tx);
}

#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_modemSetGainDb<'local>(
    _env: JNIEnv<'local>,
    _class: JClass<'local>,
    db_value: jfloat,
) {
    audio::set_gain_db(db_value as f32);
    info!("gain set to {:.1} dB", db_value as f32);
}

#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_modemStop<'local>(
    _env: JNIEnv<'local>,
    _class: JClass<'local>,
) {
    let join_opt = {
        let mut slot = state_slot().lock().unwrap();
        let mut state = match slot.take() {
            Some(s) => s,
            None => return,
        };
        state.stop.store(true, Ordering::Release);
        // Drop the sender so the demod loop's recv unblocks.
        drop(state.audio_tx);
        state.demod_join.take()
    };
    if let Some(j) = join_opt {
        let _ = j.join();
    }
    info!("modem stopped");
}

/// Build the canned POC-C TX test frame and return it as a Java short[]
/// (PCM16 mono at 22050 Hz). Synchronous; allocates the array each call.
/// No state — safe to call before or without modemStart().
#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_modemBuildTestFrame<'local>(
    env: JNIEnv<'local>,
    _class: JClass<'local>,
) -> jshortArray {
    let samples = crate::tx::canned::build_canned_test_frame_pcm();
    let arr = match env.new_short_array(samples.len() as jsize) {
        Ok(a) => a,
        Err(e) => {
            error!("new_short_array failed: {}", e);
            return std::ptr::null_mut();
        }
    };
    if let Err(e) = env.set_short_array_region(&arr, 0, &samples) {
        error!("set_short_array_region failed: {}", e);
        return std::ptr::null_mut();
    }
    info!(
        "modemBuildTestFrame: emitted {} samples ({} ms @ {} Hz)",
        samples.len(),
        samples.len() as u64 * 1000 / crate::tx::canned::SAMPLE_RATE_HZ as u64,
        crate::tx::canned::SAMPLE_RATE_HZ,
    );
    arr.into_raw()
}

fn run_demod(
    socket_path: String,
    rx: std::sync::mpsc::Receiver<Vec<i16>>,
    stop: Arc<AtomicBool>,
    ready: Arc<AtomicBool>,
) -> Result<(), String> {
    // Bind the IPC server first so `ready` flips true the moment the
    // socket is accepting. Then split into two threads:
    //   - DSP thread: always drains `rx` and feeds the demodulator.
    //     Decoded frames go onto a bounded `frames_out` queue. Drops
    //     silently when full so the audio pump never blocks.
    //   - IPC thread: blocks on `accept`, then forwards `frames_out`
    //     to the connected Go child. Drops when no client.
    // This decouples DSP from IPC — the audio pump always has somewhere
    // to push samples, and the demod always runs even if the Go child
    // is restarting. Without this split, the first ~1.5 s of audio
    // after every restart fills the rx sync_channel and gets dropped.
    let server = IpcServer::bind(&socket_path).map_err(|e| format!("bind {}: {}", socket_path, e))?;
    info!("ipc server bound at {}", socket_path);
    ready.store(true, Ordering::Release);

    let (frames_tx, frames_rx) = sync_channel::<ReceivedFrame>(64);
    // Bounded level queue. ingest() runs at audio rate but only emits at
    // ~5 Hz, so 32 slots is more than enough; try_send drops on full to
    // protect the JNI audio thread.
    let (level_tx, level_rx) = sync_channel::<DeviceLevelUpdate>(32);
    audio::install_level_tx(level_tx);
    let stop_dsp = stop.clone();
    let dsp_join = thread::Builder::new()
        .name("graywolfmodem-dsp".into())
        .spawn(move || {
            let mut demod = MultiAfskDemodulator::new(
                TARGET_SAMPLE_RATE,
                1200,
                1200,
                2200,
                0,
                &RECOMMENDED_3DEMOD,
            );
            let mut first_frame_logged = false;
            while !stop_dsp.load(Ordering::Relaxed) {
                match rx.recv_timeout(Duration::from_millis(250)) {
                    Ok(chunk) => {
                        for frame in feed_chunk(&mut demod, &chunk) {
                            if !first_frame_logged {
                                info!("poc-b: first_frame_decoded");
                                first_frame_logged = true;
                            }
                            // Tag the frame with the operator-configured
                            // channel id (captured from ConfigureChannel
                            // IPC) instead of the demodulator's internal
                            // 0-indexed `frame.chan`. Without this tag
                            // the Go dispatcher can't attribute frames
                            // to a per-channel rx_frames counter.
                            let pb = ReceivedFrame {
                                channel: config_state::channel_id(),
                                subchan: frame.subchan as u32,
                                slice: frame.slice as u32,
                                data: frame.data,
                                quality: 0,
                                audio_level_mark: 0.0,
                                audio_level_space: 0.0,
                                speed_error: 0.0,
                                retry: String::new(),
                                timestamp_ns: Utc::now().timestamp_nanos_opt().unwrap_or(0) as u64,
                            };
                            config_state::increment_rx_frames();
                            // Drop on full — IPC thread will catch up
                            // when the Go child connects.
                            let _ = frames_tx.try_send(pb);
                        }
                    }
                    Err(std::sync::mpsc::RecvTimeoutError::Timeout) => continue,
                    Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => break,
                }
            }
            info!("dsp thread exiting");
        })
        .map_err(|e| format!("spawn dsp: {}", e))?;

    // IPC accept on the original thread. Blocks until the Go child
    // connects; while blocked, the DSP thread is already running and
    // dropping frames into frames_rx (bounded → silent drop on full).
    let (handle, ipc_rx, ipc_join) =
        server.accept().map_err(|e| format!("accept: {}", e))?;
    info!("poc-b: ipc_client_connected");

    let mut last_status_emit = Instant::now();
    while !stop.load(Ordering::Relaxed) {
        // Short timeout so the loop pumps both frame and level queues
        // without head-of-line blocking on either.
        match frames_rx.recv_timeout(Duration::from_millis(50)) {
            Ok(pb) => {
                if let Err(e) = handle.send(&IpcMessage::received_frame(pb)) {
                    warn!("ipc send (frame): {}", e);
                    break;
                }
            }
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {}
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => break,
        }

        // Drain queued level updates (~5 Hz cadence; usually 0 or 1 per
        // tick). Non-blocking; never head-of-line-blocks the frame path.
        while let Ok(level) = level_rx.try_recv() {
            if let Err(e) = handle.send(&IpcMessage::device_level_update(level)) {
                warn!("ipc send (level): {}", e);
                break;
            }
        }

        // Emit StatusUpdate once per second so the Go modembridge
        // status_cache gets the cumulative rx_frames counter that
        // backs the SPA Dashboard's per-channel RX counter. Audio
        // levels are zero here -- the per-device DeviceLevelUpdate
        // path above is the source of truth on Android.
        let now = Instant::now();
        if now.duration_since(last_status_emit) >= Duration::from_millis(1000) {
            let s = StatusUpdate {
                channel: config_state::channel_id(),
                rx_frames: config_state::rx_frames(),
                rx_bad_fcs: 0,
                tx_frames: 0,
                dcd_transitions: 0,
                audio_level_mark: 0.0,
                audio_level_space: 0.0,
                audio_level_peak: 0.0,
                dcd_state: false,
                shutdown_complete: false,
                timestamp_ns: Utc::now().timestamp_nanos_opt().unwrap_or(0) as u64,
            };
            if let Err(e) = handle.send(&IpcMessage::status_update(s)) {
                warn!("ipc send (status): {}", e);
                break;
            }
            last_status_emit = now;
        }

        // Drain any inbound IPC messages from the Go side. We act on
        // ConfigureChannel to capture the operator-set channel and
        // input_device_id mapping; everything else is currently a no-op
        // on Android (TX path is phase 5).
        while let Ok(inbound) = ipc_rx.try_recv() {
            if let IpcInbound::Message(msg) = inbound {
                if let Some(ipc_message::Payload::ConfigureChannel(cc)) = msg.payload {
                    let chan = cc.channel;
                    let dev = if cc.input_device_id != 0 {
                        cc.input_device_id
                    } else {
                        cc.device_id
                    };
                    info!(
                        "configure channel: channel={} input_device_id={}",
                        chan, dev
                    );
                    config_state::set_from_configure(chan, dev);
                }
            }
        }
    }

    // Close the write side so the reader thread observes EOF and exits.
    drop(handle);
    let _ = ipc_join.join();
    audio::clear_level_tx();

    // On exit (any path), flip ready to false so the supervisor's
    // modem-health poll detects modem death even if the Go child is
    // still alive.
    ready.store(false, Ordering::Release);
    let _ = dsp_join.join();
    info!("demod loop exiting");
    Ok(())
}

// ── JNI install exports ───────────────────────────────────────────────────────
//
// These are the JNI-visible entry points that Kotlin calls once during
// GraywolfService.onCreate (after System.loadLibrary) to hand the Rust modem a
// live reference to each callback object.
//
// Signatures the Kotlin side must match (T5 / ModemBridge.kt):
//   external fun installPttCallback(cb: UsbPttCallback)
//     → interface UsbPttCallback { fun pttSet(method: Int, keyed: Boolean): Boolean }
//   external fun installAudioTxCallback(cb: AudioTxCallback)
//     → interface AudioTxCallback { fun pushSamples(samples: ShortArray, count: Int): Int }

use jni::objects::JObject;

/// Install the Kotlin `UsbPttCallback` implementation.
///
/// Resolves `pttSet(IZ)Z` on the callback object, promotes it to a `GlobalRef`,
/// and stores both so `upcall::jni_ptt_set` can invoke it from any thread.
/// Replaces any prior installation (idempotent across `GraywolfService` restarts).
/// Errors are logged but do not abort — a JNI failure here is bad, but crashing
/// the cdylib at startup is worse.
#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_installPttCallback<'local>(
    mut env: JNIEnv<'local>,
    _class: JClass<'local>,
    callback: JObject<'local>,
) {
    upcall::install_ptt(&mut env, callback);
}

/// Install the Kotlin `AudioTxCallback` implementation.
///
/// Resolves `pushSamples([SI)I` on the callback object, promotes it to a
/// `GlobalRef`, and stores both so `upcall::jni_tx_push_samples` can feed PCM
/// samples to `AudioTxPump` from the Rust modem TX thread.
/// Replaces any prior installation. Errors are logged, not panicked.
#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_installAudioTxCallback<'local>(
    mut env: JNIEnv<'local>,
    _class: JClass<'local>,
    callback: JObject<'local>,
) {
    upcall::install_audio_tx(&mut env, callback);
}
