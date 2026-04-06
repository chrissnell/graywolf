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
//! use direwolf_demod::demod_afsk::AfskDemodulator;
//! use direwolf_demod::types::AfskProfile;
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
pub mod ipc;
pub mod audio;
pub mod modem;
pub mod sdr;
pub mod modem_psk;
pub mod modem_9600;
pub mod fx25;
pub mod il2p;
