//! 9600 baud G3RUH modem.
//!
//! Implements G3RUH scrambling/descrambling for 9600 baud packet radio per
//! direwolf's `demod_9600.c`. Uses baseband (direct FSK) demodulation with
//! a simple low-pass filter, clock recovery via DPLL, and the G3RUH/K9NG
//! scrambler polynomial x^17 + x^12 + 1.

use crate::dsp;
use crate::hdlc::{DecodedFrame, HdlcDecoder};
use crate::types::*;

/// G3RUH scrambler polynomial: x^17 + x^12 + 1.
/// Taps at bits 16 and 11 (0-indexed from LSB).
const SCRAMBLE_TAP_HIGH: u32 = 16;
const SCRAMBLE_TAP_LOW: u32 = 11;

/// G3RUH scramble one bit for transmit.
#[inline]
pub fn scramble_bit(input: u8, state: &mut u32) -> u8 {
    let feedback = ((*state >> SCRAMBLE_TAP_HIGH) ^ (*state >> SCRAMBLE_TAP_LOW)) & 1;
    let out = (input ^ feedback as u8) & 1;
    *state = (*state << 1) | out as u32;
    out
}

/// G3RUH descramble one bit on receive (equivalent to hdlc.rs descramble).
#[inline]
pub fn descramble_bit(input: u8, state: &mut u32) -> u8 {
    let out = (input ^ (*state >> SCRAMBLE_TAP_HIGH) as u8 ^ (*state >> SCRAMBLE_TAP_LOW) as u8) & 1;
    *state = (*state << 1) | (input & 1) as u32;
    out
}

/// Scramble a byte buffer for TX. Returns scrambled bytes.
pub fn scramble_bytes(data: &[u8], state: &mut u32) -> Vec<u8> {
    let mut out = Vec::with_capacity(data.len());
    for &byte in data {
        let mut scrambled_byte = 0u8;
        for bit_pos in 0..8 {
            let bit = (byte >> bit_pos) & 1;
            let s = scramble_bit(bit, state);
            scrambled_byte |= s << bit_pos;
        }
        out.push(scrambled_byte);
    }
    out
}

pub struct Demod9600 {
    #[allow(dead_code)]
    sample_rate: u32,
    #[allow(dead_code)]
    baud: u32,
    #[allow(dead_code)]
    chan: usize,
    #[allow(dead_code)]
    subchan: usize,

    // LPF for baseband
    lpf_coeffs: Vec<f32>,
    lpf_buf: Vec<f32>,
    lpf_idx: usize,
    lpf_len: usize,

    // DPLL
    pll_step: i32,
    pll_acc: i32,
    prev_pll_acc: i32,

    // Previous sample for zero-crossing
    prev_sample: f32,
    prev_demod_data: i32,

    // DCD
    good_flag: bool,
    bad_flag: bool,
    good_hist: u8,
    bad_hist: u8,
    score: u32,
    data_detect: bool,

    // HDLC (with scrambled=true)
    hdlc: HdlcDecoder,
    decoded_frames: Vec<DecodedFrame>,

    // PLL tracking
    pll_nudge_total: i64,
    pll_symbol_count: i32,

    // PLL inertia
    locked_inertia: f32,
    searching_inertia: f32,
}

impl Demod9600 {
    pub fn new(sample_rate: u32, baud: u32, chan: usize, subchan: usize) -> Self {
        let baud = if baud == 0 { 9600 } else { baud };
        let sps = sample_rate as f32 / baud as f32;

        // LPF: cutoff at ~0.4 * baud rate, width ~1.2 symbols
        let lpf_width_sym = 1.2f32;
        let lpf_len = ((lpf_width_sym * sps) as usize) | 1;
        let lpf_len = lpf_len.min(MAX_FILTER_SIZE - 1);
        let mut lpf_coeffs = vec![0.0f32; lpf_len];
        let fc = baud as f32 * 0.4 / sample_rate as f32;
        dsp::gen_lowpass(fc, &mut lpf_coeffs, WindowType::Cosine);

        // DPLL step per sample
        let pll_step = (TICKS_PER_PLL_CYCLE * baud as f64 / sample_rate as f64).round() as i32;

        Self {
            sample_rate,
            baud,
            chan,
            subchan,
            lpf_coeffs,
            lpf_buf: vec![0.0; lpf_len],
            lpf_idx: 0,
            lpf_len,
            pll_step,
            pll_acc: 0,
            prev_pll_acc: 0,
            prev_sample: 0.0,
            prev_demod_data: 0,
            good_flag: false,
            bad_flag: false,
            good_hist: 0,
            bad_hist: 0,
            score: 0,
            data_detect: false,
            hdlc: HdlcDecoder::new(chan, subchan, 0, true), // scrambled=true
            decoded_frames: Vec::new(),
            pll_nudge_total: 0,
            pll_symbol_count: 0,
            locked_inertia: 0.74,
            searching_inertia: 0.50,
        }
    }

    /// Process one audio sample.
    pub fn process_sample(&mut self, sam: i32) {
        let fsam = sam as f32 / 16384.0;

        // Push into LPF ring buffer
        let idx = self.lpf_idx % self.lpf_len;
        self.lpf_buf[idx] = fsam;
        self.lpf_idx += 1;

        // Apply LPF
        let mut filtered = 0.0f32;
        for k in 0..self.lpf_len {
            let buf_idx = (self.lpf_idx + k) % self.lpf_len;
            filtered += self.lpf_buf[buf_idx] * self.lpf_coeffs[k];
        }

        // DPLL
        self.prev_pll_acc = self.pll_acc;
        self.pll_acc = (self.pll_acc as u32).wrapping_add(self.pll_step as u32) as i32;

        // Bit sampling on overflow
        if self.pll_acc < 0 && self.prev_pll_acc > 0 {
            let raw_bit = filtered > 0.0;
            if let Some(frame) = self.hdlc.process_bit(
                raw_bit,
                &mut self.pll_nudge_total,
                &mut self.pll_symbol_count,
            ) {
                self.decoded_frames.push(frame);
            }
            self.pll_symbol_count += 1;

            // DCD scoring
            self.good_hist = (self.good_hist << 1) | u8::from(self.good_flag);
            self.good_flag = false;
            self.bad_hist = (self.bad_hist << 1) | u8::from(self.bad_flag);
            self.bad_flag = false;
            let good_count = self.good_hist.count_ones() as i32;
            let bad_count = self.bad_hist.count_ones() as i32;
            self.score = (self.score << 1) | u32::from(good_count - bad_count >= 2);
            let popcount = self.score.count_ones();
            self.data_detect = popcount >= DCD_THRESH_ON
                || (popcount > DCD_THRESH_OFF && self.data_detect);
        }

        // Transition detection for PLL nudge
        let demod_data = i32::from(filtered > 0.0);
        if demod_data != self.prev_demod_data {
            let threshold = DCD_GOOD_WIDTH * 1024 * 1024;
            if self.pll_acc > -threshold && self.pll_acc < threshold {
                self.good_flag = true;
            } else {
                self.bad_flag = true;
            }

            let before = self.pll_acc as i64;
            let inertia = if self.data_detect {
                self.locked_inertia
            } else {
                self.searching_inertia
            };
            self.pll_acc = (self.pll_acc as f32 * inertia) as i32;
            let after = self.pll_acc as i64;
            self.pll_nudge_total += after - before;
        }
        self.prev_demod_data = demod_data;
        self.prev_sample = filtered;
    }

    pub fn take_frames(&mut self) -> Vec<DecodedFrame> {
        std::mem::take(&mut self.decoded_frames)
    }

    pub fn take_bad_fcs(&mut self) -> u64 {
        self.hdlc.take_bad_fcs()
    }

    pub fn data_detect(&self) -> bool {
        self.data_detect
    }

    pub fn set_fix_bits(&mut self, level: RetryType) {
        self.hdlc.set_fix_bits(level);
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scramble_descramble_roundtrip() {
        let data = b"Hello, G3RUH!";
        let mut enc_state = 0u32;
        let scrambled = scramble_bytes(data, &mut enc_state);

        // Descramble
        let mut dec_state = 0u32;
        let mut recovered = Vec::new();
        for &byte in &scrambled {
            let mut out_byte = 0u8;
            for bit_pos in 0..8 {
                let bit = (byte >> bit_pos) & 1;
                let d = descramble_bit(bit, &mut dec_state);
                out_byte |= d << bit_pos;
            }
            recovered.push(out_byte);
        }
        assert_eq!(&recovered, data);
    }

    #[test]
    fn demod_9600_creates() {
        let _d = Demod9600::new(48000, 9600, 0, 0);
    }

    #[test]
    fn demod_9600_silence() {
        let mut d = Demod9600::new(48000, 9600, 0, 0);
        for _ in 0..48000 {
            d.process_sample(0);
        }
        assert!(d.take_frames().is_empty());
        assert!(!d.data_detect());
    }

    #[test]
    fn scramble_bit_sequence() {
        // Verify scramble/descramble with a seeded state
        let mut state = 0x1FFFF_u32; // all ones initial state
        let mut out_bits = Vec::new();
        for _ in 0..100 {
            out_bits.push(scramble_bit(0, &mut state));
        }
        // With non-zero initial state, scrambler should produce non-trivial output
        let ones: usize = out_bits.iter().map(|&b| b as usize).sum();
        assert!(ones > 10, "scrambler output too uniform: {} ones in 100 bits", ones);
    }
}
