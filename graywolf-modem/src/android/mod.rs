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
use jni::sys::{jboolean, jfloat, jint, jlong, jshortArray, jstring, JNI_FALSE, JNI_TRUE, JNI_VERSION_1_6};
use jni::{JNIEnv, JavaVM};
use log::{error, info, warn};

use crate::demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD};
use crate::ipc::proto::{IpcMessage, ReceivedFrame};
use crate::ipc::server::IpcServer;
use crate::rxonly::feed_chunk;

pub mod audio;

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
    while Instant::now() < deadline {
        if ready.load(Ordering::Acquire) {
            return JNI_TRUE;
        }
        thread::sleep(Duration::from_millis(50));
    }
    JNI_FALSE
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
                            let pb = ReceivedFrame {
                                channel: frame.chan as u32,
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

    while !stop.load(Ordering::Relaxed) {
        match frames_rx.recv_timeout(Duration::from_millis(250)) {
            Ok(pb) => {
                if let Err(e) = handle.send(&IpcMessage::received_frame(pb)) {
                    warn!("ipc send: {}", e);
                    break;
                }
            }
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => {}
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => break,
        }
        // Drain any inbound IPC messages so the channel doesn't back up.
        // POC-B doesn't act on configure messages — phase 3 wires them in.
        while let Ok(_msg) = ipc_rx.try_recv() {}
    }

    // Close the write side so the reader thread observes EOF and exits.
    drop(handle);
    let _ = ipc_join.join();

    // On exit (any path), flip ready to false so the supervisor's
    // modem-health poll (Task 15) detects modem death even if the Go
    // child is still alive.
    ready.store(false, Ordering::Release);
    let _ = dsp_join.join();
    info!("demod loop exiting");
    Ok(())
}
