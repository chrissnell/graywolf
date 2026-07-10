//! ADS-B (Mode S) pulse-position modulator.
//!
//! Mode S extended squitter transmits at 1090 MHz, 1 Mbit/s, using pulse
//! position modulation (PPM). Each data bit occupies one microsecond: a `1` is
//! a pulse in the first half-microsecond, a `0` a pulse in the second half.
//! Every frame is preceded by an 8 µs preamble with pulses at 0.0, 1.0, 3.5,
//! and 4.5 µs.
//!
//! The modulator emits a magnitude (envelope) sample stream — the same
//! representation a receiver recovers from the complex 1090 MHz IF — so the
//! output pairs directly with [`super::demodulator`]. Sampling is expressed as
//! `samples_per_us`, which must be even so that half-microsecond pulse edges
//! land on sample boundaries (2 samples/µs is the dump1090 convention).

use super::{DATA_SLOTS_PER_BIT, PREAMBLE_HIGH_SLOTS, PREAMBLE_SLOTS};

/// Builds Mode S PPM magnitude waveforms.
#[derive(Clone, Copy, Debug)]
pub struct Modulator {
    /// Samples per microsecond. Must be even and non-zero (default 2).
    pub samples_per_us: usize,
    /// Magnitude emitted during a pulse.
    pub high: u16,
    /// Magnitude emitted between pulses.
    pub low: u16,
}

impl Default for Modulator {
    fn default() -> Self {
        Self { samples_per_us: 2, high: 32767, low: 0 }
    }
}

impl Modulator {
    /// Modulator at `samples_per_us` (must be even) with full-scale pulses.
    pub fn new(samples_per_us: usize) -> Self {
        assert!(samples_per_us >= 2 && samples_per_us.is_multiple_of(2), "samples_per_us must be even and >= 2");
        Self { samples_per_us, ..Self::default() }
    }

    /// Samples in one half-microsecond slot.
    fn slot_len(&self) -> usize {
        self.samples_per_us / 2
    }

    fn push_slot(&self, out: &mut Vec<u16>, high: bool) {
        let v = if high { self.high } else { self.low };
        for _ in 0..self.slot_len() {
            out.push(v);
        }
    }

    /// Modulate a raw Mode S frame (7 or 14 bytes) into a magnitude waveform.
    ///
    /// The frame is transmitted verbatim — parity must already be present (see
    /// [`super::crc::append_parity`]). Output = preamble + PPM-encoded bits.
    pub fn modulate(&self, frame: &[u8]) -> Vec<u16> {
        let nbits = frame.len() * 8;
        let total_slots = PREAMBLE_SLOTS + nbits * DATA_SLOTS_PER_BIT;
        let mut out = Vec::with_capacity(total_slots * self.slot_len());

        // Preamble: 16 half-µs slots, pulses at slots 0, 2, 7, 9.
        for slot in 0..PREAMBLE_SLOTS {
            self.push_slot(&mut out, PREAMBLE_HIGH_SLOTS.contains(&slot));
        }

        // Data: MSB-first. bit=1 -> pulse in first half, bit=0 -> second half.
        for &byte in frame {
            for bit in (0..8).rev() {
                let one = (byte >> bit) & 1 == 1;
                self.push_slot(&mut out, one);
                self.push_slot(&mut out, !one);
            }
        }

        out
    }

    /// Modulate with `lead`/`trail` microseconds of silence around the frame,
    /// so a demodulator scanning a longer buffer has quiet margins to lock onto.
    pub fn modulate_padded(&self, frame: &[u8], lead_us: usize, trail_us: usize) -> Vec<u16> {
        let mut out = vec![self.low; lead_us * self.samples_per_us];
        out.extend(self.modulate(frame));
        out.extend(std::iter::repeat_n(self.low, trail_us * self.samples_per_us));
        out
    }
}
