# M2 — OS Audio Control Layer Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add an OS-level capture-control layer to `graywolf-modem` — read/set the sound card's hardware capture level (dB) and mute, on Linux, macOS, and Windows — exposed as CLI subcommands the (future) MCP autotuner drives.

**Architecture:** One `OsAudio` trait with a `CaptureControl` snapshot type, plus a per-OS back end behind it: ALSA (vendored `alsa` crate mixer) and PipeWire/PulseAudio (`wpctl`/`pactl` subprocess) on Linux, CoreAudio on macOS, WASAPI on Windows. Device identity comes from the cpal `device_path`/`host_api` the modem already has. "No usable hardware control" is a first-class result (`supported=false`), not an error. Lives as a module in `graywolf-modem` (`src/osaudio/`); can be promoted to a workspace crate when the M4 MCP server needs to link it directly.

**Tech Stack:** Rust 2021; vendored `alsa` crate (patched fork, mixer API) on Linux; `wpctl`/`pactl` subprocess; `coreaudio-sys` (macOS, transitive via cpal); the in-tree `windows` 0.59 crate (Windows, add audio features); `serde_json` for output.

---

## Scope & platform note

M2 is **one subsystem** (the capture-control layer) but spans four back ends. Per the design spec's Day-1 mandate (`docs/superpowers/specs/2026-06-25-audio-autotune-mcp-design.md` §4.2/§4.4/§12), **all three desktop OSes ship together** — this plan covers all four back ends in one milestone, with each as its own task group behind the shared trait.

**Verification boundary (be honest about it):** Tasks 1–5 (trait, Linux ALSA, Linux PipeWire/Pulse, dispatch, CLI) are fully bite-sized TDD and verifiable on a Linux dev box / CI. Tasks 6–7 (macOS CoreAudio, Windows WASAPI) give the exact API entry points and the contract to implement against, but their unsafe FFI and live get/set are **verified on those platforms** (or via the cross-compile CI + a manual smoke test), because they cannot be run in a Linux sandbox. They are concrete, not placeholders — but their "run it" step happens on-platform.

## Build prerequisites (read first)

The `graywolf-modem` crate needs **`protoc`** (prost-build) and, on Linux, **`libasound2-dev`** (the `alsa`/`alsa-sys` link). On Debian/Ubuntu: `sudo apt-get install -y protobuf-compiler libasound2-dev`. CI's `rust-ci` job already installs these. The workspace root `Cargo.toml` pins `[patch.crates-io] alsa = { git = ".../chrissnell/alsa-rs", rev = 56099e8 }` (the t64/old-Pi fix, §4.4) — the ALSA back end MUST go through that patched crate; do not add a second `alsa` dependency.

Standard verify commands (run from repo root): `cargo check --workspace`, `cargo clippy --workspace -- -D warnings`, `cargo test -p graywolf-demod`.

---

## File structure

- **Create** `graywolf-modem/src/osaudio/mod.rs` — the `OsAudio` trait, `CaptureControl` + `Backend` types, `OsAudioError`, the `open(device_path, host_api)` dispatch, and the `NoControl` fallback. One responsibility: the public contract + platform dispatch.
- **Create** `graywolf-modem/src/osaudio/alsa.rs` — Linux ALSA back end (`#[cfg(target_os = "linux")]`).
- **Create** `graywolf-modem/src/osaudio/pulse.rs` — Linux PipeWire/PulseAudio subprocess back end (`#[cfg(target_os = "linux")]`).
- **Create** `graywolf-modem/src/osaudio/coreaudio.rs` — macOS back end (`#[cfg(target_os = "macos")]`).
- **Create** `graywolf-modem/src/osaudio/wasapi.rs` — Windows back end (`#[cfg(target_os = "windows")]`).
- **Create** `graywolf-modem/src/osaudio/cli.rs` — parses the new subcommand args and prints JSON (shared by all OSes).
- **Modify** `graywolf-modem/src/lib.rs` — `pub mod osaudio;`.
- **Modify** `graywolf-modem/src/bin/graywolf_modem.rs` — dispatch the new flags.
- **Modify** `graywolf-modem/Cargo.toml` — add `coreaudio-sys` (macOS target dep) and the WASAPI features on the `windows` dep.
- **Modify** `docs/wiki/code-map.md`, `docs/wiki/glossary.md`, `docs/handbook/audio.html` — document the capability.

---

### Task 1: `osaudio` module — trait, types, dispatch, NoControl

**Files:**
- Create: `graywolf-modem/src/osaudio/mod.rs`
- Modify: `graywolf-modem/src/lib.rs`

- [ ] **Step 1: Write the failing test**

Create `graywolf-modem/src/osaudio/mod.rs` with the test module first:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn nocontrol_reports_unsupported() {
        let mut c = NoControl { reason: "no mixer".into() };
        let cap = c.capability();
        assert_eq!(cap.backend, Backend::None);
        assert!(!cap.has_volume);
        assert!(!cap.has_mute);
        assert!(cap.current_db.is_none());
        assert!(c.set_capture_db(-9.0).is_err());
        assert!(c.set_mute(true).is_err());
    }

    #[test]
    fn capability_serializes_to_expected_json() {
        let cap = CaptureControl {
            backend: Backend::Alsa,
            has_volume: true,
            has_mute: true,
            min_db: Some(-30.0),
            max_db: Some(0.0),
            current_db: Some(-12.0),
            muted: Some(false),
        };
        let v: serde_json::Value =
            serde_json::from_str(&serde_json::to_string(&cap).unwrap()).unwrap();
        assert_eq!(v["backend"], "alsa");
        assert_eq!(v["has_volume"], true);
        assert_eq!(v["current_db"], -12.0);
        assert_eq!(v["muted"], false);
    }
}
```

- [ ] **Step 2: Run it, verify it fails**

Run: `cd graywolf-modem && cargo test --lib osaudio 2>&1 | tail -15`
Expected: FAIL to compile — `NoControl`/`CaptureControl`/`Backend` missing.

- [ ] **Step 3: Implement the module**

Prepend above the tests:

```rust
//! OS-level capture control: read/set the sound card's hardware capture level
//! (dB) and mute. One trait, a per-OS back end behind it. "No usable hardware
//! control" is a first-class result (`has_volume=false`), not an error.

use serde::Serialize;

#[derive(Serialize, Debug, Clone, Copy, PartialEq, Eq)]
#[serde(rename_all = "lowercase")]
pub enum Backend {
    Alsa,
    Pulse,
    CoreAudio,
    Wasapi,
    None,
}

/// A snapshot of the device's capture control.
#[derive(Serialize, Debug, Clone)]
pub struct CaptureControl {
    pub backend: Backend,
    pub has_volume: bool,
    pub has_mute: bool,
    /// dB range and current value; None when the device has no volume control.
    pub min_db: Option<f32>,
    pub max_db: Option<f32>,
    pub current_db: Option<f32>,
    /// None when the device has no mute switch.
    pub muted: Option<bool>,
}

#[derive(Debug)]
pub struct OsAudioError(pub String);

impl std::fmt::Display for OsAudioError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{}", self.0)
    }
}
impl std::error::Error for OsAudioError {}

impl From<String> for OsAudioError {
    fn from(s: String) -> Self {
        OsAudioError(s)
    }
}

/// The capture-control contract every back end implements.
pub trait OsAudio {
    /// Current snapshot (range, level, mute). Re-reads the device each call.
    fn capability(&mut self) -> CaptureControl;
    /// Set the hardware capture level in dB. Clamp to the device range.
    fn set_capture_db(&mut self, db: f32) -> Result<(), OsAudioError>;
    /// Set the capture mute switch.
    fn set_mute(&mut self, mute: bool) -> Result<(), OsAudioError>;
}

/// Fallback for devices/platforms with no usable hardware capture control.
pub struct NoControl {
    pub reason: String,
}

impl OsAudio for NoControl {
    fn capability(&mut self) -> CaptureControl {
        CaptureControl {
            backend: Backend::None,
            has_volume: false,
            has_mute: false,
            min_db: None,
            max_db: None,
            current_db: None,
            muted: None,
        }
    }
    fn set_capture_db(&mut self, _db: f32) -> Result<(), OsAudioError> {
        Err(OsAudioError(format!("no hardware capture control: {}", self.reason)))
    }
    fn set_mute(&mut self, _mute: bool) -> Result<(), OsAudioError> {
        Err(OsAudioError(format!("no hardware capture control: {}", self.reason)))
    }
}

/// Open the capture control for a device, given the cpal `device_path` and
/// `host_api` (e.g. "ALSA", "CoreAudio", "WASAPI"). Always returns a working
/// object — `NoControl` when nothing maps. Platform-specific `open_*` are added
/// by the per-OS tasks below.
pub fn open(device_path: &str, host_api: &str) -> Box<dyn OsAudio> {
    #[cfg(target_os = "linux")]
    {
        return linux_open(device_path, host_api);
    }
    #[cfg(target_os = "macos")]
    {
        return coreaudio::open(device_path).unwrap_or_else(|e| {
            Box::new(NoControl { reason: e.0 })
        });
    }
    #[cfg(target_os = "windows")]
    {
        return wasapi::open(device_path).unwrap_or_else(|e| {
            Box::new(NoControl { reason: e.0 })
        });
    }
    #[allow(unreachable_code)]
    {
        let _ = (device_path, host_api);
        Box::new(NoControl { reason: "unsupported platform".into() })
    }
}
```

Add to `graywolf-modem/src/lib.rs` next to the other `pub mod` lines:

```rust
pub mod osaudio;
```

Note: `linux_open`, `coreaudio::open`, `wasapi::open` are added by Tasks 4/6/7. To compile Task 1 alone, temporarily stub `linux_open` at the bottom of `mod.rs` under `#[cfg(target_os = "linux")]` returning `Box::new(NoControl { reason: "stub".into() })`; Task 4 replaces it. (Mention this stub in the commit; it is removed in Task 4.)

- [ ] **Step 4: Run tests, verify pass**

Run: `cd graywolf-modem && cargo test --lib osaudio 2>&1 | tail -15`
Expected: PASS (both tests).

- [ ] **Step 5: Lint + commit**

Run: `cd graywolf-modem && cargo clippy --lib -- -D warnings 2>&1 | tail`
```bash
git add graywolf-modem/src/osaudio/mod.rs graywolf-modem/src/lib.rs
git commit -m "feat(modem): osaudio trait + CaptureControl + NoControl fallback"
```

---

### Task 2: ALSA back end (Linux)

**Files:**
- Create: `graywolf-modem/src/osaudio/alsa.rs`
- Modify: `graywolf-modem/src/osaudio/mod.rs` (`#[cfg(target_os="linux")] mod alsa;`)
- Reference (read): `alsa-rs/src/mixer.rs` (the vendored `alsa` crate's `Mixer`/`Selem`/`MilliBel`); `graywolf-modem/src/audio/soundcard.rs::alsa_card_token` (already `pub`).

**Verified API facts:** `alsa::mixer::Mixer::new(name: &str, nonblock: bool) -> Result<Mixer>`; `mixer.iter()` yields elems, `alsa::mixer::Selem::new(elem)` → `Option<Selem>`; `selem.has_capture_volume() -> bool`; `selem.has_capture_switch() -> bool`; `selem.get_capture_db_range() -> (MilliBel, MilliBel)`; `selem.get_capture_vol_db(ch) -> Result<MilliBel>`; `selem.set_capture_db_all(MilliBel, Round)`; `selem.get_capture_switch(ch) -> Result<i32>` (1 = on/unmuted, 0 = muted); `selem.set_capture_switch_all(i32)`. `MilliBel(pub i64)` with `MilliBel::from_db(f32)` / `.to_db() -> f32`. `alsa::Round::{Floor,Ceil}`. `alsa::mixer::SelemChannelId::mono()`. `alsa_card_token("hw:CARD=Device,DEV=0") -> Some("Device")`.

- [ ] **Step 1: Write failing tests for the pure helpers**

Create `graywolf-modem/src/osaudio/alsa.rs` with tests first (these don't need a real card):

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn mixer_card_name_from_device_path() {
        assert_eq!(mixer_card_name("hw:CARD=Device,DEV=0"), Some("hw:CARD=Device".to_string()));
        assert_eq!(mixer_card_name("plughw:CARD=1,DEV=0"), Some("hw:CARD=1".to_string()));
        assert_eq!(mixer_card_name("default"), None);
    }

    #[test]
    fn picks_capture_capable_selem() {
        // (name, has_capture_volume): prefer "Capture", then "Mic", then first with volume.
        let elems = vec![
            ("Master".to_string(), false),
            ("Mic".to_string(), true),
            ("Capture".to_string(), true),
        ];
        assert_eq!(best_capture_name(&elems), Some("Capture".to_string()));
        let elems2 = vec![("Mic".to_string(), true), ("PCM".to_string(), false)];
        assert_eq!(best_capture_name(&elems2), Some("Mic".to_string()));
        let elems3 = vec![("PCM".to_string(), false)];
        assert_eq!(best_capture_name(&elems3), None);
    }

    #[test]
    fn clamps_db_to_range() {
        assert_eq!(clamp_db(-9.0, -30.0, 0.0), -9.0);
        assert_eq!(clamp_db(5.0, -30.0, 0.0), 0.0);
        assert_eq!(clamp_db(-99.0, -30.0, 0.0), -30.0);
    }
}
```

- [ ] **Step 2: Run, verify fail**

Run: `cd graywolf-modem && cargo test --lib osaudio::alsa 2>&1 | tail -15`
Expected: FAIL to compile.

- [ ] **Step 3: Implement the back end**

Prepend to `alsa.rs`:

```rust
//! Linux ALSA capture control via the (patched) `alsa` crate mixer.

use alsa::mixer::{Mixer, MilliBel, Selem, SelemChannelId};
use alsa::Round;

use super::{Backend, CaptureControl, OsAudio, OsAudioError};
use crate::audio::soundcard::alsa_card_token;

/// Build the mixer control card name ("hw:CARD=<tok>") from a cpal pcm_id.
/// Returns None for `default`/non-ALSA ids (caller falls back to NoControl).
pub(super) fn mixer_card_name(device_path: &str) -> Option<String> {
    let tok = alsa_card_token(device_path)?;
    Some(format!("hw:CARD={tok}"))
}

/// Choose the best capture simple-element name from (name, has_capture_volume)
/// pairs: prefer "Capture", then "Mic", then the first element that has a
/// capture volume. None when no element has a capture volume.
pub(super) fn best_capture_name(elems: &[(String, bool)]) -> Option<String> {
    for pref in ["Capture", "Mic"] {
        if elems.iter().any(|(n, v)| *v && n == pref) {
            return Some(pref.to_string());
        }
    }
    elems.iter().find(|(_, v)| *v).map(|(n, _)| n.clone())
}

pub(super) fn clamp_db(db: f32, min: f32, max: f32) -> f32 {
    db.max(min).min(max)
}

pub struct AlsaControl {
    card: String,
    selem_name: String,
}

/// Open an ALSA capture control. Err only on genuine open failure; a card with
/// no capture-capable element returns Err so the caller maps to NoControl.
pub fn open(device_path: &str) -> Result<AlsaControl, OsAudioError> {
    let card = mixer_card_name(device_path)
        .ok_or_else(|| OsAudioError("no ALSA card in device path".into()))?;
    let mixer = Mixer::new(&card, false).map_err(|e| OsAudioError(e.to_string()))?;
    let elems: Vec<(String, bool)> = mixer
        .iter()
        .filter_map(Selem::new)
        .map(|s| {
            (
                s.get_id().get_name().unwrap_or("").to_string(),
                s.has_capture_volume(),
            )
        })
        .collect();
    let selem_name = best_capture_name(&elems)
        .ok_or_else(|| OsAudioError(format!("no capture volume control on {card}")))?;
    Ok(AlsaControl { card, selem_name })
}

impl AlsaControl {
    fn with_selem<T>(
        &self,
        f: impl FnOnce(&Selem) -> Result<T, OsAudioError>,
    ) -> Result<T, OsAudioError> {
        let mixer = Mixer::new(&self.card, false).map_err(|e| OsAudioError(e.to_string()))?;
        let selem = mixer
            .iter()
            .filter_map(Selem::new)
            .find(|s| s.get_id().get_name().map(|n| n == self.selem_name).unwrap_or(false))
            .ok_or_else(|| OsAudioError("capture element disappeared".into()))?;
        f(&selem)
    }
}

impl OsAudio for AlsaControl {
    fn capability(&mut self) -> CaptureControl {
        let snap = self.with_selem(|s| {
            let (min, max) = s.get_capture_db_range();
            let cur = s.get_capture_vol_db(SelemChannelId::mono()).ok();
            let muted = if s.has_capture_switch() {
                s.get_capture_switch(SelemChannelId::mono()).ok().map(|v| v == 0)
            } else {
                None
            };
            Ok(CaptureControl {
                backend: Backend::Alsa,
                has_volume: true,
                has_mute: s.has_capture_switch(),
                min_db: Some(min.to_db()),
                max_db: Some(max.to_db()),
                current_db: cur.map(|m| m.to_db()),
                muted,
            })
        });
        snap.unwrap_or(CaptureControl {
            backend: Backend::Alsa,
            has_volume: false,
            has_mute: false,
            min_db: None,
            max_db: None,
            current_db: None,
            muted: None,
        })
    }

    fn set_capture_db(&mut self, db: f32) -> Result<(), OsAudioError> {
        self.with_selem(|s| {
            let (min, max) = s.get_capture_db_range();
            let clamped = clamp_db(db, min.to_db(), max.to_db());
            s.set_capture_db_all(MilliBel::from_db(clamped), Round::Floor)
                .map_err(|e| OsAudioError(e.to_string()))
        })
    }

    fn set_mute(&mut self, mute: bool) -> Result<(), OsAudioError> {
        self.with_selem(|s| {
            if !s.has_capture_switch() {
                return Err(OsAudioError("device has no capture mute switch".into()));
            }
            // ALSA switch: 1 = enabled (unmuted), 0 = muted.
            s.set_capture_switch_all(if mute { 0 } else { 1 })
                .map_err(|e| OsAudioError(e.to_string()))
        })
    }
}
```

Add to `mod.rs` (near the top): `#[cfg(target_os = "linux")] pub(crate) mod alsa;`

- [ ] **Step 4: Run unit tests, verify pass**

Run: `cd graywolf-modem && cargo test --lib osaudio::alsa 2>&1 | tail -15`
Expected: PASS (`mixer_card_name`, `picks_capture_capable_selem`, `clamps_db_to_range`). `cargo clippy --lib -- -D warnings` clean.

- [ ] **Step 5: Manual live smoke test (documented; not CI)**

On a Linux box with a USB sound card that has a capture control (find one via `arecord -l`):
```bash
# from a tiny example or a throwaway test that calls osaudio::alsa::open(...)
# then capability()/set_capture_db(-9.0)/set_mute(false); confirm `alsamixer`
# (F4 capture view) reflects the change.
```
Document the result. Many USB CODEC chips (CM108-class) have **no** capture volume — `open` returns Err there and the dispatch falls back to `NoControl`; that is correct behavior, not a bug.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/osaudio/alsa.rs graywolf-modem/src/osaudio/mod.rs
git commit -m "feat(modem): ALSA capture-control back end (level dB + mute)"
```

---

### Task 3: PipeWire/PulseAudio back end (Linux, subprocess)

**Files:**
- Create: `graywolf-modem/src/osaudio/pulse.rs`
- Modify: `graywolf-modem/src/osaudio/mod.rs` (`#[cfg(target_os="linux")] mod pulse;`)

**Why subprocess:** §4.4 — no new linked system lib (keeps the cross/musl + old-Pi toolchain intact). Prefer `wpctl` (PipeWire) and fall back to `pactl` (PulseAudio). Both speak source (capture) volume/mute.

- [ ] **Step 1: Write failing tests for the output parsers**

Create `pulse.rs` with tests first, using captured sample output (no live daemon needed):

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_pactl_source_volume_and_mute() {
        // Trimmed `pactl list sources` block for one source.
        let sample = "\
Source #1
\tName: alsa_input.usb-0d8c_USB_Audio-00.mono-fallback
\tMute: no
\tVolume: front-left: 45000 /  69% / -9.50 dB
\tBase Volume: 65536 / 100% / 0.00 dB
";
        let p = parse_pactl_source(sample, "alsa_input.usb-0d8c_USB_Audio-00.mono-fallback").unwrap();
        assert!((p.db - (-9.50)).abs() < 0.01);
        assert_eq!(p.muted, false);
    }

    #[test]
    fn pactl_volume_percent_to_db_roundtrips_sign() {
        // 100% -> 0 dB; quarter scale is negative.
        assert!(percent_to_db(100) <= 0.01);
        assert!(percent_to_db(25) < 0.0);
    }
}
```

- [ ] **Step 2: Run, verify fail**

Run: `cd graywolf-modem && cargo test --lib osaudio::pulse 2>&1 | tail -15`
Expected: FAIL to compile.

- [ ] **Step 3: Implement**

Prepend to `pulse.rs`:

```rust
//! Linux PipeWire/PulseAudio capture control via `wpctl`/`pactl` subprocess.
//! No linked dependency — keeps the cross/musl + old-Pi toolchain intact (§4.4).

use std::process::Command;

use super::{Backend, CaptureControl, OsAudio, OsAudioError};

#[derive(Debug)]
pub(super) struct PaSource {
    pub db: f32,
    pub muted: bool,
}

/// PulseAudio reports per-channel as ".. / NN% / -X.XX dB"; take the dB.
pub(super) fn parse_pactl_source(list_output: &str, source_name: &str) -> Option<PaSource> {
    // Split into per-source blocks on "Source #".
    for block in list_output.split("Source #") {
        if !block.contains(source_name) {
            continue;
        }
        let muted = block
            .lines()
            .find_map(|l| l.trim().strip_prefix("Mute:"))
            .map(|v| v.trim().eq_ignore_ascii_case("yes"))
            .unwrap_or(false);
        let db = block
            .lines()
            .find(|l| l.trim_start().starts_with("Volume:"))
            .and_then(|l| l.rsplit('/').next())
            .and_then(|s| s.trim().strip_suffix("dB").or(Some(s.trim())))
            .and_then(|s| s.trim().parse::<f32>().ok())?;
        return Some(PaSource { db, muted });
    }
    None
}

/// Rough percent->dB for the set path when dB isn't directly accepted
/// (PulseAudio volume is cubic; 100% = 0 dB). Used only as a fallback.
pub(super) fn percent_to_db(percent: u32) -> f32 {
    if percent == 0 {
        return -60.0;
    }
    20.0 * (percent as f32 / 100.0).log10()
}

pub struct PulseControl {
    source: String,
    tool: Tool,
}

#[derive(Clone, Copy)]
enum Tool {
    Wpctl,
    Pactl,
}

fn have(cmd: &str) -> bool {
    Command::new(cmd).arg("--version").output().map(|o| o.status.success()).unwrap_or(false)
}

/// Resolve the cpal `device_path` to a Pulse/PipeWire source name. The cpal
/// PulseAudio host exposes source names directly; for the ALSA host running
/// under Pulse, the source is typically `alsa_input.<...>` — match by the card
/// token. Returns Err when no daemon/source maps.
pub fn open(device_path: &str) -> Result<PulseControl, OsAudioError> {
    let tool = if have("wpctl") {
        Tool::Wpctl
    } else if have("pactl") {
        Tool::Pactl
    } else {
        return Err(OsAudioError("no wpctl/pactl available".into()));
    };
    // Use the device_path as the source name if it already looks like one,
    // else try to find a source whose description contains the card token.
    let source = resolve_source(tool, device_path)
        .ok_or_else(|| OsAudioError("no matching Pulse/PipeWire source".into()))?;
    Ok(PulseControl { source, tool })
}

fn resolve_source(tool: Tool, device_path: &str) -> Option<String> {
    match tool {
        Tool::Pactl => {
            let out = Command::new("pactl").args(["list", "sources"]).output().ok()?;
            let text = String::from_utf8_lossy(&out.stdout);
            // Prefer an exact device_path match; else first non-monitor source.
            for block in text.split("Source #") {
                if block.contains(device_path) {
                    return block
                        .lines()
                        .find_map(|l| l.trim().strip_prefix("Name:"))
                        .map(|n| n.trim().to_string());
                }
            }
            None
        }
        // wpctl resolution is by node id; for v1 fall back to pactl-style
        // names if PulseAudio compat (`pactl`) is present, else None.
        Tool::Wpctl => {
            if have("pactl") {
                return resolve_source(Tool::Pactl, device_path);
            }
            None
        }
    }
}

impl OsAudio for PulseControl {
    fn capability(&mut self) -> CaptureControl {
        let out = Command::new("pactl").args(["list", "sources"]).output();
        let parsed = out
            .ok()
            .map(|o| String::from_utf8_lossy(&o.stdout).into_owned())
            .and_then(|t| parse_pactl_source(&t, &self.source));
        match parsed {
            Some(p) => CaptureControl {
                backend: Backend::Pulse,
                has_volume: true,
                has_mute: true,
                min_db: Some(-60.0),
                max_db: Some(0.0),
                current_db: Some(p.db),
                muted: Some(p.muted),
            },
            None => CaptureControl {
                backend: Backend::Pulse,
                has_volume: false,
                has_mute: false,
                min_db: None,
                max_db: None,
                current_db: None,
                muted: None,
            },
        }
    }

    fn set_capture_db(&mut self, db: f32) -> Result<(), OsAudioError> {
        // PulseAudio accepts dB directly: `pactl set-source-volume <src> <db>dB`.
        let status = Command::new("pactl")
            .args(["set-source-volume", &self.source, &format!("{db}dB")])
            .status()
            .map_err(|e| OsAudioError(e.to_string()))?;
        if status.success() {
            Ok(())
        } else {
            Err(OsAudioError("pactl set-source-volume failed".into()))
        }
    }

    fn set_mute(&mut self, mute: bool) -> Result<(), OsAudioError> {
        let v = if mute { "1" } else { "0" };
        let status = Command::new("pactl")
            .args(["set-source-mute", &self.source, v])
            .status()
            .map_err(|e| OsAudioError(e.to_string()))?;
        if status.success() {
            Ok(())
        } else {
            Err(OsAudioError("pactl set-source-mute failed".into()))
        }
    }
}
```

Add to `mod.rs`: `#[cfg(target_os = "linux")] pub(crate) mod pulse;`

- [ ] **Step 4: Run unit tests, verify pass**

Run: `cd graywolf-modem && cargo test --lib osaudio::pulse 2>&1 | tail -15` → PASS. `cargo clippy --lib -- -D warnings` clean.

- [ ] **Step 5: Manual live smoke test (documented)** — on a PipeWire/Pulse desktop, confirm `capability()` reads the source and `set_capture_db`/`set_mute` move it (watch `pavucontrol` / `wpctl status`). Note the `wpctl`-native node-id path is a known follow-up (§11 open item); v1 uses the `pactl` compat shim that PipeWire ships.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/osaudio/pulse.rs graywolf-modem/src/osaudio/mod.rs
git commit -m "feat(modem): PipeWire/Pulse capture-control back end (wpctl/pactl subprocess)"
```

---

### Task 4: Linux dispatch (`linux_open`)

**Files:**
- Modify: `graywolf-modem/src/osaudio/mod.rs` (replace the Task-1 stub)

- [ ] **Step 1: Write the failing test**

Add to `mod.rs` tests:

```rust
    #[test]
    #[cfg(target_os = "linux")]
    fn linux_open_falls_back_to_nocontrol_for_default() {
        // "default" has no ALSA card token and (in CI) no Pulse source -> NoControl.
        let mut c = linux_open("default", "ALSA");
        let cap = c.capability();
        assert_eq!(cap.backend, Backend::None);
    }
```

- [ ] **Step 2: Run, verify fail** (the stub returns `Backend::None` already, so this may pass against the stub — that's fine; the point is the real impl keeps the contract). Run: `cargo test --lib osaudio 2>&1 | tail`.

- [ ] **Step 3: Replace the stub with the real dispatch**

In `mod.rs`, replace the Task-1 `linux_open` stub with:

```rust
#[cfg(target_os = "linux")]
fn linux_open(device_path: &str, host_api: &str) -> Box<dyn OsAudio> {
    // PulseAudio/PipeWire host -> source control; ALSA host -> mixer. Try the
    // host-appropriate back end first, then the other, then NoControl.
    let prefer_pulse = host_api.eq_ignore_ascii_case("pulseaudio")
        || host_api.eq_ignore_ascii_case("pipewire")
        || host_api.eq_ignore_ascii_case("jack");
    if prefer_pulse {
        if let Ok(c) = pulse::open(device_path) {
            return Box::new(c);
        }
        if let Ok(c) = alsa::open(device_path) {
            return Box::new(c);
        }
    } else {
        if let Ok(c) = alsa::open(device_path) {
            return Box::new(c);
        }
        if let Ok(c) = pulse::open(device_path) {
            return Box::new(c);
        }
    }
    Box::new(NoControl {
        reason: format!("no ALSA/Pulse capture control for {device_path}"),
    })
}
```

- [ ] **Step 4: Run, verify pass** — `cargo test --lib osaudio 2>&1 | tail` PASS; `cargo clippy --lib -- -D warnings` clean.

- [ ] **Step 5: Commit**

```bash
git add graywolf-modem/src/osaudio/mod.rs
git commit -m "feat(modem): Linux osaudio dispatch (ALSA + Pulse, NoControl fallback)"
```

---

### Task 5: CLI subcommands

**Files:**
- Create: `graywolf-modem/src/osaudio/cli.rs`
- Modify: `graywolf-modem/src/osaudio/mod.rs` (`pub mod cli;`), `graywolf-modem/src/bin/graywolf_modem.rs`

Surface (mirrors the M1 `--decode`/`--record` style):
- `graywolf-modem --capture-control <device> [--host-api <api>]` → prints the `CaptureControl` JSON.
- `graywolf-modem --set-capture-db <device> <db> [--host-api <api>]`
- `graywolf-modem --set-capture-mute <device> <on|off> [--host-api <api>]`

- [ ] **Step 1: Write the failing test (arg parsing)**

Create `cli.rs` with tests:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_capture_control_args() {
        let a = parse(&["--capture-control".into(), "hw:CARD=Device,DEV=0".into()]).unwrap();
        assert!(matches!(a, Cmd::Capability { ref device, .. } if device == "hw:CARD=Device,DEV=0"));
    }

    #[test]
    fn parses_set_db_and_mute() {
        let a = parse(&["--set-capture-db".into(), "dev".into(), "-9".into()]).unwrap();
        assert!(matches!(a, Cmd::SetDb { db, .. } if (db + 9.0).abs() < 1e-6));
        let m = parse(&["--set-capture-mute".into(), "dev".into(), "on".into()]).unwrap();
        assert!(matches!(m, Cmd::SetMute { mute: true, .. }));
    }

    #[test]
    fn rejects_garbage() {
        assert!(parse(&["--capture-control".into()]).is_err());
        assert!(parse(&["--set-capture-mute".into(), "dev".into(), "maybe".into()]).is_err());
    }
}
```

- [ ] **Step 2: Run, verify fail** — `cargo test --lib osaudio::cli 2>&1 | tail`.

- [ ] **Step 3: Implement**

Prepend to `cli.rs`:

```rust
//! CLI surface for the osaudio capture-control subcommands. `run` returns the
//! process exit semantics; the binary maps Ok/Err to ExitCode.

use super::open;

pub enum Cmd {
    Capability { device: String, host_api: String },
    SetDb { device: String, host_api: String, db: f32 },
    SetMute { device: String, host_api: String, mute: bool },
}

fn host_api_of(args: &[String]) -> String {
    let mut i = 0;
    while i < args.len() {
        if args[i] == "--host-api" {
            return args.get(i + 1).cloned().unwrap_or_default();
        }
        i += 1;
    }
    String::new()
}

pub fn parse(args: &[String]) -> Result<Cmd, String> {
    let usage = "usage: --capture-control <dev> | --set-capture-db <dev> <db> | --set-capture-mute <dev> <on|off>  [--host-api <api>]";
    let flag = args.first().ok_or_else(|| usage.to_string())?;
    let host_api = host_api_of(args);
    let pos: Vec<&String> = args[1..].iter().filter(|a| !a.starts_with("--")).collect();
    // skip the value following --host-api in the positional filter:
    let pos: Vec<&String> = {
        let mut out = Vec::new();
        let mut skip = false;
        for a in &args[1..] {
            if skip { skip = false; continue; }
            if a == "--host-api" { skip = true; continue; }
            if a.starts_with("--") { continue; }
            out.push(a);
        }
        let _ = pos;
        out
    };
    match flag.as_str() {
        "--capture-control" => {
            let device = pos.first().ok_or_else(|| usage.to_string())?.to_string();
            Ok(Cmd::Capability { device, host_api })
        }
        "--set-capture-db" => {
            let device = pos.first().ok_or_else(|| usage.to_string())?.to_string();
            let db = pos.get(1).ok_or_else(|| usage.to_string())?
                .parse::<f32>().map_err(|_| "bad db value".to_string())?;
            Ok(Cmd::SetDb { device, host_api, db })
        }
        "--set-capture-mute" => {
            let device = pos.first().ok_or_else(|| usage.to_string())?.to_string();
            let mute = match pos.get(1).map(|s| s.as_str()) {
                Some("on") | Some("true") | Some("1") => true,
                Some("off") | Some("false") | Some("0") => false,
                _ => return Err(usage.to_string()),
            };
            Ok(Cmd::SetMute { device, host_api, mute })
        }
        _ => Err(usage.to_string()),
    }
}

/// Execute a parsed command, printing JSON for the capability query.
pub fn run(args: &[String]) -> Result<(), String> {
    let cmd = parse(args)?;
    match cmd {
        Cmd::Capability { device, host_api } => {
            let mut ctl = open(&device, &host_api);
            let cap = ctl.capability();
            println!("{}", serde_json::to_string_pretty(&cap).map_err(|e| e.to_string())?);
        }
        Cmd::SetDb { device, host_api, db } => {
            let mut ctl = open(&device, &host_api);
            ctl.set_capture_db(db).map_err(|e| e.to_string())?;
        }
        Cmd::SetMute { device, host_api, mute } => {
            let mut ctl = open(&device, &host_api);
            ctl.set_mute(mute).map_err(|e| e.to_string())?;
        }
    }
    Ok(())
}
```

(Clean up the duplicated `pos` binding into one tidy parser while implementing — the test asserts behavior, not the intermediate shape.) Add `pub mod cli;` to `mod.rs`.

- [ ] **Step 4: Wire into the binary**

In `graywolf-modem/src/bin/graywolf_modem.rs`, before `let server = bind_server(&args);`, add (matching the existing `--decode` block style):

```rust
    if args.len() >= 2
        && matches!(args[1].as_str(), "--capture-control" | "--set-capture-db" | "--set-capture-mute")
    {
        return match graywolfmodem::osaudio::cli::run(&args[1..]) {
            Ok(()) => ExitCode::SUCCESS,
            Err(e) => {
                eprintln!("osaudio: {e}");
                ExitCode::from(1)
            }
        };
    }
```

Also extend the `bind_server` usage text with the three new lines.

- [ ] **Step 5: Build + test + manual**

Run: `cd graywolf-modem && cargo test --lib osaudio::cli && cargo build --bin graywolf-modem && cargo clippy --lib -- -D warnings 2>&1 | tail`
Manual: `./target/debug/graywolf-modem --capture-control "$(graywolf-modem --list-audio | ...pick an input pcm_id...)"` prints JSON; on a device with no control it prints `"backend":"none","has_volume":false`.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/osaudio/cli.rs graywolf-modem/src/osaudio/mod.rs graywolf-modem/src/bin/graywolf_modem.rs
git commit -m "feat(modem): --capture-control/--set-capture-db/--set-capture-mute CLI"
```

---

### Task 6: macOS CoreAudio back end (verified on macOS)

**Files:**
- Create: `graywolf-modem/src/osaudio/coreaudio.rs`
- Modify: `graywolf-modem/src/osaudio/mod.rs` (`#[cfg(target_os="macos")] mod coreaudio;`), `graywolf-modem/Cargo.toml`

**Dependency:** add under a macOS target table in `Cargo.toml`:
```toml
[target.'cfg(target_os = "macos")'.dependencies]
coreaudio-sys = "0.2"
```
(Confirm the version cpal already resolves in `Cargo.lock` and match it to avoid a duplicate.)

**Contract & exact API (implement against this; verify on a Mac):**
- Resolve the cpal `device_path` (CoreAudio uses the device UID / name) to an `AudioDeviceID`: enumerate with `AudioObjectGetPropertyData` on `kAudioObjectSystemObject` for `kAudioHardwarePropertyDevices`, then match each device's `kAudioDevicePropertyDeviceUID` (or name) to `device_path`.
- Volume **in dB**: `AudioObjectGetPropertyData` / `...SetPropertyData` with `mSelector = kAudioDevicePropertyVolumeDecibels`, `mScope = kAudioObjectPropertyScopeInput`, `mElement = kAudioObjectPropertyElementMain` (channel 0; some devices are per-channel — read `...VolumeDecibelsRange` for min/max). If the device lacks the property (`AudioObjectHasProperty` is false), return `has_volume:false`.
- Mute: `kAudioDevicePropertyMute` (UInt32 0/1), input scope.
- Map dB get/set straight onto the trait. Clamp `set_capture_db` to the `...VolumeDecibelsRange`.

- [ ] **Step 1:** Add the dep; write a unit test for the *pure* parts you can test off-device (e.g. a `clamp_db` reused from a shared helper, and UID-match selection given a `Vec<(AudioDeviceID, String)>`). Put device-touching code behind `open()`.
- [ ] **Step 2–4:** Implement `open(device_path) -> Result<CoreAudioControl, OsAudioError>` + `impl OsAudio`, using the selectors above. Build with `cargo check --target x86_64-apple-darwin` (and `aarch64-apple-darwin`) if a cross toolchain is available, else on a Mac.
- [ ] **Step 5 (on-platform verify):** On a Mac, `graywolf-modem --capture-control <input-uid>` prints sane min/max/current; `--set-capture-db` moves the slider in **Audio MIDI Setup**; devices with no input gain report `has_volume:false`.
- [ ] **Step 6: Commit** `feat(modem): macOS CoreAudio capture-control back end`.

---

### Task 7: Windows WASAPI back end (verified on Windows)

**Files:**
- Create: `graywolf-modem/src/osaudio/wasapi.rs`
- Modify: `graywolf-modem/src/osaudio/mod.rs` (`#[cfg(target_os="windows")] mod wasapi;`), `graywolf-modem/Cargo.toml`

**Dependency:** extend the existing `windows = { version = "0.59", features = [...] }` with:
```toml
    "Win32_Media_Audio",
    "Win32_Media_Audio_Endpoints",
    "Win32_System_Com",
    "Win32_System_Com_StructuredStorage",
    "Win32_UI_Shell_PropertiesSystem",
```
(Do **not** add `windows-sys`; reuse the one crate, §4.4.)

**Contract & exact API (implement against this; verify on Windows):**
- COM init once (`CoInitializeEx(None, COINIT_MULTITHREADED)`).
- `IMMDeviceEnumerator` (`CoCreateInstance` of `MMDeviceEnumerator`) → `EnumAudioEndpoints(eCapture, DEVICE_STATE_ACTIVE)` → match each `IMMDevice`'s `GetId()` / `PKEY_Device_FriendlyName` to the cpal `device_path`.
- Activate `IAudioEndpointVolume` on the matched device (`IMMDevice::Activate(IID_IAudioEndpointVolume, CLSCTX_ALL, None)`).
- Range/level **in dB**: `GetVolumeRange(&mut min, &mut max, &mut inc)` (dB); `GetMasterVolumeLevel() -> f32` (dB); `SetMasterVolumeLevel(db, None)`. Mute: `GetMute() -> BOOL`, `SetMute(mute, None)`. If activation fails (no endpoint volume), return `has_volume:false`.
- Clamp `set_capture_db` to `[min, max]`.

- [ ] **Step 1:** Add the features; write unit tests for the pure helpers (`clamp_db`, endpoint-id matching given a list).
- [ ] **Step 2–4:** Implement `open(device_path) -> Result<WasapiControl, OsAudioError>` + `impl OsAudio` with the calls above. Build with `cargo check --target x86_64-pc-windows-msvc` if a cross toolchain is available, else on Windows.
- [ ] **Step 5 (on-platform verify):** On Windows, `--capture-control <endpoint>` prints sane range/level; `--set-capture-db`/`--set-capture-mute` move the slider in **Sound > Recording > Properties > Levels**; a device with no level control reports `has_volume:false`.
- [ ] **Step 6: Commit** `feat(modem): Windows WASAPI capture-control back end`.

---

### Task 8: Verification sweep + docs

**Files:** `docs/wiki/code-map.md`, `docs/wiki/glossary.md`, `docs/handbook/audio.html`

- [ ] **Step 1:** From repo root: `cargo check --workspace`, `cargo clippy --workspace -- -D warnings`, `cargo test -p graywolf-demod` — all green on Linux. (macOS/Windows back ends are `cfg`-gated, so they don't build on Linux; confirm they at least `cargo check` on their targets per Tasks 6/7.)
- [ ] **Step 2:** `code-map.md` — add rows under the Rust-modem table: the `osaudio` module + each back end file + the three CLI flags.
- [ ] **Step 3:** `glossary.md` (Diagnostics or Hardware/OS section) — an entry for "OS capture control" describing the `OsAudio` trait, the `CaptureControl` JSON, the `supported=false` fallback, and the per-OS back ends, pointing at `src/osaudio/`.
- [ ] **Step 4:** `audio.html` — a short operator note that the modem can read/set the OS capture level from the CLI (the `--capture-control` / `--set-capture-db` / `--set-capture-mute` subcommands), framed as a device-level companion to the dashboard gain (no MCP mention).
- [ ] **Step 5: Commit** `docs: document the osaudio capture-control layer (wiki + handbook)`.

---

## Self-review

- **Spec coverage (§4.2/§4.4/§12 M2):** `OsAudio` trait + `CaptureControl` (Task 1); ALSA via the patched crate (Task 2); PipeWire/Pulse subprocess (Task 3); CoreAudio (Task 6); WASAPI (Task 7); device→control mapping via `alsa_card_token` / per-OS UID/endpoint match (Tasks 2/6/7); honest `supported=false` everywhere (NoControl + each back end's failure path); `--get/--set` capture level + mute CLI (Task 5). ✓ All four back ends present; Day-1 three-OS mandate met (with the verification boundary called out).
- **No new linked deps on Linux:** ALSA reuses the patched crate; Pulse is subprocess. macOS reuses `coreaudio-sys` (transitive); Windows extends the existing `windows` crate. ✓ (§4.4)
- **Placeholder scan:** the only "verify on platform" steps are Tasks 6/7's live checks — unavoidable for mac/win FFI and explicitly scoped, with concrete selectors/interfaces given (not "TODO"). The `cli.rs` parser has a deliberately-noted cleanup of a duplicated binding. No vague "add error handling."
- **Type consistency:** `OsAudio::{capability,set_capture_db,set_mute}`, `CaptureControl` field names, and `Backend` variants are used identically across Tasks 1–7. `open()`/`linux_open()`/`alsa::open()`/`pulse::open()` signatures line up. `MilliBel::from_db/to_db` and `alsa_card_token` are the verified real APIs.

## Open follow-ups (not blocking M2)

- Native `wpctl` node-id path (vs the `pactl` compat shim) — §11 open item.
- An armhf-t64 smoke test for the ALSA **mixer** calls (the t64 fix was for PCM htstamp; mixer is a different path but worth a canary), per §4.4.

## Execution handoff

This plan produces working, testable software on Linux on its own (Tasks 1–5), with the macOS/Windows back ends (Tasks 6–7) landing in the same milestone, verified on their platforms. Recommended: subagent-driven-development for Tasks 1–5 inline; Tasks 6–7 executed by an implementer with a Mac/Windows (or via cross-`check` + on-platform smoke test).
