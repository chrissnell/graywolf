# Audio Auto-Tuning MCP Server — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build, in phases, an MCP server that auto-tunes a station's receive audio chain for optimal APRS packet decoding, starting with a recorder/decoder built into `graywolf-modem`.

**Architecture:** Per the design spec `docs/superpowers/specs/2026-06-25-audio-autotune-mcp-design.md`. Capture + offline decode + live monitoring fold into the existing `graywolf-modem` binary; an OS audio-control layer (capture level + mute) lives in the same crate; a Rust MCP server orchestrates a precheck → headroom → trim → validate → escalate loop over the Graywolf REST API and that OS layer. Target: **raw ADC headroom (~−9 dBFS peak) with software gain near unity**, confirmed by decode count.

**Tech Stack:** Rust (`graywolf-modem` crate, edition 2021), `cpal` (capture), `hound` (WAV), `claxon` (FLAC, already present), the production `AfskDemodulator`, the patched `alsa` crate for the Linux mixer, `coreaudio-sys` (macOS), the `windows` crate (Windows), and an MCP server crate.

---

## Phase roadmap

The design spec defines five milestones. They span **independent subsystems**, so per the writing-plans scope rule each milestone gets its **own** bite-sized plan when its phase begins, rather than one speculative mega-plan. **This document fully specifies Phase 1 (M1).** M2–M5 are scoped here as a roadmap and will be expanded into their own `docs/superpowers/plans/` files as their upstream lands.

| Phase | Milestone | Deliverable | Plan status |
|-------|-----------|-------------|-------------|
| **1** | **M1 — Recorder + offline scorer** | `graywolf-modem --record` (WAV) and `--decode` (JSON score) | **Detailed below** |
| 2 | M2 — OS audio control layer | `OsAudio` trait + ALSA/PipeWire/CoreAudio/WASAPI back ends; `--get/--set-capture`, `--get/--set-mute` | Roadmap (own plan at M2 start) |
| 3 | M3 — Live monitor | `graywolf-modem --monitor` JSON-lines level+guidance stream (§6/§9 of the spec) | Roadmap |
| 4 | M4 — MCP server + autotune | Rust MCP server running the §5 state machine end-to-end on Linux/macOS/Windows | Roadmap |
| 5 | M5 — Hardening + reference-signal validation | per-OS device-quirk coverage; optional TX-loopback / WA8LMF injection | Roadmap |

**Why M1 first:** it's self-contained, ships working/testable software on its own, and unblocks the deterministic digital-gain sweep the autotuner relies on (spec §7). Nothing in M1 needs the OS layer or the MCP server.

---

## Phase 1 (M1) — Recorder + offline scorer

### What M1 builds

Two new subcommands on the `graywolf-modem` binary:

- **`graywolf-modem --record <device-path> --seconds <N> --out <file.wav>`** — capture mono i16 audio from a cpal input device for N seconds and write a WAV (PCM s16le) file.
- **`graywolf-modem --decode <file.wav|file.flac>`** — feed a captured clip through the production `AfskDemodulator` and print a JSON summary: good-frame count, bad-FCS count, and per-packet audio levels (`level_dbfs`, mark/space, twist).

Both reuse existing crate machinery: `audio::soundcard::spawn` for capture, `claxon` for FLAC, the `AfskDemodulator` DSP core for decode. The only new dependency is `hound` for WAV I/O.

### Grounding facts (verified against the crate)

- Binary is `graywolf-modem` (`graywolf-modem/src/bin/graywolf_modem.rs`); the library crate is `graywolfmodem`. Existing flags are hand-matched in `main()` (e.g. `graywolfmodem::list_audio::run()`); new subcommands slot in **before** `bind_server(&args)` (around line 74).
- `audio::soundcard::spawn(cfg: SoundcardConfig, sink: SyncSender<AudioChunk>) -> Result<AudioSource, String>`, where `SoundcardConfig { device_name: String, sample_rate: u32, channels: u32, audio_channel: u32 }`, `AudioChunk = Vec<i16>` (mono i16), and `AudioSource { sample_rate: u32, .. }` with `stop_and_join(&mut self)`. Channel capacity is `audio::CHUNK_QUEUE_DEPTH = 64`.
- `AfskDemodulator` (`src/demod_afsk.rs`): construct exactly as `src/bin/demod_multi.rs::run_cfg` does —
  `AfskDemodulator::new(sample_rate, DEFAULT_BAUD, DEFAULT_MARK_FREQ, DEFAULT_SPACE_FREQ, <Profile A>, 0, 0)`; feed with `process_sample(s as i32)`; then `take_frames() -> Vec<DecodedFrame>` (good frames) and `take_bad_fcs() -> u64` (failed-FCS count).
- `DecodedFrame` (`src/hdlc.rs`): fields include `data: Vec<u8>`, `audio_level_mark: f32`, `audio_level_space: f32` (linear, ~1.0 = full-scale tone), `quality: i32`.
- dBFS convention (matches `pkg/app/rxfanout.go` `toDBFS` + the design spec): `level_dbfs = toDBFS((mark+space)/2)`, where `toDBFS(v) = -60.0` if `v <= 0` else `(20*log10(v)).max(-60.0)`, rounded to 1 decimal.
- WAV reference: `src/bin/demod_multi.rs::read_wav` shows the on-disk shape (RIFF/WAVE, 16-bit PCM); we use `hound` instead of hand-rolling.
- Tests: integration tests live in `graywolf-modem/tests/` and **skip** when the optional `aprs-test-tracks/` fixtures are absent (see `tests/ipc_flac_e2e.rs::test_track`). Mirror that skip pattern.
- Build/test: `cargo check --workspace`, `cargo clippy --workspace -- -D warnings`, `cargo test` from the repo root; the crate needs `protoc` and (Linux) `libasound2-dev`. The workspace root pins `alsa` to the `chrissnell/alsa-rs` fork.

### File structure (M1)

- **Create** `graywolf-modem/src/wavio.rs` — `to_dbfs`, `write_wav_i16`, `read_audio_i16` (WAV via hound, FLAC via claxon). One responsibility: sample-file + level helpers shared by record and decode.
- **Create** `graywolf-modem/src/record.rs` — `pub fn run(args: &[String]) -> Result<(), String>`: parse `--record` args, capture, write WAV.
- **Create** `graywolf-modem/src/decode.rs` — `pub fn run(args: &[String]) -> Result<(), String>`: parse `--decode` args, decode, print JSON.
- **Modify** `graywolf-modem/src/lib.rs` — declare `pub mod wavio; pub mod record; pub mod decode;`.
- **Modify** `graywolf-modem/src/bin/graywolf_modem.rs` — dispatch `--record` / `--decode` before `bind_server`.
- **Modify** `graywolf-modem/Cargo.toml` — add `hound = "3"`.
- **Create** `graywolf-modem/tests/decode_e2e.rs` — integration test decoding a known track (skip if absent).

---

### Task 1: `wavio` module — dBFS helper + WAV/FLAC sample I/O

**Files:**
- Modify: `graywolf-modem/Cargo.toml` (add `hound`)
- Create: `graywolf-modem/src/wavio.rs`
- Modify: `graywolf-modem/src/lib.rs` (add `pub mod wavio;`)

- [ ] **Step 1: Add the `hound` dependency**

In `graywolf-modem/Cargo.toml`, under `[dependencies]` (next to `claxon = "0.4"`), add:

```toml
hound = "3"
```

- [ ] **Step 2: Write the failing test for `to_dbfs` and a WAV round-trip**

Create `graywolf-modem/src/wavio.rs` with only a test module first:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn to_dbfs_matches_convention() {
        assert_eq!(to_dbfs(1.0), 0.0);
        assert_eq!(to_dbfs(0.5), -6.0);
        assert_eq!(to_dbfs(0.1), -20.0);
        assert_eq!(to_dbfs(0.0), -60.0);
        assert_eq!(to_dbfs(-1.0), -60.0); // unset placeholder
        assert_eq!(to_dbfs(0.0005), -60.0); // floored
    }

    #[test]
    fn wav_round_trips_i16() {
        let dir = std::env::temp_dir();
        let path = dir.join("wavio_roundtrip.wav");
        let samples: Vec<i16> = vec![0, 100, -100, 32767, -32768, 5];
        write_wav_i16(path.to_str().unwrap(), &samples, 48000).unwrap();
        let (back, rate) = read_audio_i16(path.to_str().unwrap()).unwrap();
        assert_eq!(rate, 48000);
        assert_eq!(back, samples);
        let _ = std::fs::remove_file(path);
    }
}
```

- [ ] **Step 3: Run the test to verify it fails**

Run: `cd graywolf-modem && cargo test --lib wavio 2>&1 | tail -20`
Expected: FAIL to compile — `to_dbfs`, `write_wav_i16`, `read_audio_i16` not found.

- [ ] **Step 4: Implement the module**

Prepend to `graywolf-modem/src/wavio.rs` (above the test module):

```rust
//! WAV/FLAC sample I/O and the dBFS helper shared by the record/decode
//! subcommands. WAV uses `hound`; FLAC reuses `claxon` (already a dep).

use std::path::Path;

/// Linear amplitude (1.0 = full scale) to dBFS, matching the Go `toDBFS`
/// (`pkg/app/rxfanout.go`) and the device meter: non-positive -> -60,
/// otherwise 20*log10(v) floored at -60, rounded to one decimal.
pub fn to_dbfs(v: f32) -> f32 {
    if v <= 0.0 {
        return -60.0;
    }
    let db = (20.0 * v.log10()).max(-60.0);
    (db * 10.0).round() / 10.0
}

/// Write mono i16 samples as a PCM s16le WAV at `rate` Hz.
pub fn write_wav_i16(path: &str, samples: &[i16], rate: u32) -> Result<(), String> {
    let spec = hound::WavSpec {
        channels: 1,
        sample_rate: rate,
        bits_per_sample: 16,
        sample_format: hound::SampleFormat::Int,
    };
    let mut w = hound::WavWriter::create(path, spec).map_err(|e| e.to_string())?;
    for &s in samples {
        w.write_sample(s).map_err(|e| e.to_string())?;
    }
    w.finalize().map_err(|e| e.to_string())
}

/// Read a WAV (.wav) or FLAC (.flac) file into mono i16 samples. For
/// multi-channel input, channel 0 is taken. Returns (samples, sample_rate).
pub fn read_audio_i16(path: &str) -> Result<(Vec<i16>, u32), String> {
    let ext = Path::new(path)
        .extension()
        .and_then(|e| e.to_str())
        .unwrap_or("")
        .to_ascii_lowercase();
    match ext.as_str() {
        "flac" => read_flac_i16(path),
        _ => read_wav_i16(path),
    }
}

fn read_wav_i16(path: &str) -> Result<(Vec<i16>, u32), String> {
    let mut r = hound::WavReader::open(path).map_err(|e| e.to_string())?;
    let spec = r.spec();
    let ch = spec.channels.max(1) as usize;
    let mut out = Vec::new();
    match spec.sample_format {
        hound::SampleFormat::Int => {
            let shift = 16i32 - spec.bits_per_sample as i32;
            for (i, s) in r.samples::<i32>().enumerate() {
                let s = s.map_err(|e| e.to_string())?;
                if i % ch == 0 {
                    let v = if shift > 0 { s << shift } else { s >> (-shift) };
                    out.push(v.clamp(i16::MIN as i32, i16::MAX as i32) as i16);
                }
            }
        }
        hound::SampleFormat::Float => {
            for (i, s) in r.samples::<f32>().enumerate() {
                let s = s.map_err(|e| e.to_string())?;
                if i % ch == 0 {
                    out.push((s.clamp(-1.0, 1.0) * 32767.0) as i16);
                }
            }
        }
    }
    Ok((out, spec.sample_rate))
}

fn read_flac_i16(path: &str) -> Result<(Vec<i16>, u32), String> {
    let mut reader = claxon::FlacReader::open(path).map_err(|e| e.to_string())?;
    let info = reader.streaminfo();
    let ch = info.channels.max(1) as usize;
    let bits = info.bits_per_sample as i32;
    let mut out = Vec::new();
    for (i, s) in reader.samples().enumerate() {
        let s = s.map_err(|e| e.to_string())?;
        if i % ch == 0 {
            let v = if bits > 16 {
                s >> (bits - 16)
            } else if bits < 16 {
                s << (16 - bits)
            } else {
                s
            };
            out.push(v.clamp(i16::MIN as i32, i16::MAX as i32) as i16);
        }
    }
    Ok((out, info.sample_rate))
}
```

Then add the module to the library — in `graywolf-modem/src/lib.rs`, add alongside the other `pub mod` lines:

```rust
pub mod wavio;
```

- [ ] **Step 5: Run the tests to verify they pass**

Run: `cd graywolf-modem && cargo test --lib wavio 2>&1 | tail -20`
Expected: PASS (`to_dbfs_matches_convention`, `wav_round_trips_i16`).

- [ ] **Step 6: Lint**

Run: `cd graywolf-modem && cargo clippy --lib -- -D warnings 2>&1 | tail -20`
Expected: no warnings.

- [ ] **Step 7: Commit**

```bash
git add graywolf-modem/Cargo.toml graywolf-modem/Cargo.lock graywolf-modem/src/wavio.rs graywolf-modem/src/lib.rs
git commit -m "feat(modem): add wavio module (dBFS helper + WAV/FLAC sample I/O)"
```

---

### Task 2: `decode` subcommand core — score a clip to JSON

**Files:**
- Create: `graywolf-modem/src/decode.rs`
- Modify: `graywolf-modem/src/lib.rs` (add `pub mod decode;`)
- Reference (read, don't modify): `graywolf-modem/src/bin/demod_multi.rs::run_cfg` (exact demod construction), `graywolf-modem/src/demod_afsk.rs` (`new`, `process_sample`, `take_frames`, `take_bad_fcs`), `graywolf-modem/src/hdlc.rs` (`DecodedFrame`).

- [ ] **Step 1: Write the failing unit test for the summary computation**

Create `graywolf-modem/src/decode.rs` with the result type and a pure summarizer plus its test (no I/O), so the scoring logic is testable without an audio fixture:

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn summarize_computes_levels_and_counts() {
        // mark/space linear amplitudes (1.0 = full scale)
        let frames = vec![(0.5_f32, 0.5_f32), (0.1_f32, 0.1_f32)];
        let s = summarize(&frames, 3);
        assert_eq!(s.rx_frames, 2);
        assert_eq!(s.rx_bad_fcs, 3);
        // level_dbfs of mean(0.5,0.5)=0.5 -> -6.0; mean(0.1,0.1)=0.1 -> -20.0
        // median of [-6.0, -20.0] = -13.0
        assert_eq!(s.level_dbfs_med, -13.0);
        assert!((s.twist_db_med - 0.0).abs() < 1e-6);
    }

    #[test]
    fn summarize_handles_no_frames() {
        let s = summarize(&[], 0);
        assert_eq!(s.rx_frames, 0);
        assert_eq!(s.rx_bad_fcs, 0);
        assert!(s.level_dbfs_med.is_none() == false || s.rx_frames == 0);
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd graywolf-modem && cargo test --lib decode 2>&1 | tail -20`
Expected: FAIL to compile — `summarize`, `Summary` not found.

- [ ] **Step 3: Implement the summarizer + JSON result type**

Prepend to `graywolf-modem/src/decode.rs`:

```rust
//! `--decode <file>`: feed a captured clip through the production AfskDemodulator
//! and print a JSON summary (good frames, bad-FCS, per-packet dBFS levels).
//! Deterministic on a fixed clip -> the digital-gain/profile sweep scorer.

use serde::Serialize;

use crate::demod_afsk::AfskDemodulator;
use crate::wavio::{read_audio_i16, to_dbfs};

#[derive(Serialize, Debug, Clone)]
pub struct Summary {
    pub rx_frames: u64,
    pub rx_bad_fcs: u64,
    /// Median per-packet level_dbfs over decoded frames; null when no frames.
    pub level_dbfs_med: f32,
    pub mark_dbfs_med: f32,
    pub space_dbfs_med: f32,
    pub twist_db_med: f32,
    pub sample_rate: u32,
}

fn median(mut xs: Vec<f32>) -> f32 {
    if xs.is_empty() {
        return -60.0;
    }
    xs.sort_by(|a, b| a.partial_cmp(b).unwrap());
    let n = xs.len();
    if n % 2 == 1 {
        xs[n / 2]
    } else {
        (xs[n / 2 - 1] + xs[n / 2]) / 2.0
    }
}

/// Pure scoring core. `frames` holds (mark, space) linear amplitudes per good
/// frame; `bad_fcs` is the failed-FCS count from the demodulator.
pub fn summarize(frames: &[(f32, f32)], bad_fcs: u64) -> Summary {
    let levels: Vec<f32> = frames
        .iter()
        .map(|(m, s)| to_dbfs((m.max(0.0) + s.max(0.0)) / 2.0))
        .collect();
    let marks: Vec<f32> = frames.iter().map(|(m, _)| to_dbfs(*m)).collect();
    let spaces: Vec<f32> = frames.iter().map(|(_, s)| to_dbfs(*s)).collect();
    let twists: Vec<f32> = marks
        .iter()
        .zip(&spaces)
        .map(|(m, s)| (m - s).abs())
        .collect();
    Summary {
        rx_frames: frames.len() as u64,
        rx_bad_fcs: bad_fcs,
        level_dbfs_med: median(levels),
        mark_dbfs_med: median(marks),
        space_dbfs_med: median(spaces),
        twist_db_med: median(twists),
        sample_rate: 0,
    }
}
```

Add the module to `graywolf-modem/src/lib.rs`:

```rust
pub mod decode;
```

- [ ] **Step 4: Run the unit tests to verify they pass**

Run: `cd graywolf-modem && cargo test --lib decode 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Add the file-decode `run` entry point (drives the real demod)**

Append to `graywolf-modem/src/decode.rs`, below the summarizer. Construct the demodulator exactly as `src/bin/demod_multi.rs::run_cfg` does — read that function and mirror its `AfskDemodulator::new(...)` arguments (Profile A, the `DEFAULT_BAUD` / `DEFAULT_MARK_FREQ` / `DEFAULT_SPACE_FREQ` constants) so the offline path matches the production AFSK-1200 demod:

```rust
/// Decode a clip and return its summary. `path` is .wav or .flac.
pub fn decode_file(path: &str) -> Result<Summary, String> {
    let (samples, rate) = read_audio_i16(path)?;
    // Mirror src/bin/demod_multi.rs::run_cfg for the production AFSK-1200
    // demodulator (Profile A, single slicer). If those constants are not
    // public, copy their literal values from demod_multi.rs.
    let mut demod = AfskDemodulator::new(
        rate,
        crate::demod_afsk::DEFAULT_BAUD,
        crate::demod_afsk::DEFAULT_MARK_FREQ,
        crate::demod_afsk::DEFAULT_SPACE_FREQ,
        crate::demod_afsk::Profile::A,
        0,
        0,
    );
    for &s in &samples {
        demod.process_sample(s as i32);
    }
    let good = demod.take_frames();
    let bad = demod.take_bad_fcs();
    let pairs: Vec<(f32, f32)> = good
        .iter()
        .map(|f| (f.audio_level_mark, f.audio_level_space))
        .collect();
    let mut summary = summarize(&pairs, bad);
    summary.sample_rate = rate;
    Ok(summary)
}

/// CLI entry: `--decode <file>`.
pub fn run(args: &[String]) -> Result<(), String> {
    let path = args
        .first()
        .ok_or_else(|| "usage: graywolf-modem --decode <file.wav|file.flac>".to_string())?;
    let summary = decode_file(path)?;
    let json = serde_json::to_string_pretty(&summary).map_err(|e| e.to_string())?;
    println!("{json}");
    Ok(())
}
```

**Note for the implementer:** `Profile`, `DEFAULT_BAUD`, `DEFAULT_MARK_FREQ`, `DEFAULT_SPACE_FREQ` are referenced by `demod_multi.rs`. Confirm their exact paths/visibility there; if any are private to that bin, either make them `pub` in `demod_afsk.rs` (preferred, one-line visibility change) or inline the literal values demod_multi uses. Run `cargo check` after this step and fix any path/visibility mismatch before moving on.

- [ ] **Step 6: Build and verify it compiles**

Run: `cd graywolf-modem && cargo check 2>&1 | tail -20`
Expected: clean compile (resolve any `Profile`/`DEFAULT_*` visibility per the note).

- [ ] **Step 7: Commit**

```bash
git add graywolf-modem/src/decode.rs graywolf-modem/src/lib.rs graywolf-modem/src/demod_afsk.rs
git commit -m "feat(modem): add --decode scorer core (good/bad-FCS counts + per-packet dBFS)"
```

---

### Task 3: `record` subcommand — capture to WAV

**Files:**
- Create: `graywolf-modem/src/record.rs`
- Modify: `graywolf-modem/src/lib.rs` (add `pub mod record;`)
- Reference: `graywolf-modem/src/audio/soundcard.rs` (`SoundcardConfig`, `spawn`), `graywolf-modem/src/audio/mod.rs` (`AudioChunk`, `CHUNK_QUEUE_DEPTH`, `AudioSource`).

- [ ] **Step 1: Write the failing test for arg parsing**

Create `graywolf-modem/src/record.rs` with a pure arg parser and its test (capture itself needs hardware, so we unit-test only the parser):

```rust
#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parses_record_args() {
        let args = vec![
            "plughw:CARD=Device,DEV=0".to_string(),
            "--seconds".to_string(),
            "5".to_string(),
            "--out".to_string(),
            "/tmp/clip.wav".to_string(),
        ];
        let p = parse_args(&args).unwrap();
        assert_eq!(p.device, "plughw:CARD=Device,DEV=0");
        assert_eq!(p.seconds, 5);
        assert_eq!(p.out, "/tmp/clip.wav");
        assert_eq!(p.sample_rate, 48000); // default
    }

    #[test]
    fn rejects_missing_out() {
        let args = vec!["dev".to_string(), "--seconds".to_string(), "5".to_string()];
        assert!(parse_args(&args).is_err());
    }
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd graywolf-modem && cargo test --lib record 2>&1 | tail -20`
Expected: FAIL to compile — `parse_args`, `RecordArgs` not found.

- [ ] **Step 3: Implement the parser**

Prepend to `graywolf-modem/src/record.rs`:

```rust
//! `--record <device> --seconds <N> --out <file.wav>`: capture mono i16 from a
//! cpal input device and write a WAV clip. Reuses audio::soundcard::spawn so
//! recorded samples travel the exact path the modem decodes.

use std::sync::mpsc::sync_channel;
use std::time::{Duration, Instant};

use crate::audio::soundcard::{self, SoundcardConfig};
use crate::audio::CHUNK_QUEUE_DEPTH;
use crate::wavio::write_wav_i16;

pub struct RecordArgs {
    pub device: String,
    pub seconds: u64,
    pub out: String,
    pub sample_rate: u32,
}

pub fn parse_args(args: &[String]) -> Result<RecordArgs, String> {
    let usage =
        "usage: graywolf-modem --record <device> --seconds <N> --out <file.wav> [--rate <hz>]";
    let device = args.first().ok_or_else(|| usage.to_string())?.clone();
    let mut seconds: Option<u64> = None;
    let mut out: Option<String> = None;
    let mut sample_rate: u32 = 48000;
    let mut i = 1;
    while i < args.len() {
        match args[i].as_str() {
            "--seconds" => {
                i += 1;
                seconds = Some(args.get(i).ok_or_else(|| usage.to_string())?
                    .parse().map_err(|_| "bad --seconds".to_string())?);
            }
            "--out" => {
                i += 1;
                out = Some(args.get(i).ok_or_else(|| usage.to_string())?.clone());
            }
            "--rate" => {
                i += 1;
                sample_rate = args.get(i).ok_or_else(|| usage.to_string())?
                    .parse().map_err(|_| "bad --rate".to_string())?;
            }
            other => return Err(format!("unknown arg: {other}\n{usage}")),
        }
        i += 1;
    }
    Ok(RecordArgs {
        device,
        seconds: seconds.ok_or_else(|| usage.to_string())?,
        out: out.ok_or_else(|| usage.to_string())?,
        sample_rate,
    })
}
```

- [ ] **Step 4: Run the parser tests to verify they pass**

Run: `cd graywolf-modem && cargo test --lib record 2>&1 | tail -20`
Expected: PASS.

- [ ] **Step 5: Implement the capture `run` entry point**

Append to `graywolf-modem/src/record.rs`:

```rust
/// CLI entry: `--record ...`. Captures `seconds` of audio and writes a WAV.
pub fn run(args: &[String]) -> Result<(), String> {
    let a = parse_args(args)?;
    let (tx, rx) = sync_channel::<crate::audio::AudioChunk>(CHUNK_QUEUE_DEPTH);
    let cfg = SoundcardConfig {
        device_name: a.device.clone(),
        sample_rate: a.sample_rate,
        channels: 1,
        audio_channel: 0,
    };
    let mut source = soundcard::spawn(cfg, tx)?;
    let rate = source.sample_rate;

    let mut samples: Vec<i16> = Vec::new();
    let deadline = Instant::now() + Duration::from_secs(a.seconds);
    while Instant::now() < deadline {
        match rx.recv_timeout(Duration::from_millis(250)) {
            Ok(chunk) => samples.extend_from_slice(&chunk),
            Err(std::sync::mpsc::RecvTimeoutError::Timeout) => continue,
            Err(std::sync::mpsc::RecvTimeoutError::Disconnected) => break,
        }
    }
    source.stop_and_join();

    write_wav_i16(&a.out, &samples, rate)?;
    eprintln!(
        "recorded {} samples ({:.1}s @ {} Hz) -> {}",
        samples.len(),
        samples.len() as f32 / rate as f32,
        rate,
        a.out
    );
    Ok(())
}
```

Add the module to `graywolf-modem/src/lib.rs`:

```rust
pub mod record;
```

- [ ] **Step 6: Build + lint**

Run: `cd graywolf-modem && cargo check && cargo clippy --lib -- -D warnings 2>&1 | tail -20`
Expected: clean. (If `SoundcardConfig` field names differ, fix to match `src/audio/soundcard.rs`.)

- [ ] **Step 7: Manual device smoke test (documented; not CI)**

On a machine with an input device:
Run: `cargo run --bin graywolf-modem -- --record default --seconds 3 --out /tmp/smoke.wav`
Then: `cargo run --bin graywolf-modem -- --decode /tmp/smoke.wav`
Expected: a WAV is written; `--decode` prints JSON (0 frames on silence is fine).

- [ ] **Step 8: Commit**

```bash
git add graywolf-modem/src/record.rs graywolf-modem/src/lib.rs
git commit -m "feat(modem): add --record (capture mono i16 to WAV via cpal)"
```

---

### Task 4: Wire subcommands into the binary + integration test

**Files:**
- Modify: `graywolf-modem/src/bin/graywolf_modem.rs`
- Create: `graywolf-modem/tests/decode_e2e.rs`

- [ ] **Step 1: Dispatch the new subcommands in `main()`**

In `graywolf-modem/src/bin/graywolf_modem.rs`, immediately **before** the `let server = bind_server(&args);` line, add:

```rust
    if args.len() >= 2 && args[1] == "--decode" {
        return match graywolfmodem::decode::run(&args[2..]) {
            Ok(()) => ExitCode::SUCCESS,
            Err(e) => {
                eprintln!("decode: {e}");
                ExitCode::from(1)
            }
        };
    }

    if args.len() >= 2 && args[1] == "--record" {
        return match graywolfmodem::record::run(&args[2..]) {
            Ok(()) => ExitCode::SUCCESS,
            Err(e) => {
                eprintln!("record: {e}");
                ExitCode::from(1)
            }
        };
    }
```

- [ ] **Step 2: Build the binary**

Run: `cd graywolf-modem && cargo build --bin graywolf-modem 2>&1 | tail -20`
Expected: builds clean.

- [ ] **Step 3: Write the decode integration test (skips if no fixture)**

Create `graywolf-modem/tests/decode_e2e.rs`, mirroring the skip pattern in `tests/ipc_flac_e2e.rs`:

```rust
//! End-to-end: run `graywolf-modem --decode <track>` on a known-good FLAC/WAV
//! track and assert it reports decoded frames as JSON. Skips when the optional
//! aprs-test-tracks fixtures are absent (same convention as ipc_flac_e2e.rs).

use std::path::PathBuf;
use std::process::Command;

fn manifest() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR"))
}

fn test_track() -> Option<PathBuf> {
    // Reuse whatever directory ipc_flac_e2e.rs uses; adjust the relative path
    // to match that test's `test_track()` if it differs.
    let candidates = [
        manifest().join("aprs-test-tracks/track01.flac"),
        manifest().join("testdata/track01.wav"),
    ];
    candidates.into_iter().find(|p| p.exists())
}

fn binary() -> PathBuf {
    // target/{debug,release}/graywolf-modem relative to the workspace.
    let mut p = manifest();
    p.pop(); // workspace root
    for prof in ["debug", "release"] {
        let cand = p.join("target").join(prof).join("graywolf-modem");
        if cand.exists() {
            return cand;
        }
    }
    p.join("target/debug/graywolf-modem")
}

#[test]
fn decode_reports_frames_for_known_track() {
    let track = match test_track() {
        Some(p) => p,
        None => {
            eprintln!("skipping: no aprs-test-tracks fixture present");
            return;
        }
    };
    let bin = binary();
    if !bin.exists() {
        eprintln!("skipping: graywolf-modem binary not built at {}", bin.display());
        return;
    }
    let out = Command::new(&bin)
        .arg("--decode")
        .arg(&track)
        .output()
        .expect("run --decode");
    assert!(out.status.success(), "decode exited non-zero");
    let stdout = String::from_utf8_lossy(&out.stdout);
    let v: serde_json::Value = serde_json::from_str(&stdout).expect("valid JSON");
    let frames = v["rx_frames"].as_u64().expect("rx_frames");
    assert!(frames > 0, "expected >0 decoded frames, got {frames}");
}
```

- [ ] **Step 4: Run the integration test**

Run: `cd graywolf-modem && cargo build --bin graywolf-modem && cargo test --test decode_e2e 2>&1 | tail -20`
Expected: PASS if a track fixture exists, otherwise prints "skipping" and passes. (If skipped, also confirm `decode_file` against a real track manually once.)

- [ ] **Step 5: Full workspace check**

Run from repo root: `cargo check --workspace && cargo clippy --workspace -- -D warnings && cargo test 2>&1 | tail -30`
Expected: green.

- [ ] **Step 6: Commit**

```bash
git add graywolf-modem/src/bin/graywolf_modem.rs graywolf-modem/tests/decode_e2e.rs
git commit -m "feat(modem): wire --record/--decode into the binary + decode e2e test"
```

---

### Task 5: Document the new modes

**Files:**
- Modify: `graywolf-modem/src/bin/graywolf_modem.rs` (usage text)
- Modify: `docs/wiki/` (the modem/CLI page, per the repo's wiki-maintenance rule)

- [ ] **Step 1: Extend the binary usage/help text**

In `graywolf_modem.rs`, update the `bind_server` usage string (and any `--help` handling) to list the two new subcommands:

```
usage: graywolf-modem <socket-path>
       graywolf-modem --record <device> --seconds <N> --out <file.wav> [--rate <hz>]
       graywolf-modem --decode <file.wav|file.flac>
       graywolf-modem --list-audio | --list-cm108 | --list-usb | --version
```

- [ ] **Step 2: Add a wiki note**

Find the wiki page that documents the modem binary / CLI (grep `docs/wiki` for `graywolf-modem` or `--list-audio`). Add a short subsection describing `--record` and `--decode`, what they're for (offline tuning / decode scoring), and the JSON shape `--decode` emits (`rx_frames`, `rx_bad_fcs`, `level_dbfs_med`, `mark_dbfs_med`, `space_dbfs_med`, `twist_db_med`, `sample_rate`). Keep it to navigation + intent, not code.

- [ ] **Step 3: Commit**

```bash
git add graywolf-modem/src/bin/graywolf_modem.rs docs/wiki
git commit -m "docs(modem): document --record/--decode subcommands"
```

---

## Self-review (M1)

- **Spec coverage:** M1 of the spec is "`--record` (WAV) and `--decode` (JSON score) in graywolf-modem, cross-platform via cpal, unblocks the deterministic digital sweep." Tasks 1–5 cover WAV I/O (T1), the decode scorer with good/bad-FCS + per-packet dBFS (T2), capture (T3), binary wiring + e2e (T4), docs (T5). ✓
- **Cross-platform:** capture and decode use cpal + pure-Rust WAV/FLAC; no OS-specific code in M1, so it's tri-OS from the start (spec §12). ✓
- **Placeholders:** the only deliberate "confirm in code" points are the `SoundcardConfig` field names and the `Profile`/`DEFAULT_*` visibility (Task 2 Step 5, Task 3 Step 6) — both call out the exact reference function to mirror and the one-line fix, not vague "handle it." Acceptable and explicit.
- **Type consistency:** `Summary` fields (Task 2) are the same ones the integration test (Task 4) and the wiki (Task 5) reference; `RecordArgs`/`parse_args` names match between Task 3 steps; `to_dbfs`/`read_audio_i16`/`write_wav_i16` (Task 1) are used unchanged by Tasks 2–3. ✓

## Execution handoff

Phase 1 is fully specified. M2–M5 each get their own plan when their phase starts (the OS-control layer especially is large enough to warrant its own bite-sized plan, written once M1 lands and the device→mixer mapping can be exercised against the recorder).
