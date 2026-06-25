# Audio Auto-Tuning MCP Server — Design

**Date:** 2026-06-25
**Issue:** GRA-206
**Related:** GRA-130 (per-packet dBFS audio levels, graywolf #324);
`docs/superpowers/plans/2026-06-17-packet-audio-level-dbfs-alignment.md`

---

## 1. Goal

Let an operator point an assistant at their station and have it drive the
receive audio chain to the level that decodes the most packets — automatically
where the chain is software-controllable, and with clear, live, human-readable
guidance where it is not (the radio's volume knob).

When this lands: a user says "tune my packet audio," and the system unmutes the
capture channel if needed, sets the OS capture level and Graywolf's software
gain to land the signal in the decode sweet spot, verifies the result by
actually decoding traffic, and — only if the software knobs can't get there —
tells the user *"turn your radio's volume down a little"* with a live meter that
converges as they turn the knob.

The work splits into three deliverables:

1. A **recording / live-analysis mode** in the existing `graywolf-modem` binary
   (single binary, cross-platform).
2. An **OS audio control layer** (capture level + mute, per platform).
3. An **MCP server** that orchestrates the tuning loop over the Graywolf REST
   API and the OS audio layer, and surfaces guidance to the user/agent.

**Platforms (hard requirement): Linux, macOS, and Windows are all Day-1
first-class** — Graywolf has users on every desktop OS and audio-level pain is
universal, so all three ship in the same release with no "Linux first, others
later" staging (§4.2, §12). **Android is out of scope** (operators don't run MCP
clients from phones), which lets the OS layer target just the three desktop
mixer APIs.

---

## 2. Background — why this is a real problem

Several gain stages sit between the radio and a decoded packet, and they
interact non-linearly. Operators routinely misadjust them, and the existing UI
doesn't tell them how to fix it. The signal chain, upstream → downstream:

| # | Stage | Control | Who sets it | Range |
|---|-------|---------|-------------|-------|
| 1 | Radio AF/volume out | analog knob | **human** | continuous |
| 2 | OS capture mute | mixer switch | software | on/off |
| 3 | OS capture level | mixer/ADC gain | software | device-specific dB |
| 4 | Graywolf software gain | DSP multiply | software | −60..+12 dB |
| (W) | Windows "enhancements" | endpoint APOs | software (off) | corrupts audio |

The hard constraints that dictate everything below come from the physics of the
chain, confirmed against `graywolf-modem/src/modem/mod.rs` (`pump_all_audio`):

- **Clipping at the ADC is unrecoverable.** Once samples rail at full scale, no
  downstream knob restores the waveform. A hot signal *must* come down upstream
  (OS level, then radio).
- **Digital boost doesn't improve SNR.** Graywolf's software gain multiplies
  signal *and* noise together, and on positive gain runs the result through a
  `tanh` soft-limiter (`mod.rs` ~L555) — so boosting a too-quiet signal
  distorts the tones before it ever reaches a usable level. A rail-low signal
  *must* come up upstream.
- Therefore **software gain only has authority inside a window** that the
  upstream (OS + radio) stages establish. "Just crank Graywolf's gain" cannot
  be the whole answer; the tuner has to be able to reach the upstream knobs.

---

## 3. Measurement strategy — what we optimize against

Graywolf already exposes two distinct audio measurements. They are *not*
interchangeable, and choosing the right one for each job is the core insight of
this design (it's also what GRA-130 set up).

### 3.1 Per-packet level — the primary signal objective

Source: the demodulator's tone-matched correlator
(`graywolf-modem/src/demod_afsk.rs`: mix → low-pass → magnitude on the mark and
space oscillators, peak-tracked, latched per frame via `set_audio_level`).
Surfaced as `mark_dbfs` / `space_dbfs` / `level_dbfs` on each packet
(`pkg/packetlog`, `GET /api/packets`).

This measures **the AFSK tones we actually decode**, only while a packet is
present — frequency-selective and packet-gated. It is the right number for "is
the signal at a good level." Use the **median `level_dbfs` over a rolling window
of recent decodes** (robust to a single hot/quiet packet). Use the
`mark_dbfs` − `space_dbfs` spread (**twist**) as a *separate diagnostic*: a
persistent imbalance points at radio de-emphasis / audio response, not level.

### 3.2 Device meter — overload guard + acquisition only

Source: broadband peak/RMS over each audio chunk, ~5 Hz
(`pump_all_audio`), exposed as `peak_dbfs` / `rms_dbfs` / `clipping` on
`GET /api/audio-devices/levels`. This is a sound-card VU meter — dominated by
whatever is loudest on the input (hiss, static crashes, adjacent signals), not
your packet. It is the wrong number for the signal objective, but the right
number for two jobs the per-packet measure can't do:

1. **Overload guard.** A clipped tone still carries strong tone energy, so the
   correlator can read a *healthy* `level_dbfs` on a clipped signal. The
   broadband `peak_dbfs` is what catches overload. **Per-packet level sets the
   target; device peak vetoes clipping.**
2. **Acquisition.** Per-packet levels exist only when packets decode. If the
   channel is so misadjusted that nothing decodes, the broadband peak/RMS is the
   only signal available to steer back into range.

### 3.3 Decode counts — ground truth

`GET /api/status` per-channel `rx_frames` (good) and `rx_bad_fcs` (failed FCS).
The good/bad ratio and good-frame rate over a window are the final arbiter:
levels are a proxy; decode count is the thing we actually want to maximize.

### 3.4 Three facts to design around (and small fixes worth making)

Found while tracing `pump_all_audio`:

- **Both meters are post software-gain** (the chunk is gain-adjusted before
  either the meter or the demod sees it). To reason about true ADC headroom
  independent of Graywolf gain, subtract `gain_db` or sample at unity gain.
  Otherwise software gain can mask an upstream level problem.
- **Positive software gain soft-limits via `tanh`** — digital boost distorts
  before it clips. Treat positive gain as a last-resort trim, not a fix.
- **The device `clipping` flag is only evaluated when `gain_db != 0`** (it's
  inside the `if gain_db.abs() > EPSILON` block), so at unity gain it is always
  `false`. The tuner should derive clipping from `peak_dbfs ≳ −0.5` rather than
  trusting the flag. *Optional modem fix: compute `clipping` unconditionally.*

### 3.5 Reference station — measured target (NW5W, 2026-06-25)

The target zones below are **anchored to a real, well-tuned station**
(`10.50.0.120`, NW5W-5), read live from its API. This is the calibration that
matters most: it pins the per-packet correlator scale to reality rather than a
guess.

Active RX device (CM108AH USB, `plughw:CARD=Device`), channel "VHF APRS"
(AFSK 1200), ~4 h uptime, 1443 good frames:

| Quantity | Measured | Notes |
|----------|----------|-------|
| Per-packet `level_dbfs` (median) | **−29.7** (−29.6…−29.9, very tight) | the signal objective |
| `mark_dbfs` / `space_dbfs` | −29.6 / −29.8 | |
| **Twist** \|mark − space\| (median) | **0.2 dB** (max 0.5) | near-perfect tone balance |
| Graywolf software gain | **−25.5 dB** | hot radio feed, pulled down digitally |
| Device meter `peak_dbfs` | −25.5, `clipping=false` | broadband, ~4 dB *above* per-packet |
| `rx_bad_fcs / (good+bad)` | 495 / 1938 ≈ 25% | normal for a busy collision-prone VHF channel |

Two things this confirms with hard data:

- **The per-packet correlator scale is not the device-meter scale.** A
  well-decoding signal reads ≈ **−30 dBFS** per-packet but ≈ −25 dBFS on the
  broadband device meter — a built-in offset (the tone-matched correlator sits
  below broadband peak). My earlier −12..−6 guess was wrong; it conflated the
  two scales. This is exactly the device-tab-vs-packet-log discrepancy the
  operator flagged, now quantified.
- **Negative software gain is the clean direction.** His chain runs the radio
  hot (raw near full-scale) and attenuates −25.5 dB in software (linear, no
  `tanh`, not clipping) — and decodes great. So "hot in, pulled down clean" is a
  legitimate well-tuned state, not a problem to correct.

### 3.6 Target zones (defaults, operator-overridable; anchored to §3.5)

- Per-packet `level_dbfs`: aim **≈ −30 dBFS** (reference median), accept roughly
  **−34 .. −26**. Treat the reference as the anchor and **confirm by decode
  count**, since the absolute correlator level varies with modem profile
  (Profile A/B, hard-limiter, slicer count) — do not hard-code −30 across all
  configs.
- Twist |mark − space|: **≤ ~2 dB** is excellent (reference 0.2); warn beyond
  ~6 dB (points at de-emphasis / audio response, not level).
- Device `peak_dbfs`: **< −1 dBFS** always (clipping veto / acquisition only —
  *not* a signal-level target).
- Decode health: good-frame rate not decreasing and `rx_bad_fcs / rx_frames`
  not increasing across a change (collisions inflate bad-FCS independent of
  level, so weight good-frame *rate* most).

---

## 4. Architecture

Three components, each with one clear job:

```
┌─────────────────────────────────────────────────────────────┐
│  MCP server  (the orchestrator / brain)                      │
│   • runs the tuning state machine (§5)                       │
│   • reads Graywolf REST: /status, /packets, /audio-devices/* │
│   • drives knobs: GW gain (REST), OS level+mute (OS layer)   │
│   • emits guidance to the user/agent (radio-knob prompts)    │
└───────────────┬───────────────────────────┬─────────────────┘
                │ REST + Bearer              │ in-proc / FFI / subprocess
                ▼                            ▼
┌───────────────────────────┐   ┌─────────────────────────────┐
│  Graywolf (Go service)    │   │  OS audio control layer      │
│   • /api/status           │   │   • get/set capture level dB │
│   • /api/packets          │   │   • get/set mute (switch)    │
│   • /api/audio-devices/*  │   │   • detect Win enhancements  │
│   • /{id}/gain  (DSP gain)│   │   ALSA | PipeWire/Pulse |    │
└──────────┬────────────────┘   │   CoreAudio | WASAPI         │
           │ IPC                 └─────────────────────────────┘
           ▼
┌───────────────────────────────────────────────────────────────┐
│  graywolf-modem  (single binary — capture + decode + meters)   │
│   existing: cpal capture, ensemble demod, per-packet + device  │
│             levels, --list-audio, offline FLAC/file decode     │
│   NEW: --record (WAV out) · --monitor (live level/guidance     │
│        JSON stream) · --decode <clip> (offline score)          │
└───────────────────────────────────────────────────────────────┘
```

### 4.1 Single binary for record/decode/monitor — confirmed feasible

`graywolf-modem` already *is* a cross-platform capture engine (cpal:
CoreAudio/ALSA/WASAPI, `audio/soundcard.rs`), already decodes captured audio
through the **production** pipeline (`bench.sh`: file/FLAC source →
`DevicePipeline` → ensemble demod → IPC `ReceivedFrame`), and already produces
both level measurements. So record + decode + count + monitor stay in one
binary. We do **not** ship sox/arecord:

- The modem's cpal path guarantees recorded samples travel the *exact* path the
  decoder uses (same device, rate, format). sox would introduce a second
  device-enumeration world that won't match our cpal `device_path`/`pcm_id`
  naming, plus possible resampling/format drift that silently changes decode
  results.
- No extra binary to package/sign/notarize across platforms.

New surface on the binary:

| Mode | Behavior |
|------|----------|
| `--record <dev> --seconds N --out clip.wav` | Capture via cpal, write **WAV PCM s16le** (`hound`). |
| `--decode <clip.wav\|.flac>` | Run clip through the bench/offline pipeline; emit JSON `{rx_frames, rx_bad_fcs, per-packet level_dbfs/mark/space, twist}`. |
| `--monitor <dev>` | Live: stream JSON-lines level stats + directional guidance (§6) while optionally writing the WAV. One session = human-alignment meter **and** decode artifact. |

WAV chosen over FLAC: universal, and avoids a FLAC *encoder* dependency (the
vendored `claxon` is decode-only; the modem only reads FLAC today). For purely
transient tuning captures we can skip the file entirely and feed an in-memory
i16 buffer straight into the pipeline (the `stdin_raw` source proves raw-PCM-in
works). FLAC archival stays a future nicety.

### 4.2 OS audio control layer

This is the one capability neither Graywolf nor cpal provides today (cpal does
capture, not mixer control). Per-platform back ends behind one trait:

```
trait OsAudio {
    fn capture_db_range(dev) -> (min_db, max_db, supported: bool)
    fn get_capture_db(dev)   -> dB
    fn set_capture_db(dev, dB)
    fn get_mute(dev)         -> bool
    fn set_mute(dev, bool)
    fn enhancements_active(dev) -> bool   // Windows only
}
```

**Linux, macOS, and Windows are all Day-1 first-class.** Graywolf has users on
all three and audio-level pain is universal, so the OS layer ships every desktop
back end in the *same* release — none is deferred behind a "Linux first" phase.
Each back end implements the full `OsAudio` trait (level range + get/set, mute
get/set, plus Windows enhancements). Android is **out of scope** — operators
don't drive MCP clients from phones — which also lets us ignore AAudio here and
keep the trait to the three desktop mixer APIs.

| Platform | Back end | Day-1 coverage |
|----------|----------|----------------|
| **Linux** | vendored **`alsa-rs`** `mixer` for raw ALSA; **`wpctl`/`pactl`** (PipeWire/PulseAudio) for the desktop-routed case | `has_capture_volume` / `set_capture_dB` / `get_capture_dB_range` / **`has_capture_switch`** (mute). Both paths required: headless rigs hit ALSA directly, desktop installs route through PipeWire/Pulse. ALSA needs no new dep. |
| **macOS** | CoreAudio (`coreaudio-sys`): input `kAudioDevicePropertyVolumeScalar` + `kAudioDevicePropertyMute`, addressed per `AudioObjectID` | Full level + mute. |
| **Windows** | WASAPI (`windows` crate): `IAudioEndpointVolume` (`GetMasterVolumeLevelScalar` / `SetMasterVolumeLevelScalar` / `Get/SetMute`) on the capture endpoint; enhancements already detected (`enhancements_enabled`) | Full level + mute + the enhancements precheck. |

Each back end normalizes its native scale (ALSA millibels, CoreAudio 0..1
scalar, WASAPI 0..1 scalar or dB) into the trait's **dB** contract so the state
machine reasons in one unit. cpal already gives us per-host device identity on
all three, which seeds the device→control mapping below.

**Mapping is the sharp edge** (and it is per-OS, so all three need it Day 1).
Graywolf's `device_path` (cpal name + `host_api`) must resolve to the right OS
*mixer control*: an ALSA simple-element / `plughw:`-card on Linux, an
`AudioObjectID` on macOS, an endpoint `IMMDevice` id on Windows. Some USB
adapters expose a single "Mic"/"Capture" control; some expose **none** (fixed
gain) — in that case `set_capture_db` is a no-op and the tuner falls back to
software gain + radio-knob guidance. Every back end must report
`capture_db_range.supported = false` honestly so the state machine can skip
straight to the human step. This "no usable hardware control" path is common
enough (and varies by OS) that it's a first-class outcome, not an error.

Where the layer lives: cleanest is **inside `graywolf-modem`** (it already owns
cpal device identity and has the vendored `alsa-rs`), exposed to the MCP server
either as additional binary subcommands (`--get-capture / --set-capture /
--get-mute / --set-mute`) or via the existing IPC. That keeps "one binary" and
avoids the MCP server re-deriving device identity. The MCP server stays
language-light and just calls it.

### 4.3 Auth / deployment

Graywolf API auth is a **Bearer token** when `BearerToken` is set (desktop
default empty / localhost). MCP config = `{ base_url, token?, device_id }`. OS
mixer access needs appropriate permissions (Linux `audio` group; macOS/Windows
local user). All local-first; nothing leaves the host.

### 4.4 Cross-platform / vendoring constraints (learned from the existing tree)

The modem already carries hard-won portability work, especially for old/32-bit
Raspberry Pi. The OS-control layer must inherit it, not fight it:

- **Linux ALSA goes through the patched `alsa` crate, never stock.** The
  workspace `Cargo.toml` pins `[patch.crates-io] alsa = { git =
  ".../chrissnell/alsa-rs", rev = 56099e8 }` to fix a **t64 `struct timespec`
  stack overflow** that SIGSEGV-crashes capture on current 32-bit Pi OS
  (libasound2t64 — Pi Zero/1/2 and 32-bit Pi 3/4; issue #231,
  `docs/plans/2026-06-11-armhf-t64-alsa-htstamp-fix.md`). The mixer FFI
  (`snd_mixer_selem_*`) we need touches `i64`/MilliBel ranges, **not** timespec,
  so it's outside the crashing path — but the layer must use the *patched* crate
  and we should add an armhf-t64 smoke test for the mixer calls so we don't
  reintroduce the class of bug. Old-Pi support thus comes essentially for free.
- **Avoid new dynamically-linked system libs** — they break the cross/musl
  release toolchain (`Cross.toml`) and old glibc targets. Precedent: `hidapi` is
  pulled with `linux-native-basic-udev` (pure-Rust hidraw, **no libudev link**).
  Apply the same rule here: do the **PipeWire/Pulse** path via `wpctl`/`pactl`
  **subprocess**, not a linked `libpulse`/`libpipewire`, so nothing new has to
  cross-compile for armv6/armhf/musl.
- **Windows reuses the in-tree `windows` 0.59 crate** (already used for PTT and
  the enhancements registry read) — add `Win32_Media_Audio` /
  `Win32_System_Com` features for `IAudioEndpointVolume`; don't introduce a
  parallel `windows-sys`.
- **macOS reuses `coreaudio-sys`** (already in the tree transitively via cpal)
  for the input volume/mute properties — no new top-level dep.
- **Device identity comes from cpal**, which the modem already owns on every
  host, so the OS-control layer maps `device_path`/`host_api` → mixer control in
  the one process that already enumerated the device (avoids a second, drifting
  enumeration world — the same reason §4.1 rejects shipping sox).

Net: the only genuinely new linked code is the per-OS mixer calls themselves
(patched-ALSA mixer, CoreAudio, WASAPI); everything else is subprocess or an
existing dependency, which keeps the old-RPi and cross-compile story intact.

---

## 5. The tuning state machine

Automatic inner loop (OS level + software gain), human-guided outer loop (radio),
escalating outward only when the inner knobs hit a rail. Decode validation
gates every committed change.

```
0. PRECHECK
   • resolve device → OS mixer control; read capture_db range/support
   • if muted → unmute (or, if no control, prompt user)
   • if Windows enhancements active → prompt user to disable (corrupts AFSK)
   • snapshot current GW gain, OS level, baseline rx_frames/rx_bad_fcs

1. ACQUIRE  (broadband, traffic-tolerant)
   • if no recent decodes: use device peak/rms to get peak into ~ −12..−3 dBFS,
     clipping=false — just enough to start decoding

2. SET OS CAPTURE LEVEL  (primary automatic knob)
   • adjust capture_db so device peak stays < −1 (no clip) AND
     median per-packet level_dbfs trends toward the reference band (≈ −30, §3.6)
   • monotone search w/ dwell time per step (§6 stats); never exceed clip veto

3. TRIM GW SOFTWARE GAIN  (fine, bounded)
   • center median per-packet level_dbfs in the §3.6 band (≈ −30); negative gain
     is clean, so prefer pulling a hot feed down; avoid >0 dB (tanh distortion)
     unless the feed is genuinely too quiet

4. VALIDATE
   • hold and watch a window of decodes: good-frame rate not down,
     rx_bad_fcs/rx_frames not up vs the pre-change baseline
   • for digital-only sweeps, score deterministically on a captured clip (§7)

5. ESCALATE TO HUMAN  (only if step 2 railed)
   • railed HIGH and still clipping     → "turn radio volume DOWN a little"
   • railed LOW  and still too quiet/SNR → "turn radio volume UP a little"
   • enter live assist (§6); on each nudge, return to step 1
```

Commit policy: change one knob at a time, dwell, measure, keep only if decode
health holds or improves; otherwise revert. Persist the winning OS level + GW
gain; report the final settings and before/after metrics.

---

## 6. Handling the human knob — live tuning assistant

The radio volume is human-controlled, slow, and — critically — each adjustment
re-captures *different* live traffic, so discrete before/after decode counts
aren't comparable across radio changes. The solution is **not** a batch sweep;
it's a continuous loop closed *through the human*:

- The `--monitor` stream reports **robust statistics over a rolling window**
  (e.g. 95th-percentile device peak, **median per-packet `level_dbfs`**, and
  clip-rate %) over ~20–60 s — never instantaneous values, so a quiet gap or one
  loud neighbor doesn't jerk the guidance.
- It emits a **direction + target range**, never a precise number:
  *"Hot — clipping 8% of packets. Turn radio volume DOWN a little."* Humans
  can't hit a number and traffic varies; precision is the automatic knobs' job
  afterward.
- Because the user watches a **smoothed live meter converge** rather than
  comparing captures, the "different traffic each time" problem dissolves — it's
  tuning by ear, like peaking an analog signal.

"Can we do it all during capture?" — yes, and that's the clean design: a single
`--monitor` session is simultaneously (a) the WAV writer / decoder feed, (b) the
JSON-lines level+guidance source the MCP server relays to the user, and (c) the
stream the MCP server reads to drive the automatic knobs. One capture, three
uses.

---

## 7. Offline decode sweep for the digital knobs

Decode-on-recording is deterministic **only on a fixed clip**, which makes it
perfect for the *digital* side: capture once, then apply each candidate software
gain (and/or modem profile) to the *same* samples and `--decode` each —
apples-to-apples, traffic-independent. So:

- **Decode-count scoring** is the objective for the digital knobs (software
  gain, profile) — run on a captured clip.
- **Live dBFS / clip-rate targets** are the objective for the analog knobs (OS
  level coarse, radio) — since each change captures different traffic.
- A **looped reference signal** (a TX playing a known clip, or injecting the
  WA8LMF tracks at the radio) makes even the analog step decode-scoreable for
  gold-standard validation/regression.

---

## 8. MCP tool surface

| Tool | Purpose |
|------|---------|
| `list_audio_devices` | Graywolf devices + OS-control capability (range, mute, supported?). |
| `get_levels` | Device peak/rms/clipping snapshot (guard/acquisition). |
| `get_packet_levels` | Aggregated recent per-packet `level_dbfs` (median/IQR) + twist. |
| `get_decode_stats` | `rx_frames` / `rx_bad_fcs` deltas over a window. |
| `set_software_gain` | Proxy `PUT /api/audio-devices/{id}/gain`. |
| `get_os_capture` / `set_os_capture` | OS capture level (dB) via the OS layer. |
| `get_mute` / `set_mute` | OS capture switch. |
| `record_clip` / `decode_clip` | Capture a clip / score it offline. |
| `start_monitor` / `read_monitor` | Live level+guidance stream for the human step. |
| `autotune` | Run the §5 state machine end-to-end; returns chosen settings + before/after metrics + any pending human prompt. |

`autotune` is the headline; the rest are the primitives it (and a curious
operator) compose.

---

## 9. Data contracts

`--monitor` / `--decode` JSON line (stable, versioned):

```json
{
  "v": 1,
  "device_id": 1,
  "window_s": 30,
  "device": { "peak_dbfs": -2.1, "rms_dbfs": -18.4, "clip_rate": 0.06 },
  "packet": { "n": 24, "level_dbfs_med": -7.8, "level_dbfs_iqr": 1.9,
              "twist_db_med": 3.1 },
  "decode": { "rx_frames": 24, "rx_bad_fcs": 2 },
  "guidance": { "stage": "human_radio", "direction": "down",
                "reason": "clipping", "message": "Turn radio volume down a little." }
}
```

`autotune` result: `{ committed: {os_capture_db, gain_db}, before/after:
{level_dbfs_med, peak_dbfs, rx_frames_rate, bad_fcs_ratio}, escalations: [...],
notes }`.

---

## 10. Code touch points

- `graywolf-modem`: new `--record` / `--monitor` / `--decode` subcommands
  (reuse `soundcard.rs` capture, bench offline pipeline, existing level paths);
  add `hound` (WAV) and the rolling-stat aggregator; OS-control subcommands/IPC
  over the `alsa-rs` mixer + per-OS back ends. Optional: make the device
  `clipping` flag unconditional (§3.4).
- Graywolf Go service: **no API change required** — `/status`, `/packets`,
  `/audio-devices/levels`, `/{id}/gain` already cover reads + the software-gain
  write. (Possible nicety later: a per-packet WS feed; today only the AX.25
  terminal WS exists, so live per-packet data comes from the modem's own
  `--monitor` stream.)
- New: the MCP server (Rust; §11) + a shared OS-audio-control crate used by both
  the server and `graywolf-modem`.

---

## 11. Decisions (settled) and remaining questions

**Settled with the operator (2026-06-25):**

1. **MCP server language — Rust.** Shares the modem's stack and the patched
   `alsa` crate; mature cross-platform OS-audio crates (`coreaudio-sys`, the
   `windows` crate already in-tree). The OS-control layer is a small crate the
   modem and the MCP server both use — one implementation, all three desktop
   OSes, direct (non-subprocess) bindings everywhere except the Linux
   PipeWire/Pulse path. May use the `creating-mcps` skill.
2. **OS control lives in `graywolf-modem`.** Keeps device identity (cpal, all
   three hosts) and the vendored audio FFI in one place; the MCP server calls it
   via subcommands/IPC (§4.2).
3. **Default target zone — settled by the NW5W reference (§3.5/§3.6):** aim
   per-packet `level_dbfs` ≈ −30, twist ≤ ~2 dB, confirm by decode count.

**Still open:**

4. **Linux PipeWire/Pulse path** — `wpctl`/`pactl` subprocess for v1 vs a
   `libpulse`/PipeWire binding. Subprocess preferred (no new linked dep; see
   §4.4); raw ALSA stays the no-dep direct path for headless rigs.
5. **Reference-signal validation** — is a TX-loopback / WA8LMF-injection harness
   in scope for v1, or just live-traffic + level targets? (Deferred to M5.)

---

## 12. Phasing

**Platform mandate:** the v1 release ships **Linux + macOS + Windows together**.
Phases are sliced by *capability*, and each capability lands on all three OSes
before it's "done" — we don't ship a Linux-only milestone and backfill the
others. Android is excluded (§13). The cross-platform work parallelizes cleanly
because every back end implements the same `OsAudio` trait behind the same
device-mapping seam (cpal identity), so the three can be built and tested
side-by-side rather than serially.

1. **M1 — Recorder + offline score.** `--record` (WAV) and `--decode` (JSON
   score) in `graywolf-modem`. cpal capture + the offline pipeline are already
   cross-platform, so this is tri-OS from the start. Unblocks the deterministic
   digital sweep.
2. **M2 — OS audio layer, all three back ends.** Capture level + mute behind the
   `OsAudio` trait: ALSA (`alsa-rs`) **and** PipeWire/Pulse on Linux, CoreAudio
   on macOS, WASAPI on Windows; device→control mapping + honest `supported`
   reporting on each. `--get/--set-capture` / `--get/--set-mute` subcommands. A
   milestone exit criterion is "all three platforms pass the same control
   smoke test."
3. **M3 — `--monitor` live stats + guidance** stream (§6/§9) — platform-neutral
   (pure DSP/stats), works everywhere M1 does.
4. **M4 — MCP server + `autotune` state machine** (§5/§8), end-to-end on all
   three desktop OSes.
5. **M5 — Hardening + optional reference-signal validation** (TX-loopback /
   WA8LMF injection); per-OS device-quirk coverage (USB adapters with no/odd
   capture controls).

---

## 13. Out of scope (for now)

- **Android.** Operators don't drive MCP clients from phones, so the AAudio
   mixer path is excluded; the OS layer targets only the three desktop APIs.
- Auto-adjusting **TX** audio / deviation (this is RX decode tuning only).
- Touching modem DSP profiles automatically beyond an optional offline sweep.
- A GUI; the MCP/agent conversation is the interface for v1.
