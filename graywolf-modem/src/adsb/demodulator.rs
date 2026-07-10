//! ADS-B (Mode S) pulse-position demodulator.
//!
//! Operates on a magnitude (envelope) sample stream — the classic dump1090
//! approach. For each candidate offset it correlates against the 8 µs preamble,
//! then slices each 1 µs data bit by comparing its two half-microsecond slots
//! (first half stronger ⇒ `1`). The downlink format field selects the frame
//! length (56 or 112 bits) and the 24-bit CRC gates acceptance.

use super::crc;
use super::{
    long_frame_df, DATA_SLOTS_PER_BIT, LONG_FRAME_BITS, PREAMBLE_HIGH_SLOTS, PREAMBLE_LOW_SLOTS,
    PREAMBLE_SLOTS, SHORT_FRAME_BITS,
};

/// A demodulated Mode S frame.
#[derive(Clone, Debug, PartialEq, Eq)]
pub struct DemodFrame {
    /// Raw message bytes (7 or 14).
    pub bytes: Vec<u8>,
    /// Downlink format (top 5 bits of byte 0).
    pub df: u8,
    /// CRC residual — `0` for a clean extended-squitter frame.
    pub crc_residual: u32,
    /// Sample index of the preamble start.
    pub offset: usize,
}

impl DemodFrame {
    /// True when the extended-squitter CRC checks out.
    pub fn crc_ok(&self) -> bool {
        self.crc_residual == 0
    }
}

/// Decodes Mode S PPM from magnitude samples.
#[derive(Clone, Copy, Debug)]
pub struct Demodulator {
    /// Samples per microsecond. Private so the even/≥2 invariant established in
    /// [`Demodulator::new`] cannot be bypassed by a struct literal (which would
    /// make `slot_len` zero and panic on divide-by-zero).
    samples_per_us: usize,
    /// Accept only frames whose extended-squitter CRC is zero. When false,
    /// frames are returned regardless of residual (useful for DF0/4/5/11/20/21
    /// whose parity is overlaid with an address).
    pub require_crc: bool,
}

impl Default for Demodulator {
    fn default() -> Self {
        Self { samples_per_us: 2, require_crc: true }
    }
}

impl Demodulator {
    pub fn new(samples_per_us: usize) -> Self {
        assert!(samples_per_us >= 2 && samples_per_us.is_multiple_of(2), "samples_per_us must be even and >= 2");
        Self { samples_per_us, ..Self::default() }
    }

    /// Configured samples per microsecond.
    pub fn samples_per_us(&self) -> usize {
        self.samples_per_us
    }

    fn slot_len(&self) -> usize {
        self.samples_per_us / 2
    }

    /// Mean magnitude over the half-microsecond slot beginning at `sample`.
    fn slot_mag(&self, mag: &[u16], sample: usize) -> u32 {
        let len = self.slot_len();
        let mut sum = 0u32;
        for k in 0..len {
            sum += mag[sample + k] as u32;
        }
        sum / len as u32
    }

    /// Preamble match at offset `i`: every high slot must exceed every low slot.
    ///
    /// This strict correlation is tuned for a clean modulator/offline stream —
    /// on a noisy off-air envelope a single elevated low slot rejects the match.
    /// A field-grade detector would add a per-pulse threshold and margin; that
    /// belongs with the eventual live-pipeline wiring.
    fn preamble_ok(&self, mag: &[u16], i: usize) -> bool {
        let slot = self.slot_len();
        let mut high_min = u32::MAX;
        for &s in &PREAMBLE_HIGH_SLOTS {
            high_min = high_min.min(self.slot_mag(mag, i + s * slot));
        }
        if high_min == 0 {
            return false;
        }
        let mut low_max = 0u32;
        for &s in &PREAMBLE_LOW_SLOTS {
            low_max = low_max.max(self.slot_mag(mag, i + s * slot));
        }
        high_min > low_max
    }

    /// Slice `nbits` PPM data bits that follow the preamble at offset `i`.
    fn slice_bits(&self, mag: &[u16], i: usize, nbits: usize) -> Vec<u8> {
        let slot = self.slot_len();
        let data_start = i + PREAMBLE_SLOTS * slot;
        let mut bytes = vec![0u8; nbits / 8];
        for j in 0..nbits {
            let base = data_start + j * DATA_SLOTS_PER_BIT * slot;
            let first = self.slot_mag(mag, base);
            let second = self.slot_mag(mag, base + slot);
            if first > second {
                bytes[j / 8] |= 1 << (7 - (j % 8));
            }
        }
        bytes
    }

    /// Samples spanned by a full frame (preamble + `nbits` data) at this rate.
    fn frame_samples(&self, nbits: usize) -> usize {
        (PREAMBLE_SLOTS + nbits * DATA_SLOTS_PER_BIT) * self.slot_len()
    }

    /// Scan `mag` and return every accepted Mode S frame.
    pub fn demodulate(&self, mag: &[u16]) -> Vec<DemodFrame> {
        let mut frames = Vec::new();
        let long_span = self.frame_samples(LONG_FRAME_BITS);
        let short_span = self.frame_samples(SHORT_FRAME_BITS);
        let mut i = 0usize;
        while i + short_span <= mag.len() {
            if !self.preamble_ok(mag, i) {
                i += 1;
                continue;
            }

            // Read the DF from a short slice, then extend to a long frame when
            // the format demands it (and the buffer allows).
            let short_bytes = self.slice_bits(mag, i, SHORT_FRAME_BITS);
            let df = short_bytes[0] >> 3;
            let nbits = if long_frame_df(df) && i + long_span <= mag.len() {
                LONG_FRAME_BITS
            } else {
                SHORT_FRAME_BITS
            };

            let bytes = if nbits == SHORT_FRAME_BITS {
                short_bytes
            } else {
                self.slice_bits(mag, i, LONG_FRAME_BITS)
            };

            let residual = crc::checksum(&bytes);
            if !self.require_crc || residual == 0 {
                frames.push(DemodFrame { bytes, df, crc_residual: residual, offset: i });
                i += self.frame_samples(nbits);
            } else {
                i += 1;
            }
        }
        frames
    }
}
