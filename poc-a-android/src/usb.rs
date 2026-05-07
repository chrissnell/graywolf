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

use std::sync::Arc;

use jni::objects::{JByteArray, JObject, JString, JValue};
use jni::JavaVM;
use log::{info, warn};

const USB_AUDIO_CLASS: i32 = 0x01;
// USB Audio Class spec, table A-9: SET_CUR request.
const UAC_SET_CUR: i32 = 0x01;
const UAC_GET_CUR: i32 = 0x81;
const UAC_GET_MIN: i32 = 0x82;
const UAC_GET_MAX: i32 = 0x83;
const UAC_GET_RES: i32 = 0x84;
// Class-specific request, recipient interface, host-to-device.
const UAC_CONTROL_OUT: i32 = 0x21;
// Class-specific request, recipient interface, device-to-host.
const UAC_CONTROL_IN: i32 = 0xa1;
// Volume Control Selector inside a Feature Unit (UAC1 spec, Appendix A.10).
const VOLUME_CONTROL: i32 = 0x02;
const MUTE_CONTROL: i32 = 0x01;
// USB Audio Class descriptor types (Appendix A.4).
const CS_INTERFACE: u8 = 0x24;
const FEATURE_UNIT: u8 = 0x06;
// Standard descriptor type for "Configuration".
const DESCRIPTOR_TYPE_CONFIG: u8 = 0x02;
const REQ_GET_DESCRIPTOR: i32 = 0x06;
const REQ_TYPE_STANDARD_IN: i32 = 0x80;

/// Description of one Feature Unit found in the descriptor.
#[derive(Debug)]
struct FuInfo {
    ac_iface: u8,
    unit_id: u8,
    source_id: u8,
    bma_master: u8,
}

/// Walk the configuration descriptor and list every Feature Unit with
/// Volume Control on the master channel that lives inside an Audio
/// Control interface (class 0x01, sub 0x01).
fn find_all_feature_units(desc: &[u8]) -> Vec<FuInfo> {
    let mut out = Vec::new();
    let mut i = 0;
    let mut current_ac_iface: Option<u8> = None;
    while i + 2 <= desc.len() {
        let b_length = desc[i] as usize;
        if b_length < 2 || i + b_length > desc.len() {
            break;
        }
        let b_descriptor_type = desc[i + 1];
        if b_descriptor_type == 0x04 && b_length >= 9 {
            let n = desc[i + 2];
            let c = desc[i + 5];
            let s = desc[i + 6];
            current_ac_iface = if c == 0x01 && s == 0x01 { Some(n) } else { None };
        }
        if b_descriptor_type == CS_INTERFACE && b_length >= 7 && desc[i + 2] == FEATURE_UNIT {
            if let Some(ac_iface) = current_ac_iface {
                let unit_id = desc[i + 3];
                let source_id = desc[i + 4];
                let b_control_size = desc[i + 5] as usize;
                if b_control_size > 0 && 6 + b_control_size <= b_length {
                    let bma_master = desc[i + 6];
                    if bma_master & 0x02 != 0 {
                        out.push(FuInfo {
                            ac_iface,
                            unit_id,
                            source_id,
                            bma_master,
                        });
                    }
                }
            }
        }
        i += b_length;
    }
    out
}

/// Diagnostic-only: list USB devices and their class topology so the
/// run report can document what graywolf-modem sees on this tablet.
/// We do not attempt FU_VOLUME control here — once the audio HAL claims
/// the USB-Audio device for AudioRecord, control transfers from
/// user-space are refused (rc=-1 on every SET_CUR attempt).
pub fn enumerate_only(vm: Arc<JavaVM>, activity_ptr_usize: usize) -> Result<(), String> {
    enumerate_and_set_volume(vm, activity_ptr_usize, 0.0)?;
    Ok(())
}

#[allow(dead_code)]
fn enumerate_and_set_volume(
    vm: Arc<JavaVM>,
    activity_ptr_usize: usize,
    target_db: f32,
) -> Result<(), String> {
    let activity_ptr = activity_ptr_usize as jni::sys::jobject;
    if activity_ptr.is_null() {
        return Err("activity ptr is null".into());
    }
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

    let fus = find_all_feature_units(&desc);
    if fus.is_empty() {
        warn!("no Feature Unit with Volume Control found in descriptor");
        return Ok(());
    }
    info!("found {} Feature Unit(s) with Volume Control:", fus.len());
    for fu in &fus {
        info!(
            "  FU: ac_iface={} bUnitID={} bSourceID={} bma_master=0x{:02x}",
            fu.ac_iface, fu.unit_id, fu.source_id, fu.bma_master
        );
    }
    // Probe each FU's MIN/MAX/CUR so we can identify which is the
    // capture path (typically the one whose range goes negative).
    for fu in &fus {
        let probe_w_value: i32 = (VOLUME_CONTROL << 8) | 0x00;
        let probe_w_index: i32 = ((fu.unit_id as i32) << 8) | (fu.ac_iface as i32);
        let mut snapshot = String::new();
        for (label, req) in [
            ("MIN", UAC_GET_MIN),
            ("MAX", UAC_GET_MAX),
            ("CUR", UAC_GET_CUR),
        ] {
            let arr = match env.new_byte_array(2) {
                Ok(a) => JByteArray::from(a),
                Err(_) => continue,
            };
            let rc = env
                .call_method(
                    &connection,
                    "controlTransfer",
                    "(IIII[BII)I",
                    &[
                        JValue::Int(UAC_CONTROL_IN),
                        JValue::Int(req),
                        JValue::Int(probe_w_value),
                        JValue::Int(probe_w_index),
                        (&arr).into(),
                        JValue::Int(2),
                        JValue::Int(2000),
                    ],
                )
                .and_then(|v| v.i())
                .unwrap_or(-1);
            if rc < 2 {
                snapshot.push_str(&format!(" {}=ERR", label));
                continue;
            }
            let bytes = env
                .convert_byte_array(&arr)
                .map(|v| v.into_iter().take(2).collect::<Vec<u8>>())
                .unwrap_or_default();
            if bytes.len() == 2 {
                let val = i16::from_le_bytes([bytes[0], bytes[1]]);
                snapshot.push_str(&format!(" {}={:.1}dB", label, val as f32 / 256.0));
            }
        }
        info!("  FU {}:{}", fu.unit_id, snapshot);
    }
    // Apply attenuation across every FU with a negative MIN — emulates
    // Linux's compound `amixer set Capture` which spans the same chain
    // of CM108 Feature Units. Each FU gets pinned to its own MIN so the
    // total path attenuation is the sum.
    info!(
        "applying FU_VOLUME at MIN across all capture FUs (total target {} dB)",
        target_db
    );

    // Note: do NOT claimInterface here. SET_CUR is a control-endpoint-0
    // transfer that does not require a claimed interface, and claiming
    // the AC interface detaches the snd-usb-audio kernel driver that
    // AAudio depends on.

    // Heuristic: pick the FU whose bma_master has the AGC bit (0x40)
    // set — on CM108-class codecs that's the microphone capture FU
    // (source feeding the ADC), which is the right place to attenuate
    // to keep the demod's audio out of clipping. The other Volume-
    // capable FU is typically the sidetone/monitor path which has no
    // bearing on what the host receives over USB-Audio Streaming.
    let mic_fu = fus
        .iter()
        .find(|fu| fu.bma_master & 0x40 != 0)
        .or_else(|| fus.first());
    let mic_fu = match mic_fu {
        Some(f) => f,
        None => {
            warn!("no candidate Feature Unit; skipping volume set");
            return Ok(());
        }
    };
    info!(
        "selected mic FU: bUnitID={} (bma_master=0x{:02x})",
        mic_fu.unit_id, mic_fu.bma_master
    );

    // Belt-and-suspenders: clear MUTE on every volume-capable FU before
    // touching volume. Some CM108-class codecs flip an internal mute bit
    // when SET_CUR Volume gets a value outside the legal range, and a
    // subsequent in-range SET_CUR doesn't auto-clear it.
    for fu in &fus {
        if fu.bma_master & 0x01 == 0 {
            continue;
        }
        let mute_payload: Vec<u8> = vec![0u8];
        let arr = match env.byte_array_from_slice(&mute_payload) {
            Ok(a) => JByteArray::from(a),
            Err(_) => continue,
        };
        let w_value: i32 = (MUTE_CONTROL << 8) | 0x00;
        let w_index: i32 = ((fu.unit_id as i32) << 8) | (fu.ac_iface as i32);
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
                    (&arr).into(),
                    JValue::Int(1),
                    JValue::Int(2000),
                ],
            )
            .and_then(|v| v.i())
            .unwrap_or(-1);
        info!("FU {} MUTE=0 rc={}", fu.unit_id, rc);
    }

    // Restore the other FUs to a neutral 0 dB so prior debug runs that
    // pushed them out of band don't leave the device in a silent state.
    for fu in &fus {
        if fu.unit_id == mic_fu.unit_id {
            continue;
        }
        let q8_8: i16 = 0;
        let payload: Vec<u8> = vec![(q8_8 & 0xff) as u8, ((q8_8 >> 8) & 0xff) as u8];
        let payload_jarr = match env.byte_array_from_slice(&payload) {
            Ok(a) => JByteArray::from(a),
            Err(_) => continue,
        };
        let w_value: i32 = (VOLUME_CONTROL << 8) | 0x00;
        let w_index: i32 = ((fu.unit_id as i32) << 8) | (fu.ac_iface as i32);
        let _ = env
            .call_method(
                &connection,
                "controlTransfer",
                "(IIII[BII)I",
                &[
                    JValue::Int(UAC_CONTROL_OUT),
                    JValue::Int(UAC_SET_CUR),
                    JValue::Int(w_value),
                    JValue::Int(w_index),
                    (&payload_jarr).into(),
                    JValue::Int(2),
                    JValue::Int(2000),
                ],
            )
            .and_then(|v| v.i());
        info!("FU {} reset to 0 dB (non-mic FU)", fu.unit_id);
    }

    // Clamp target into mic FU's range. Apply.
    let probe_w_value: i32 = (VOLUME_CONTROL << 8) | 0x00;
    let probe_w_index: i32 = ((mic_fu.unit_id as i32) << 8) | (mic_fu.ac_iface as i32);
    let arr_min = JByteArray::from(env.new_byte_array(2).unwrap());
    let _ = env
        .call_method(
            &connection,
            "controlTransfer",
            "(IIII[BII)I",
            &[
                JValue::Int(UAC_CONTROL_IN),
                JValue::Int(UAC_GET_MIN),
                JValue::Int(probe_w_value),
                JValue::Int(probe_w_index),
                (&arr_min).into(),
                JValue::Int(2),
                JValue::Int(2000),
            ],
        )
        .and_then(|v| v.i());
    let min_val = env
        .convert_byte_array(&arr_min)
        .map(|v| {
            if v.len() >= 2 {
                i16::from_le_bytes([v[0], v[1]]) as f32 / 256.0
            } else {
                target_db
            }
        })
        .unwrap_or(target_db);
    let applied_db = target_db.max(min_val);
    let q8_8 = (applied_db * 256.0) as i16;
    let payload: Vec<u8> = vec![(q8_8 & 0xff) as u8, ((q8_8 >> 8) & 0xff) as u8];
    let payload_jarr = match env.byte_array_from_slice(&payload) {
        Ok(a) => JByteArray::from(a),
        Err(_) => return Ok(()),
    };
    let w_value: i32 = (VOLUME_CONTROL << 8) | 0x00;
    let w_index: i32 = ((mic_fu.unit_id as i32) << 8) | (mic_fu.ac_iface as i32);
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
                (&payload_jarr).into(),
                JValue::Int(2),
                JValue::Int(2000),
            ],
        )
        .and_then(|v| v.i())
        .unwrap_or(-1);
    info!(
        "FU {} SET_CUR {:.1} dB rc={} (target {:.1}, min {:.1})",
        mic_fu.unit_id, applied_db, rc, target_db, min_val
    );

    let _ = env.call_method(&connection, "close", "()V", &[]);
    Ok(())
}
