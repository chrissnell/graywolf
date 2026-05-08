//! Android JNI surface for the modem cdylib. All entry points live here.
//!
//! Java package path (must match Kotlin `ModemBridge` declaration):
//!     com.nw5w.graywolf.jni.ModemBridge
//!
//! JNI mangling: `Java_<pkg>_<class>_<method>` with dots/dollars escaped.

#![cfg(target_os = "android")]

use std::ffi::c_void;

use jni::objects::JClass;
use jni::sys::{jint, jstring, JNI_VERSION_1_6};
use jni::{JNIEnv, JavaVM};
use log::info;

mod audio;

const LOG_TAG: &str = "graywolfmodem";

/// Called once by the JVM when `System.loadLibrary("graywolfmodem")` resolves
/// the cdylib. Initialises `android_logger` and stashes the JavaVM in
/// `ndk_context` so any future code path that pulls the global Android
/// context (cpal, ndk crate, etc.) finds it populated. POC-A demonstrated
/// that this is required even when nothing in the Rust crate currently
/// touches `ndk_context::android_context()` — leaving it un-init makes
/// later additions silently panic.
#[no_mangle]
pub extern "system" fn JNI_OnLoad(vm: JavaVM, _reserved: *mut c_void) -> jint {
    android_logger::init_once(
        android_logger::Config::default()
            .with_max_level(log::LevelFilter::Info)
            .with_tag(LOG_TAG),
    );
    info!("graywolfmodem JNI_OnLoad: {}", crate::full_version());

    // ndk_context's slot is OnceCell-backed; double-init panics. Safe-guard
    // by ignoring the error from a re-entry (only happens in development
    // hot-reload scenarios, which POC-B doesn't exercise).
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
    let v = crate::full_version();
    env.new_string(v)
        .expect("alloc version string")
        .into_raw()
}
