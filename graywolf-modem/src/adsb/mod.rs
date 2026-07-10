//! ADS-B (Mode S) modulator and demodulator.
//!
//! Mode S extended squitter is the 1090 MHz downlink that carries ADS-B: a
//! 1 Mbit/s pulse-position-modulated signal with a fixed 8 µs preamble and a
//! 56- or 112-bit body protected by a 24-bit CRC. This module provides the
//! physical layer in both directions plus the message parsing needed to make
//! it useful:
//!
//! - [`crc`] — Mode S 24-bit parity (dump1090-compatible).
//! - [`modulator::Modulator`] — bits → PPM magnitude waveform.
//! - [`demodulator::Demodulator`] — magnitude waveform → frames (preamble
//!   correlation, PPM slicing, CRC gating).
//! - [`message`] — DF / ICAO / type code / callsign / airborne-position (CPR)
//!   decoding and frame construction, following the `adsb_deku` reference
//!   layout (<https://github.com/rsadsb/adsb_deku>).
//!
//! ## Signal model
//!
//! Sampling is expressed as `samples_per_us` (even; 2 samples/µs is the
//! dump1090 convention). The waveform is a magnitude/envelope stream — the same
//! representation a receiver recovers from the complex 1090 MHz IF — so the
//! modulator output feeds straight into the demodulator.
//!
//! ```text
//! bits ─► Modulator ─► magnitude samples ─► Demodulator ─► frames ─► message::Frame
//! ```

pub mod crc;
pub mod demodulator;
pub mod message;
pub mod modulator;

#[cfg(test)]
mod tests;

pub use demodulator::{DemodFrame, Demodulator};
pub use message::{AirbornePosition, Frame};
pub use modulator::Modulator;

/// Short Mode S frame length in bits (DF 0/4/5/11/…).
pub const SHORT_FRAME_BITS: usize = 56;
/// Long (extended squitter) frame length in bits (DF 16/17/18/…).
pub const LONG_FRAME_BITS: usize = 112;

/// Half-microsecond slots in the 8 µs preamble.
pub const PREAMBLE_SLOTS: usize = 16;
/// Half-microsecond slots per data bit (one pulse position pair).
pub const DATA_SLOTS_PER_BIT: usize = 2;

/// Preamble slots that carry a pulse — pulses at 0.0, 1.0, 3.5, 4.5 µs.
pub const PREAMBLE_HIGH_SLOTS: [usize; 4] = [0, 2, 7, 9];
/// Preamble slots that must be quiet.
pub const PREAMBLE_LOW_SLOTS: [usize; 12] = [1, 3, 4, 5, 6, 8, 10, 11, 12, 13, 14, 15];

/// True when downlink format `df` denotes a 112-bit (long) frame.
pub fn long_frame_df(df: u8) -> bool {
    matches!(df, 16 | 17 | 18 | 19 | 20 | 21 | 24 | 25 | 26 | 27 | 28 | 29 | 30 | 31)
}
