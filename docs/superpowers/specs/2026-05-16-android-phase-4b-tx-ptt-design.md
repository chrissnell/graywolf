# Android Phase 4b — TX Audio + PTT Plumbing → Over-the-Air Beacons — Design

**Date:** 2026-05-16
**Author:** Chris Snell
**Branch (spec):** `docs/android-phase-4b-spec`
**Builds on:** `.context/2026-05-09-android-phase-3-handoff.md`, phase 4a (GPS in, platform abstraction, UI gating, app icon)

---

## 1. Goal

Make TX work end-to-end on Android. Beacon scheduler, digipeater, iGate, and operator manual-PTT all drive radio TX through the existing Rust modem TX governor; the actuator paths (audio out, PTT wire toggle) are new Android-specific implementations.

When 4b lands: an operator with a Topicon T865 + Digirig (or AIOC) + 2m radio configures a channel, hits go, and the reference station decodes the beacon over RF.

## 2. Scope

In-scope:

1. **AudioTrack TX pump** — Kotlin `AudioTxPump` symmetric to `AudioPump` RX. Streaming mode, USB audio output, fed by Rust modem TX samples via JNI.
2. **PTT actuator path — Rust modem → JNI → Kotlin** — new `ptt_android.rs` PTT trait impl; new JNI export; restore `UsbPttAdapter` to `GraywolfService` lifecycle (POC-D code, stripped in phase 3).
3. **PTT method matrix — comprehensive** — CP2102N RTS, CDC-ACM DTR, CM108 HID GPIO, VOX. All four selectable per channel.
4. **USB device permission flow — hybrid** — manifest intent-filter for auto-attach + `requestUsbPermission` for app-already-running. Curated `device_filter.xml` (aprsdroid pattern) drawn from `usb-serial-for-android` device IDs.
5. **SPA channel config UX** — PTT method dropdown + Test-PTT toggle next to it. Auto-route audio to first USB output (no per-channel device picker).
6. **Foreground Service type bitmap** — adds `mediaPlayback` + `connectedDevice`. Phase 3 + 4a already have `microphone` + `location`.
7. **Manifest perms** — re-add `FOREGROUND_SERVICE_CONNECTED_DEVICE`. USB perm comes via the intent-filter / `requestUsbPermission`, not a manifest perm.

Out of scope:

| Topic | Goes to |
|---|---|
| iGate counter dashboard fix | 4c (may resolve automatically once TX works) |
| Modem-optional boot rework | 4d |
| Multiple concurrent USB dongles (Digirig + AIOC together) | future polish — single-dongle assumed |
| Per-channel output-device picker | future polish — auto-route to first USB output |
| Bluetooth SPP serial PTT | phase 5c per master design |
| `AudioManager.setCommunicationDevice` system routing (API 31+) | future polish |
| Hot-swap of PTT method while channel is active | future polish — operator stops/starts channel |

## 3. Architecture

```
                    Go child (cmd/graywolf, //go:build android)
                    ┌─────────────────────────────────────────┐
                    │  Beacon scheduler / digi / iGate-to-RF  │
                    │   │                                     │
                    │   ▼                                     │
                    │  modembridge IPC (existing UDS)         │
                    └────────────────┬────────────────────────┘
                                     │ frame to TX
                                     ▼
            Rust modem (in-process to GraywolfService — phase 3)
            ┌────────────────────────────────────────────────────┐
            │  TX governor                                        │
            │   ├── CSMA (reads in-process DCD from RX demod)     │
            │   ├── Pre-key delay                                 │
            │   ├── PCM render (HDLC encode → AFSK mod)           │
            │   └── Tail delay                                    │
            │                                                     │
            │  Two outputs during a TX cycle:                     │
            │                                                     │
            │   ┌─ PTT key/unkey ────────────────────┐            │
            │   │  via ptt_android.rs trait impl     │            │
            │   │  ↓ JNI: pttSet(method, keyed)      │            │
            │   │                                    │            │
            │   └─ PCM samples ─────────────────┐    │            │
            │      ↓ JNI: txPushSamples(buf,n)  │    │            │
            └────────────────────────────────┼──┼────────────────┘
                                             │  │
                                             ▼  ▼
               GraywolfService — Kotlin lifecycle owner
               ┌──────────────────────────────────────────────────┐
               │  UsbPttAdapter (POC-D code, restored to scope)   │
               │    .pttSet(method, keyed)                        │
               │      ├── CP2102N RTS → usb-serial-for-android    │
               │      ├── CDC-ACM DTR → usb-serial-for-android    │
               │      ├── CM108 HID  → UsbDeviceConnection +      │
               │      │                 HID Set_Report            │
               │      └── VOX        → no-op                      │
               │                                                  │
               │  AudioTxPump (NEW — mirror of AudioPump RX)      │
               │    AudioTrack(MODE_STREAM)                       │
               │    setPreferredDevice(first TYPE_USB_DEVICE out) │
               │    .pushSamples(short[] buf, int n)              │
               └──────────────────────────────────────────────────┘
                                       │
                                       │ USB audio out + PTT wire
                                       ▼
                              Digirig / AIOC / CM108
                                       │
                                       ▼
                                      Radio TX → RF
```

### 3.1 Lifecycle

`GraywolfService.onCreate` extends (4b adds the last two):

1. (existing) modem cdylib boot
2. (existing) AudioPump start (gated on RECORD_AUDIO)
3. (existing) PlatformServer.start
4. (existing) GoLauncher.exec
5. (4a) `gpsAdapter.start()` if FINE_LOCATION granted
6. **(4b) `audioTxPump.start()` — AudioTrack idles in PLAY state**
7. **(4b) `usbPttAdapter.enumerate()` — registers `UsbManager` `BroadcastReceiver`; if devices already plugged, calls `requestUsbPermission` per device**

`onDestroy` reverses: `audioTxPump.stop()` + `usbPttAdapter.closeAll()` before existing teardown.

`UsbPttAdapter` was in tree from POC-D and stripped from app lifecycle in phase 3 (`refactor(android): drop UsbPttAdapter from Activity/Service/App lifecycles`). 4b restores its Service-scope lifecycle. The class itself is largely unchanged from POC-D — same per-transport `data class` handles, same broadcast-receiver permission flow.

### 3.2 AudioTxPump

`android/app/src/main/kotlin/com/nw5w/graywolf/audio/AudioTxPump.kt`:

```kotlin
package com.nw5w.graywolf.audio

import android.content.Context
import android.media.AudioAttributes
import android.media.AudioFormat
import android.media.AudioManager
import android.media.AudioTrack
import android.util.Log

class AudioTxPump(private val ctx: Context) {
    @Volatile private var track: AudioTrack? = null
    @Volatile private var routedDevice: String = "<none>"

    fun start(sampleRate: Int = 22050) {
        if (track != null) return

        val bufBytes = AudioTrack.getMinBufferSize(
            sampleRate,
            AudioFormat.CHANNEL_OUT_MONO,
            AudioFormat.ENCODING_PCM_16BIT,
        ) * 4
        check(bufBytes > 0) { "AudioTrack.getMinBufferSize=$bufBytes" }

        val t = AudioTrack.Builder()
            .setAudioAttributes(AudioAttributes.Builder()
                .setUsage(AudioAttributes.USAGE_MEDIA)
                .setContentType(AudioAttributes.CONTENT_TYPE_MUSIC)
                .build())
            .setAudioFormat(AudioFormat.Builder()
                .setEncoding(AudioFormat.ENCODING_PCM_16BIT)
                .setSampleRate(sampleRate)
                .setChannelMask(AudioFormat.CHANNEL_OUT_MONO)
                .build())
            .setBufferSizeInBytes(bufBytes)
            .setTransferMode(AudioTrack.MODE_STREAM)
            .build()

        // Auto-route to first USB audio output (Q5 decision A).
        val am = ctx.getSystemService(AudioManager::class.java)
        val usbOut = am?.getDevices(AudioManager.GET_DEVICES_OUTPUTS)
            ?.firstOrNull { it.type == android.media.AudioDeviceInfo.TYPE_USB_DEVICE }
        if (usbOut != null) {
            t.setPreferredDevice(usbOut)
            routedDevice = usbOut.productName?.toString() ?: "USB device"
            Log.i(TAG, "AudioTxPump routed to USB output: $routedDevice")
        } else {
            routedDevice = "system default (no USB audio dongle found)"
            Log.w(TAG, "AudioTxPump: $routedDevice")
        }

        t.play()
        track = t
        Log.i(TAG, "AudioTxPump init rate=$sampleRate bufBytes=$bufBytes routed=$routedDevice")
    }

    /**
     * Called from Rust modem via JNI on every TX PCM frame.
     * Blocking write — Rust modem TX thread is OK to block while audio drains.
     */
    fun pushSamples(samples: ShortArray, count: Int): Int {
        val t = track ?: return -1
        return t.write(samples, 0, count, AudioTrack.WRITE_BLOCKING)
    }

    fun stop() {
        val t = track ?: return
        try { t.stop() } catch (_: Throwable) {}
        t.release()
        track = null
    }

    companion object { private const val TAG = "AudioTxPump" }
}
```

Stays in PLAY state from Service boot. PCM only flows when the Rust modem TX governor pushes samples; idle = silent buffer drain. Mirror of `AudioPump` shape exactly (same sample rate, same encoding, same buffer-size multiplier).

### 3.3 PTT actuator — Rust trait impl

New `graywolf-modem/src/tx/ptt_android.rs`, gated `#[cfg(target_os = "android")]`:

```rust
use crate::tx::ptt::{Ptt, PttError};

// PttMethod values match proto/platform.proto PttMethod enum + Kotlin
// UsbPttAdapter's transport tags. Kept in lockstep manually for 4b.
// Promote to a generated enum once we stop being lazy.
const PTT_METHOD_UNKNOWN: i32 = 0;
const PTT_METHOD_CP2102N_RTS: i32 = 1;
const PTT_METHOD_CM108_HID: i32 = 2;
const PTT_METHOD_AIOC_CDC_DTR: i32 = 3;
const PTT_METHOD_VOX: i32 = 4;

pub struct AndroidPtt {
    method: i32,
}

impl AndroidPtt {
    pub fn new(method: i32) -> Self { Self { method } }
}

impl Ptt for AndroidPtt {
    fn key(&mut self) -> Result<(), PttError> {
        unsafe {
            crate::android::jni_ptt_set(self.method, true)
                .map_err(|e| PttError::IoError(format!("JNI ptt key: {e}")))
        }
    }
    fn unkey(&mut self) -> Result<(), PttError> {
        unsafe {
            crate::android::jni_ptt_set(self.method, false)
                .map_err(|e| PttError::IoError(format!("JNI ptt unkey: {e}")))
        }
    }
}
```

`graywolf-modem/src/android/mod.rs` adds:

```rust
// JNI bridge — Kotlin installs the callback on cdylib load.
// Stored as global Mutex<Option<JStaticMethodID>> per phase-3 pattern.
static PTT_SET_CALLBACK: Mutex<Option<...>> = Mutex::new(None);

pub(crate) unsafe fn jni_ptt_set(method: i32, keyed: bool) -> Result<(), String> {
    // Look up cached JNIEnv (phase 3's run_demod owns one).
    // Call the cached static method.
    // Return Err on attach/call failure.
}

#[no_mangle]
pub extern "system" fn Java_com_nw5w_graywolf_jni_ModemBridge_installPttCallback(
    env: JNIEnv, _class: JClass, callback: JObject,
) {
    // Resolve callback's `pttSet(int method, boolean keyed)` method id,
    // stash in PTT_SET_CALLBACK.
}
```

Channel config in Rust modem grows a `ptt: PttConfig::Android { method: i32 }` variant; the existing PttConfig enum already has `Cm108`, `Cp210x`, `RigCtld` desktop variants. Android variant constructs `AndroidPtt::new(method)`.

### 3.4 PTT actuator — Kotlin side

`UsbPttAdapter` is in tree from POC-D (`android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt`). 4b changes:

1. **Reactivate Service lifecycle wiring.** `GraywolfService.onCreate` calls `usbPttAdapter.enumerate()`; `onDestroy` calls `closeAll()`.
2. **New JNI entry** — `ModemBridge.installPttCallback(adapter: UsbPttCallback)`. Called from `GraywolfService.onCreate` after `loadLibrary` and before modem boot.

```kotlin
// android/app/src/main/kotlin/com/nw5w/graywolf/jni/ModemBridge.kt
@JvmStatic external fun installPttCallback(cb: UsbPttCallback)

interface UsbPttCallback {
    fun pttSet(method: Int, keyed: Boolean): Boolean
}
```

3. **`UsbPttAdapter` implements `UsbPttCallback`.** Dispatches on method:

```kotlin
override fun pttSet(method: Int, keyed: Boolean): Boolean {
    return when (method) {
        PTT_METHOD_CP2102N_RTS -> cp2102n?.setRts(keyed) ?: false
        PTT_METHOD_AIOC_CDC_DTR -> aioc?.setDtr(keyed) ?: false
        PTT_METHOD_CM108_HID -> cm108?.setHidGpio(CM108_BIT, keyed) ?: false
        PTT_METHOD_VOX -> true  // no-op; audio drives VOX
        else -> { Log.w(TAG, "pttSet unknown method=$method"); false }
    }
}
```

POC-D already implemented `setRts` (CP2102N), `setHidGpio` (CM108), `setDtr` (AIOC). 4b doesn't re-derive these — restores the lifecycle wiring + adds the JNI bridge.

### 3.5 Manual PTT path

SPA "Test PTT" toggle drives the same path as production:

```
SPA Test toggle pressed
  → POST /api/channels/{id}/ptt {keyed: true}
    → Go modembridge: modem.ManualPtt(channelId, true)
      → Rust modem: tx_governor::manual_key(channel)
        → Rust modem: AndroidPtt::key()
          → JNI pttSet(method, true)
            → Kotlin UsbPttAdapter.pttSet(method, true)
              → wire toggles, radio keys
```

Unkey is symmetric on toggle release. Manual key holds PTT until release; if SPA disconnects mid-key (page reload, network drop), Rust modem has a watchdog timeout (~10s) that auto-unkeys to prevent stuck PTT.

### 3.6 USB device permission flow

**Manifest changes:**

```xml
<uses-permission android:name="android.permission.FOREGROUND_SERVICE_CONNECTED_DEVICE"/>

<activity android:name=".MainActivity" ...>
    <intent-filter>
        <action android:name="android.hardware.usb.action.USB_DEVICE_ATTACHED"/>
    </intent-filter>
    <meta-data
        android:name="android.hardware.usb.action.USB_DEVICE_ATTACHED"
        android:resource="@xml/device_filter"/>
</activity>

<service
    android:name=".GraywolfService"
    android:foregroundServiceType="microphone|location|mediaPlayback|connectedDevice"
    .../>
```

**`res/xml/device_filter.xml`** — curated list. See Appendix A for the exact ~15-entry VID/PID payload.

**WebAppInterface extensions:**

```kotlin
@JavascriptInterface
fun listUsbDevices(): String = adapter.enumerate().toJsonString()

@JavascriptInterface
fun requestUsbPermission(vid: Int, pid: Int, callbackId: String) {
    adapter.requestPermission(vid, pid) { granted ->
        webView.post {
            webView.evaluateJavascript(
                "window.__usbResult($callbackId, $granted)", null,
            )
        }
    }
}
```

These mirror POC-D's `requestUsbPermission` shape. SPA channel config polls `listUsbDevices()` to populate device chooser; if the operator's chosen device isn't `granted`, SPA calls `requestUsbPermission` and waits on the `__usbResult` callback.

### 3.7 SPA channel config UX

`web/src/routes/Channels.svelte` form additions for Android:

```
┌─ Channel "Main 2m" ──────────────────────────────────────┐
│  Name              [Main 2m_______________]              │
│  Frequency         [144.390 MHz___________]              │
│  Modem             [AFSK 1200 ▾]                         │
│                                                          │
│  PTT method        [CP2102N RTS ▾]    [⚡ Test PTT]       │
│  TX delay (ms)     [200_____]                            │
│  Tail delay (ms)   [100_____]                            │
│  Repeats           [1_______]                            │
│                                                          │
│  USB hardware status: Digirig (Granted ✓)                │
│  Audio routing:       Burr-Brown USB Audio (auto)        │
│                                                          │
│  [Save] [Cancel]                                         │
└──────────────────────────────────────────────────────────┘
```

- **PTT method dropdown**: CP2102N RTS / CDC-ACM DTR / CM108 HID / VOX
- **Test PTT** button is press-and-hold. While pressed, sends `POST /api/channels/{id}/ptt {keyed:true}` and re-sends `keyed:true` every 2s (watchdog liveness). On release sends `keyed:false`.
- **USB hardware status** auto-polls `listUsbDevices()` every 2s, shows the matched device + permission state. Permission-not-granted state shows a `[Grant access]` button that triggers `requestUsbPermission`.
- **Audio routing** is read-only display of the auto-selected USB output (or "system default" warning if none found).

Channel form fields **un-hide on Android in 4b**: TX delay, Tail delay, Repeats, "Packets TX" stat. These were always visible per phase 4a's "don't hide things that will work" rule; 4b just makes them functional.

`output_device_id` stays hidden (auto-route per Q5).

### 3.8 Beacon failure UX

Failure cases + handling:

| Case | Handling |
|---|---|
| No USB device attached when TX fires | Rust modem TX governor logs `tx_failed: no usb device`; beacon counted as failed in existing TX-stats; next beacon retries |
| USB device attached but permission not granted | Same as above; `usb_perm_required` flag set in `/api/channels/{id}/status`; SPA channel card shows "Grant access" call-to-action |
| PTT actuator returns error (USB transient) | Logged; counted; beacon dropped this cycle |
| AudioTrack write returns error | Logged; counted; beacon dropped this cycle |
| AudioTrack auto-routed to system default (no USB out) | One-time WARN at Service boot; per-beacon WARN with rate limit |

The dashboard's existing TX counter on Channels page becomes the failure-visible surface. New `tx_errors_24h` field added to ChannelStats (additive); rendered next to the TX counter when > 0.

## 4. Proto changes

None required.

Phase 2's `PttKeyRequest` / `PttUnkeyRequest` / `PttAck` oneof variants remain unused in 4b production (PTT actuation goes via JNI, not proto). Keep defined — additive proto, no wire cost. Future iOS port or headless-test path may use them.

Phase 4a's `GpsFix.accuracy_m` + `GnssStatusUpdate` are independent. No interaction.

## 5. File structure

### 5.1 New files

| Path | Purpose |
|---|---|
| `android/app/src/main/kotlin/com/nw5w/graywolf/audio/AudioTxPump.kt` | AudioTrack TX mirror of AudioPump |
| `android/app/src/main/kotlin/com/nw5w/graywolf/audio/AudioTxPumpTest.kt` | unit test (mock AudioTrack) |
| `android/app/src/main/res/xml/device_filter.xml` | USB intent-filter VID/PID list (Appendix A) |
| `graywolf-modem/src/tx/ptt_android.rs` | Android PTT trait impl that JNI-calls Kotlin |
| `graywolf-modem/src/tx/ptt_android_test.rs` | Rust unit test with mock JNI |
| `web/src/routes/Channels.android.ptt.svelte` (or in-component `{#if Platform.kind === 'android'}` split) | PTT method dropdown + Test toggle |

### 5.2 Modified files

| Path | Change |
|---|---|
| `android/app/src/main/AndroidManifest.xml` | re-add FOREGROUND_SERVICE_CONNECTED_DEVICE, USB intent-filter on MainActivity, FGS type bitmap gains mediaPlayback + connectedDevice |
| `android/app/src/main/kotlin/com/nw5w/graywolf/GraywolfService.kt` | onCreate adds audioTxPump.start + usbPttAdapter.enumerate; onDestroy adds audioTxPump.stop + usbPttAdapter.closeAll; installs PTT callback into ModemBridge after loadLibrary |
| `android/app/src/main/kotlin/com/nw5w/graywolf/jni/ModemBridge.kt` | add `installPttCallback(cb)` and `UsbPttCallback` interface; add `txPushSamples(buf, n)` |
| `android/app/src/main/kotlin/com/nw5w/graywolf/usb/UsbPttAdapter.kt` | implement `UsbPttCallback`; minor — POC-D shape mostly intact |
| `android/app/src/main/kotlin/com/nw5w/graywolf/webview/WebAppInterface.kt` | add `listUsbDevices` + `requestUsbPermission` JS-bridge methods (restore POC-D shape) |
| `graywolf-modem/src/android/mod.rs` | add JNI bridge for ptt_set callback storage + invocation; add txPushSamples export |
| `graywolf-modem/src/android/audio.rs` | add `tx_emit_samples(buf)` that JNI-calls Kotlin `AudioTxPump.pushSamples` |
| `graywolf-modem/src/tx/mod.rs` | register `ptt_android::AndroidPtt` in PttConfig dispatch |
| `graywolf-modem/src/tx/ptt.rs` | add `PttConfig::Android { method: i32 }` variant |
| `pkg/modembridge/...` | route operator manual-PTT REST through to modem (new `ManualPtt(channelId, keyed)` RPC); modem replies with PttAck-shaped response |
| `pkg/app/...` | wire `/api/channels/{id}/ptt` REST endpoint |
| `web/src/routes/Channels.svelte` | PTT method dropdown + Test toggle (Android branch) |
| `web/src/lib/api.js` | `postChannelPtt(channelId, keyed)` |

### 5.3 Untouched (intentional)

- `pkg/beacon/`, `pkg/digipeater/`, `pkg/igate/` — already produce TX frames via modembridge on desktop; the new Android TX path is transparent to them
- Rust modem demod (RX) — unchanged
- `pkg/gps/` from phase 4a — unchanged
- proto/platform.proto — unchanged (no new messages)

## 6. Test plan

### 6.1 Kotlin unit

- `AudioTxPumpTest`:
  - given device with no USB output, AudioTxPump starts with system default + WARN logged
  - given mocked AudioManager returning a TYPE_USB_DEVICE, AudioTxPump calls `setPreferredDevice(usbDevice)`
  - `pushSamples` after `stop()` returns -1 (no crash)
  - `start()` is idempotent
- `UsbPttAdapter.pttSet`:
  - dispatches to `cp2102n.setRts` for `PTT_METHOD_CP2102N_RTS`
  - dispatches to `aioc.setDtr` for `PTT_METHOD_AIOC_CDC_DTR`
  - dispatches to `cm108.setHidGpio(bit=3)` for `PTT_METHOD_CM108_HID`
  - returns true and writes nothing for `PTT_METHOD_VOX`
  - returns false + WARN for unknown method

### 6.2 Rust unit

- `ptt_android_test`:
  - `AndroidPtt::key()` invokes the registered JNI callback with `(method, true)`
  - `AndroidPtt::unkey()` invokes with `(method, false)`
  - error from JNI callback propagates as `PttError::IoError`

### 6.3 Go unit

- `pkg/modembridge::TestManualPtt`:
  - REST endpoint forwards keyed=true/false to modem RPC
  - watchdog auto-unkeys after 10s of no liveness ping

### 6.4 SPA unit (node:test)

- `Channels.android.ptt.test.js`:
  - dropdown renders 4 options on Android
  - Test PTT toggle press calls `postChannelPtt(id, true)`; release calls `postChannelPtt(id, false)`
  - Held toggle pings every 2s

### 6.5 Live device test (T865 + Digirig + reference station)

- **CP2102N RTS path:** configure channel, fire one beacon, reference station decodes → ✅
- **CDC-ACM DTR path:** swap to AIOC, reconfigure channel, fire beacon, decode → ✅
- **CM108 HID path:** configure with CM108 HID (any wired-GPIO CM108 dongle if available); skip if only Digirig (Digirig CM108 GPIO unwired per POC-D finding)
- **VOX path:** configure VOX, radio configured for VOX, fire beacon, decode → ✅
- **Manual Test PTT:** press toggle, radio keys (sidetone), release, radio unkeys cleanly
- **Manual PTT watchdog:** press toggle then kill app (force-stop); confirm modem auto-unkeys within 10s (no stuck PTT)
- **USB auto-attach:** force-stop app, unplug Digirig, replug → "Open with graywolf?" dialog appears
- **USB pre-plug launch:** force-stop app, leave Digirig plugged, tap launcher icon → app gets permission prompt
- **iGate-to-RF gating:** confirm gated packets transmit + dashboard counter increments (sanity for phase 4c)

### 6.6 Cross-compile + assemble

- Go: `GOWORK=off go test ./...` clean (desktop)
- Go: `GOOS=android GOARCH=arm64 CGO_ENABLED=0 GOWORK=off go build ./...` clean
- Rust: `cargo test --target x86_64-unknown-linux-gnu --features android-test-stub` clean (host harness with mock JNI)
- Rust: `cargo ndk -t arm64-v8a -t x86_64 -P 28 build --lib --release` clean
- Kotlin: `gradle :app:testDebugUnitTest` green
- `gradle clean :app:assembleDebug` produces APK; install on T865; execute §6.5

## 7. Definition-of-done criteria

| # | Criterion | How verified |
|---|-----------|--------------|
| 1 | AudioTxPump pushes PCM to USB audio output | logcat marker + AudioTxPumpTest |
| 2 | Rust modem `AndroidPtt::key/unkey` invokes Kotlin via JNI | Rust unit test + logcat |
| 3 | UsbPttAdapter dispatches to correct transport per method | Kotlin unit test |
| 4 | CP2102N RTS path produces an audible beacon decoded by reference station | live test |
| 5 | CDC-ACM DTR path produces an audible beacon decoded by reference station | live test |
| 6 | VOX path produces an audible beacon decoded by reference station | live test |
| 7 | Test-PTT toggle on channel config keys + unkeys cleanly | manual UI |
| 8 | Manual PTT watchdog auto-unkeys after 10s of no liveness | force-stop test |
| 9 | USB intent-filter auto-attach prompts on fresh plug-in | manual UX |
| 10 | `requestUsbPermission` flow works when app launched first | manual UX |
| 11 | iGate gating to RF produces decoded packets on reference station | live test (also closes most of phase 4c) |
| 12 | All unit + integration tests green; APK assembles clean | CI + gradle |

## 8. Open questions / decisions deferred

| # | Question | Resolution |
|---|---|---|
| 1 | How to handle two USB dongles plugged simultaneously | YAGNI — single-dongle assumed in 4b. Bug filed → upgrade later |
| 2 | When does AudioTxPump rebind if USB device is hot-swapped mid-session | `AudioManager.AudioDeviceCallback` listens for `onAudioDevicesAdded`; AudioTxPump rebinds `setPreferredDevice` on USB output appear/disappear |
| 3 | Manual PTT watchdog timeout value | 10s as starting point; revisit after live trials. Configurable in modem TX governor settings |
| 4 | Should iGate counter fix land in 4b? | Out of scope per phase split. iGate verification in §6.5 will surface whether 4c is still needed or whether the counter starts working once TX exists |
| 5 | Is the `mediaPlayback` FGS type sufficient for AudioTrack? Or do we also need `dataSync`? | Android 14 docs say `mediaPlayback` covers `AudioTrack`. Verify on T865; if AudioTrack throws SecurityException, add `dataSync` |

## 9. Phase ordering note

```
4a (GPS) ──► 4b (this spec, TX + PTT) ──► 4c (iGate counter, if still needed)
                            ↘ 4d (modem-optional boot, low priority)
```

4b is independent of 4d. The "modem always boots" assumption of phase 3 + 4a remains in 4b — operators with no radio configured still see modem boot up, just no TX activity.

---

## Appendix A: device_filter.xml VID/PID payload

Derived from `usb-serial-for-android` `com.felhr.deviceids` filtered to chips we actually use for PTT (CP210x, FTDI, CH34x, CDC-ACM) plus CM108-family HID dongles. Bottom of the file pins the explicit known-good entries first for review-time clarity.

```xml
<?xml version="1.0" encoding="utf-8"?>
<resources>
  <!-- Known-good (verified on operator bench, POC-D) -->
  <usb-device vendor-id="4292"  product-id="60000" /> <!-- 0x10C4:0xEA60 Digirig CP2102N -->
  <usb-device vendor-id="3468"  product-id="18"    /> <!-- 0x0D8C:0x0012 Digirig CM108 -->
  <usb-device vendor-id="4617"  product-id="29576" /> <!-- 0x1209:0x7388 AIOC -->

  <!-- CP210x family (Silicon Labs) -->
  <usb-device vendor-id="4292"  product-id="60016" /> <!-- 0x10C4:0xEA70 CP2105 -->
  <usb-device vendor-id="4292"  product-id="60017" /> <!-- 0x10C4:0xEA71 CP2108 -->

  <!-- FTDI family -->
  <usb-device vendor-id="1027"  product-id="24577" /> <!-- 0x0403:0x6001 FT232R -->
  <usb-device vendor-id="1027"  product-id="24592" /> <!-- 0x0403:0x6010 FT2232H -->
  <usb-device vendor-id="1027"  product-id="24593" /> <!-- 0x0403:0x6011 FT4232H -->
  <usb-device vendor-id="1027"  product-id="24596" /> <!-- 0x0403:0x6014 FT232H -->
  <usb-device vendor-id="1027"  product-id="24597" /> <!-- 0x0403:0x6015 FT231X -->

  <!-- CH34x family (QinHeng) -->
  <usb-device vendor-id="6790"  product-id="29987" /> <!-- 0x1A86:0x7523 CH340 -->
  <usb-device vendor-id="6790"  product-id="21795" /> <!-- 0x1A86:0x5523 CH341 -->

  <!-- Prolific PL2303 -->
  <usb-device vendor-id="1659"  product-id="8963"  /> <!-- 0x067B:0x2303 PL2303 -->

  <!-- Generic CDC-ACM (covers AIOC variants + others) -->
  <usb-device class="2" subclass="2" />
</resources>
```

The trailing `class="2" subclass="2"` is the CDC-ACM class match — catches any future CDC-ACM device without us having to add the VID/PID explicitly. Belt-and-suspenders against AIOC firmware revisions or clones.

## Appendix B: PTT method enum keep-in-sync table

| Name | Rust const | Kotlin const | proto enum value | Notes |
|---|---|---|---|---|
| UNKNOWN | `PTT_METHOD_UNKNOWN = 0` | `0` | `PTT_METHOD_UNKNOWN = 0` | error / unset |
| CP2102N RTS | `PTT_METHOD_CP2102N_RTS = 1` | `1` | `PTT_METHOD_CP2102N_RTS = 1` | Digirig |
| CM108 HID | `PTT_METHOD_CM108_HID = 2` | `2` | `PTT_METHOD_CM108_HID = 2` | wired-GPIO CM108 dongles only |
| CDC-ACM DTR | `PTT_METHOD_AIOC_CDC_DTR = 3` | `3` | `PTT_METHOD_AIOC_CDC_DTR = 3` | AIOC (per `feedback_aioc_ptt_cdc_acm_dtr`) |
| VOX | `PTT_METHOD_VOX = 4` | `4` | `PTT_METHOD_VOX = 4` | no PTT wire |

Drift hazard: this table is the only canonical mapping. Add an integration test that asserts all three sources produce the same int → name mapping at runtime (Go via proto reflection, Kotlin via reflection on the consts object, Rust via match-arm coverage).
