//! # graywolf-demod
//!
//! High-performance AFSK (Audio Frequency Shift Keying) demodulator, ported
//! from the [Dire Wolf](https://github.com/wb2osz/direwolf) amateur radio
//! packet TNC by John Langner, WB2OSZ.
//!
//! ## Overview
//!
//! This crate decodes Bell 202 AFSK modulated AX.25 packet radio signals from
//! raw audio samples. It is a faithful port of the Dire Wolf 1.7 demodulator
//! and is intended to serve as the foundation for a complete Rust reimplementation.
//!
//! ## Capabilities
//!
//! - **Profile A** — Mark/space amplitude comparison with IIR automatic gain control
//! - **Profile B** — FM discriminator using instantaneous phase-rate measurement
//! - **Multi-slicer** — Parallel decision thresholds for robust decoding
//! - **Bit error correction** — Single and double bit-flip retry strategies
//! - **DCD** — Data Carrier Detect based on DPLL phase alignment
//!
//! ## Signal Flow
//!
//! ```text
//! Audio Samples (i16 / i32)
//!     │
//!     ▼
//! ┌─────────────────┐
//! │  Bandpass Filter │  Isolate AFSK mark/space band
//! │   (prefilter)    │
//! └────────┬────────┘
//!          │
//!          ▼
//! ┌─────────────────┐
//! │   Demodulator    │  Profile A: dual-LO mixing + RRC + AGC
//! │                  │  Profile B: single-LO FM discriminator
//! └────────┬────────┘
//!          │ analog demod output
//!          ▼
//! ┌─────────────────┐
//! │   DPLL / DCD     │  Clock recovery via digital PLL
//! │                  │  Data Carrier Detect scoring
//! └────────┬────────┘
//!          │ raw bits at baud rate
//!          ▼
//! ┌─────────────────┐
//! │   HDLC Decoder   │  NRZI → data bits → flag/abort/stuff detection
//! │                  │  FCS-16 validation, retry strategies
//! └────────┬────────┘
//!          │
//!          ▼
//!    DecodedFrame { data, chan, subchan, slice, quality, ... }
//! ```
//!
//! ## Quick Start
//!
//! ```rust
//! use graywolfmodem::demod_afsk::AfskDemodulator;
//! use graywolfmodem::types::AfskProfile;
//!
//! let mut demod = AfskDemodulator::new(
//!     44100,  // sample rate
//!     1200,   // baud rate
//!     1200,   // mark frequency
//!     2200,   // space frequency
//!     AfskProfile::A,
//!     0, 0,   // chan, subchan
//! );
//!
//! // Feed audio samples one at a time
//! for sample in [0i32; 1024] {
//!     demod.process_sample(sample);
//! }
//!
//! // Collect any decoded frames
//! let frames = demod.take_frames();
//! ```

#![allow(clippy::needless_range_loop)]
#![allow(clippy::manual_range_contains)]

pub mod types;
pub mod filter_buf;
pub mod dsp;
pub mod state;
pub mod hdlc;
pub mod demod_afsk;
pub mod demod_afsk_multi;
pub mod ipc;
pub mod audio;
pub mod modem;
pub mod sdr;
pub mod modem_psk;
pub mod modem_9600;
pub mod fx25;
pub mod il2p;
pub mod tx;
// CM108 HID PTT and USB topology enumeration depend on hidapi / nusb,
// which require host-OS facilities (libudev / IOKit / SetupAPI) that
// aren't present on Android. The Android target uses different audio
// routing entirely (USB-Audio class via OTG), so neither module is
// reachable from the POC-A binary.
#[cfg(not(target_os = "android"))]
pub mod cm108;
pub mod list_audio;
#[cfg(not(target_os = "android"))]
pub mod list_usb;
pub mod rxonly;
#[cfg(target_os = "android")]
pub mod android;
// On the host with android-test-stub, android/mod.rs is not compiled
// (jni crate is android-only), but upcall.rs and audio_tx.rs are self-contained
// and only need std. Pull them in directly so stub-mode unit tests are reachable.
#[cfg(all(feature = "android-test-stub", not(target_os = "android")))]
#[path = "android/upcall.rs"]
pub mod android_upcall;
#[cfg(all(feature = "android-test-stub", not(target_os = "android")))]
#[path = "android/audio_tx.rs"]
pub mod android_audio_tx;

// Unified cross-cfg re-exports so T3/T4 callers use `crate::jni_ptt_set`
// and `crate::jni_tx_push_samples` unconditionally inside their own cfg blocks.
#[cfg(target_os = "android")]
pub(crate) use android::upcall::{jni_ptt_set, jni_tx_push_samples};
#[cfg(all(not(target_os = "android"), feature = "android-test-stub"))]
pub(crate) use android_upcall::{jni_ptt_set, jni_tx_push_samples};

// Test-hook re-exports: single import path for T3/T4 unit tests.
#[cfg(feature = "android-test-stub")]
pub use android_upcall::{clear_mocks, install_audio_tx_mock, install_ptt_mock};

/// Base semver string ("0.7.13"), injected at build time from the repo's
/// VERSION file (via the GRAYWOLF_VERSION env var set by the Makefile / CI).
pub const VERSION: &str = env!("GRAYWOLF_VERSION");

/// Short git commit hash, optionally suffixed with "-dirty" if the working
/// tree had uncommitted changes at build time. Injected via the
/// GRAYWOLF_GIT_COMMIT env var, or derived from `git rev-parse` by build.rs.
pub const GIT_COMMIT: &str = env!("GRAYWOLF_GIT_COMMIT");

/// Returns the full display-format version string, e.g. "v0.7.13-abcdef1"
/// or "v0.7.13-abcdef1-dirty". The Go parent process prints this at
/// startup and compares it to its own full version; any mismatch produces
/// a warning about a potentially inconsistent build.
pub fn full_version() -> String {
    format!("v{}-{}", VERSION, GIT_COMMIT)
}
