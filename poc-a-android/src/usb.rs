//! USB Audio Class capture-gain control via JNI to UsbManager.
//!
//! AAudio on Android does not expose any equivalent of ALSA's
//! `amixer set Capture XdB`. The Linux graywolf-modem operator workflow
//! for a Digirig + UV5R chain calibrates the codec's capture-side gain
//! to roughly -35 dB; without that stage the analog-into-digital
//! conversion saturates the i16 range on normal APRS-volume audio.
//!
//! This module reaches around AAudio by talking directly to the USB
//! Audio Class control interface via Android's UsbManager:
//!
//!   1. Enumerate USB devices via `UsbManager.getDeviceList()`.
//!   2. Pick one that exposes a USB Audio Class (0x01) interface.
//!   3. Open a `UsbDeviceConnection` and walk the Audio Control
//!      interface descriptor to find the input Feature Unit and its
//!      Volume Control selector.
//!   4. Issue a USB Audio Class SET_CUR control transfer with the
//!      desired dB target, mirroring what `snd-usb-audio` does on
//!      Linux when the operator runs `amixer set Capture -35dB`.
//!
//! Stage 1 (this commit): just enumerate + log. Stages 2-4 follow
//! once we know what the device descriptors look like.

use android_activity::AndroidApp;
use jni::objects::{JByteArray, JObject, JString, JValue};
use jni::JavaVM;
use log::{info, warn};

const USB_AUDIO_CLASS: i32 = 0x01;
// USB Audio Class spec, table A-9: SET_CUR request.
const UAC_SET_CUR: i32 = 0x01;
// Class-specific request, recipient interface, host-to-device.
const UAC_CONTROL_OUT: i32 = 0x21;
// Volume Control Selector inside a Feature Unit (UAC1 spec, Appendix A.10).
const VOLUME_CONTROL: i32 = 0x02;
// USB Audio Class descriptor types (Appendix A.4).
const CS_INTERFACE: u8 = 0x24;
const FEATURE_UNIT: u8 = 0x06;
// Standard descriptor type for "Configuration".
const DESCRIPTOR_TYPE_CONFIG: u8 = 0x02;
const REQ_GET_DESCRIPTOR: i32 = 0x06;
const REQ_TYPE_STANDARD_IN: i32 = 0x80;

/// Walk the configuration descriptor returned by GET_DESCRIPTOR and
/// find the first Feature Unit (subtype 0x06) inside the Audio Control
/// interface (class 0x01, sub 0x01) whose `bmaControls(0)` (master
/// channel) advertises Volume Control (bit 1).
///
/// Returns `(audio_control_iface_number, feature_unit_id)` if found.
fn find_feature_unit_with_volume(desc: &[u8]) -> Option<(u8, u8)> {
    let mut i = 0;
    let mut current_ac_iface: Option<u8> = None;
    while i + 2 <= desc.len() {
        let b_length = desc[i] as usize;
        if b_length < 2 || i + b_length > desc.len() {
            break;
        }
        let b_descriptor_type = desc[i + 1];
        // Standard Interface descriptor (0x04) — track AC interface number.
        if b_descriptor_type == 0x04 && b_length >= 9 {
            let b_interface_number = desc[i + 2];
            let b_interface_class = desc[i + 5];
            let b_interface_subclass = desc[i + 6];
            if b_interface_class == 0x01 && b_interface_subclass == 0x01 {
                current_ac_iface = Some(b_interface_number);
            } else {
                current_ac_iface = None;
            }
        }
        // Class-specific AC descriptor (0x24) of subtype FEATURE_UNIT (0x06).
        if b_descriptor_type == CS_INTERFACE && b_length >= 7 && desc[i + 2] == FEATURE_UNIT {
            if let Some(ac_iface) = current_ac_iface {
                let b_unit_id = desc[i + 3];
                let b_control_size = desc[i + 5] as usize;
                if b_control_size > 0 && i + 6 + b_control_size <= i + b_length {
                    let bma_master = desc[i + 6];
                    // Bit 1 = Volume Control per UAC1 spec table A-7.
                    if bma_master & 0x02 != 0 {
                        return Some((ac_iface, b_unit_id));
                    }
                }
            }
        }
        i += b_length;
    }
    None
}

/// Best-effort: list USB devices, locate the USB-Audio class device,
/// request permission, open it, walk the config descriptor for a
/// volume-capable Feature Unit, and SET_CUR the master volume to
/// `target_db`. Logs progress and returns Ok unless we hit something
/// that blocks the entire flow.
pub fn enumerate_and_set_volume(app: &AndroidApp, target_db: f32) -> Result<(), String> {
    let vm_ptr = app.vm_as_ptr() as *mut jni::sys::JavaVM;
    let activity_ptr = app.activity_as_ptr() as jni::sys::jobject;
    if vm_ptr.is_null() || activity_ptr.is_null() {
        return Err("AndroidApp has null VM or Activity pointer".into());
    }

    let vm = unsafe { JavaVM::from_raw(vm_ptr) }.map_err(|e| format!("JavaVM::from_raw: {}", e))?;
    let mut env = vm
        .attach_current_thread()
        .map_err(|e| format!("attach_current_thread: {}", e))?;
    let context = unsafe { JObject::from_raw(activity_ptr) };

    // UsbManager um = (UsbManager) context.getSystemService("usb");
    let svc_name = env
        .new_string("usb")
        .map_err(|e| format!("new_string usb: {}", e))?;
    let usb_manager = env
        .call_method(
            &context,
            "getSystemService",
            "(Ljava/lang/String;)Ljava/lang/Object;",
            &[(&svc_name).into()],
        )
        .and_then(|v| v.l())
        .map_err(|e| format!("getSystemService(usb): {}", e))?;

    // HashMap<String, UsbDevice> map = um.getDeviceList();
    let device_map = env
        .call_method(&usb_manager, "getDeviceList", "()Ljava/util/HashMap;", &[])
        .and_then(|v| v.l())
        .map_err(|e| format!("getDeviceList: {}", e))?;
    let values = env
        .call_method(&device_map, "values", "()Ljava/util/Collection;", &[])
        .and_then(|v| v.l())
        .map_err(|e| format!("values: {}", e))?;
    let iter = env
        .call_method(&values, "iterator", "()Ljava/util/Iterator;", &[])
        .and_then(|v| v.l())
        .map_err(|e| format!("iterator: {}", e))?;

    let mut count = 0u32;
    let mut audio_count = 0u32;
    let mut audio_device: Option<JObject> = None;
    loop {
        let has_next = env
            .call_method(&iter, "hasNext", "()Z", &[])
            .and_then(|v| v.z())
            .map_err(|e| format!("hasNext: {}", e))?;
        if !has_next {
            break;
        }
        let device = env
            .call_method(&iter, "next", "()Ljava/lang/Object;", &[])
            .and_then(|v| v.l())
            .map_err(|e| format!("next: {}", e))?;

        let vid = env
            .call_method(&device, "getVendorId", "()I", &[])
            .and_then(|v| v.i())
            .unwrap_or(-1);
        let pid = env
            .call_method(&device, "getProductId", "()I", &[])
            .and_then(|v| v.i())
            .unwrap_or(-1);
        let class = env
            .call_method(&device, "getDeviceClass", "()I", &[])
            .and_then(|v| v.i())
            .unwrap_or(-1);

        let name = env
            .call_method(&device, "getDeviceName", "()Ljava/lang/String;", &[])
            .and_then(|v| v.l())
            .map(|o| {
                env.get_string(&JString::from(o))
                    .map(|s| s.to_string_lossy().into_owned())
                    .unwrap_or_default()
            })
            .unwrap_or_default();

        let prod_name = env
            .call_method(&device, "getProductName", "()Ljava/lang/String;", &[])
            .and_then(|v| v.l())
            .map(|o| {
                if o.is_null() {
                    String::new()
                } else {
                    env.get_string(&JString::from(o))
                        .map(|s| s.to_string_lossy().into_owned())
                        .unwrap_or_default()
                }
            })
            .unwrap_or_default();

        info!(
            "USB device #{}: name={} product='{}' vid=0x{:04x} pid=0x{:04x} devClass=0x{:02x}",
            count, name, prod_name, vid, pid, class
        );

        let iface_count = env
            .call_method(&device, "getInterfaceCount", "()I", &[])
            .and_then(|v| v.i())
            .unwrap_or(0);
        let mut has_audio = false;
        for i in 0..iface_count {
            let iface = match env
                .call_method(
                    &device,
                    "getInterface",
                    "(I)Landroid/hardware/usb/UsbInterface;",
                    &[(i as jni::sys::jint).into()],
                )
                .and_then(|v| v.l())
            {
                Ok(o) => o,
                Err(e) => {
                    warn!("  iface[{}] read err: {}", i, e);
                    continue;
                }
            };
            let iclass = env
                .call_method(&iface, "getInterfaceClass", "()I", &[])
                .and_then(|v| v.i())
                .unwrap_or(-1);
            let isub = env
                .call_method(&iface, "getInterfaceSubclass", "()I", &[])
                .and_then(|v| v.i())
                .unwrap_or(-1);
            let iproto = env
                .call_method(&iface, "getInterfaceProtocol", "()I", &[])
                .and_then(|v| v.i())
                .unwrap_or(-1);
            info!(
                "  iface[{}] class=0x{:02x} sub=0x{:02x} proto=0x{:02x}",
                i, iclass, isub, iproto
            );
            if iclass == 0x01 {
                has_audio = true;
            }
        }
        if has_audio {
            audio_count += 1;
            // Stash the first audio device we find for the volume-set step.
            if audio_device.is_none() {
                audio_device = Some(device);
            }
        } else {
            // Drop the local ref if we didn't keep the JObject.
            drop(device);
        }
        count += 1;
    }
    info!(
        "USB enumeration: {} device(s), {} with USB Audio class interface",
        count, audio_count
    );

    let device = match audio_device {
        Some(d) => d,
        None => {
            warn!("no USB Audio class device present; skipping capture-gain setup");
            return Ok(());
        }
    };

    // Permission gating. UsbManager.hasPermission(device) is fast; if false
    // we ask via requestPermission and poll. Without permission the
    // openDevice call returns null.
    let mut has_perm = env
        .call_method(
            &usb_manager,
            "hasPermission",
            "(Landroid/hardware/usb/UsbDevice;)Z",
            &[(&device).into()],
        )
        .and_then(|v| v.z())
        .unwrap_or(false);
    info!("USB capture device: hasPermission={}", has_perm);

    if !has_perm {
        // Build a dummy PendingIntent so requestPermission has somewhere to
        // route the user's response. We don't read the broadcast — we just
        // poll hasPermission below until the dialog resolves.
        let action = env
            .new_string("com.chrissnell.graywolf.poca.USB_PERM")
            .map_err(|e| format!("new_string action: {}", e))?;
        let intent_class = env
            .find_class("android/content/Intent")
            .map_err(|e| format!("find Intent: {}", e))?;
        let intent = env
            .new_object(intent_class, "(Ljava/lang/String;)V", &[(&action).into()])
            .map_err(|e| format!("new Intent: {}", e))?;
        // PendingIntent.FLAG_MUTABLE = 0x02000000 (required on Android 12+).
        // FLAG_UPDATE_CURRENT = 0x08000000.
        let flags: i32 = 0x02000000 | 0x08000000;
        let pi_class = env
            .find_class("android/app/PendingIntent")
            .map_err(|e| format!("find PendingIntent: {}", e))?;
        let pending_intent = env
            .call_static_method(
                pi_class,
                "getBroadcast",
                "(Landroid/content/Context;ILandroid/content/Intent;I)Landroid/app/PendingIntent;",
                &[
                    (&context).into(),
                    JValue::Int(0),
                    (&intent).into(),
                    JValue::Int(flags),
                ],
            )
            .and_then(|v| v.l())
            .map_err(|e| format!("PendingIntent.getBroadcast: {}", e))?;

        info!("requesting USB permission via UsbManager.requestPermission");
        env.call_method(
            &usb_manager,
            "requestPermission",
            "(Landroid/hardware/usb/UsbDevice;Landroid/app/PendingIntent;)V",
            &[(&device).into(), (&pending_intent).into()],
        )
        .map_err(|e| format!("requestPermission: {}", e))?;

        // Poll up to ~30 seconds for the user to tap Allow.
        for tick in 0..60 {
            std::thread::sleep(std::time::Duration::from_millis(500));
            has_perm = env
                .call_method(
                    &usb_manager,
                    "hasPermission",
                    "(Landroid/hardware/usb/UsbDevice;)Z",
                    &[(&device).into()],
                )
                .and_then(|v| v.z())
                .unwrap_or(false);
            if has_perm {
                info!("USB permission granted after {} ticks", tick);
                break;
            }
        }
    }
    if !has_perm {
        warn!("USB permission not granted (timeout); capture gain stays at default");
        return Ok(());
    }

    // openDevice -> UsbDeviceConnection
    let connection = env
        .call_method(
            &usb_manager,
            "openDevice",
            "(Landroid/hardware/usb/UsbDevice;)Landroid/hardware/usb/UsbDeviceConnection;",
            &[(&device).into()],
        )
        .and_then(|v| v.l())
        .map_err(|e| format!("openDevice: {}", e))?;
    if connection.is_null() {
        return Err("openDevice returned null".into());
    }

    // GET_DESCRIPTOR(CONFIGURATION) returns the full configuration tree
    // including all class-specific Audio Control descriptors. 4 KB is far
    // more than enough for a CM108-class device.
    let buf = env
        .new_byte_array(4096)
        .map_err(|e| format!("new_byte_array: {}", e))?;
    let buf_ref = JByteArray::from(buf);
    let n = env
        .call_method(
            &connection,
            "controlTransfer",
            "(IIII[BII)I",
            &[
                JValue::Int(REQ_TYPE_STANDARD_IN),
                JValue::Int(REQ_GET_DESCRIPTOR),
                JValue::Int(((DESCRIPTOR_TYPE_CONFIG as i32) << 8) | 0),
                JValue::Int(0),
                (&buf_ref).into(),
                JValue::Int(4096),
                JValue::Int(2000),
            ],
        )
        .and_then(|v| v.i())
        .map_err(|e| format!("GET_DESCRIPTOR: {}", e))?;
    if n < 0 {
        return Err(format!("GET_DESCRIPTOR returned {}", n));
    }
    info!("config descriptor: {} bytes", n);
    let mut desc = vec![0u8; n as usize];
    let signed: Vec<i8> = env
        .convert_byte_array(&buf_ref)
        .map_err(|e| format!("convert_byte_array: {}", e))?
        .into_iter()
        .take(n as usize)
        .map(|b| b as i8)
        .collect();
    for (j, b) in signed.iter().enumerate() {
        desc[j] = *b as u8;
    }

    let (ac_iface, fu_id) = match find_feature_unit_with_volume(&desc) {
        Some(t) => t,
        None => {
            warn!("no Feature Unit with Volume Control found in descriptor");
            return Ok(());
        }
    };
    info!(
        "found Feature Unit: AC iface={} bUnitID={} (target {} dB)",
        ac_iface, fu_id, target_db
    );

    // Note: do NOT claimInterface here. SET_CUR is a control-endpoint-0
    // transfer that does not require a claimed interface, and claiming
    // the AC interface detaches the snd-usb-audio kernel driver, which
    // is what AAudio depends on. Hijacking the interface gave us a
    // silent capture stream (all-zero samples) on the first attempt.

    // Volume value: 1/256 dB units, signed 16-bit little-endian.
    let q8_8 = (target_db * 256.0) as i16;
    let payload: [i8; 2] = [(q8_8 & 0xff) as i8, ((q8_8 >> 8) & 0xff) as i8];
    let payload_jarr = env
        .byte_array_from_slice(&payload.iter().map(|&b| b as u8).collect::<Vec<u8>>())
        .map_err(|e| format!("byte_array_from_slice: {}", e))?;
    let payload_ref = JByteArray::from(payload_jarr);

    // wValue = (control_selector << 8) | channel_number. channel 0 = master.
    let w_value: i32 = (VOLUME_CONTROL << 8) | 0x00;
    // wIndex = (entity_id << 8) | interface_number.
    let w_index: i32 = ((fu_id as i32) << 8) | (ac_iface as i32);

    let rc = env
        .call_method(
            &connection,
            "controlTransfer",
            "(IIII[BII)I",
            &[
                JValue::Int(UAC_CONTROL_OUT),
                JValue::Int(UAC_SET_CUR),
                JValue::Int(w_value),
                JValue::Int(w_index),
                (&payload_ref).into(),
                JValue::Int(2),
                JValue::Int(2000),
            ],
        )
        .and_then(|v| v.i())
        .map_err(|e| format!("SET_CUR controlTransfer: {}", e))?;
    if rc < 0 {
        warn!(
            "SET_CUR FU_VOLUME (master) returned {} — channel master may be unsupported, will retry per-channel",
            rc
        );
        // Try left channel (0x01) and right channel (0x02) in case the
        // codec doesn't honor master.
        for ch in [0x01i32, 0x02i32] {
            let w_value: i32 = (VOLUME_CONTROL << 8) | ch;
            let rc2 = env
                .call_method(
                    &connection,
                    "controlTransfer",
                    "(IIII[BII)I",
                    &[
                        JValue::Int(UAC_CONTROL_OUT),
                        JValue::Int(UAC_SET_CUR),
                        JValue::Int(w_value),
                        JValue::Int(w_index),
                        (&payload_ref).into(),
                        JValue::Int(2),
                        JValue::Int(2000),
                    ],
                )
                .and_then(|v| v.i())
                .unwrap_or(-1);
            info!("SET_CUR FU_VOLUME ch{} -> rc={}", ch, rc2);
        }
    } else {
        info!(
            "SET_CUR FU_VOLUME master={} dB applied (rc={})",
            target_db, rc
        );
    }

    let _ = env.call_method(&connection, "close", "()V", &[]);
    Ok(())
}
