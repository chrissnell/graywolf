//! PSK 2400/4800 baud demodulators.
//!
//! Implements V.26 / V.27 coherent PSK demodulation per direwolf's approach.
//! Supports QPSK (2400 baud) and 8-PSK (4800 baud) with Costas loop carrier
//! recovery and Gardner timing recovery.

use std::f32::consts::PI;

use crate::hdlc::{DecodedFrame, HdlcDecoder};
use crate::types::*;

/// Gray-coded QPSK constellation: phase → dibit mapping.
/// Phase quadrants: 0=00, π/2=01, π=11, 3π/2=10 (V.26 alternative B).
const QPSK_GRAY_B: [u8; 4] = [0b00, 0b01, 0b11, 0b10];
/// V.26 alternative A mapping.
const QPSK_GRAY_A: [u8; 4] = [0b00, 0b10, 0b11, 0b01];

/// 8-PSK constellation for V.27: phase → tribit.
const PSK8_GRAY: [u8; 8] = [0b001, 0b000, 0b010, 0b011, 0b111, 0b110, 0b100, 0b101];

/// PSK demodulator state.
pub struct PskDemodulator {
    #[allow(dead_code)]
    sample_rate: u32,
    #[allow(dead_code)]
    baud: u32,
    is_8psk: bool,
    v26_alt: V26Alternative,

    // Carrier NCO
    carrier_phase: f32,
    carrier_freq: f32,    // radians per sample
    carrier_nominal: f32, // nominal carrier freq (rad/sample)

    // Matched filter / LPF (RRC)
    i_filter: Vec<f32>,
    q_filter: Vec<f32>,
    rrc_coeffs: Vec<f32>,
    filter_len: usize,
    filter_idx: usize,

    // Timing recovery (Gardner)
    samples_per_symbol: f32,
    timing_acc: f32,
    prev_i: f32,
    prev_q: f32,
    mid_i: f32,
    mid_q: f32,

    // Differential decode
    prev_phase_idx: usize,

    // HDLC + output
    hdlc: HdlcDecoder,
    decoded_frames: Vec<DecodedFrame>,
    #[allow(dead_code)]
    chan: usize,
    #[allow(dead_code)]
    subchan: usize,

    // Bit accumulation for multi-bit symbols
    #[allow(dead_code)]
    bit_acc: u8,
    #[allow(dead_code)]
    bits_in_acc: usize,

    // DCD
    symbol_count: u32,
    good_symbols: u32,
    data_detect: bool,

    // PLL nudge tracking
    pll_nudge_total: i64,
    pll_symbol_count: i32,
}

impl PskDemodulator {
    pub fn new(
        sample_rate: u32,
        baud: u32,
        carrier_freq: u32,
        v26_alt: V26Alternative,
        chan: usize,
        subchan: usize,
    ) -> Self {
        let is_8psk = baud >= 4800;
        let carrier_rad = 2.0 * PI * carrier_freq as f32 / sample_rate as f32;
        let sps = sample_rate as f32 / baud as f32;

        // RRC filter: 4 symbol spans
        let rrc_span = 4.0;
        let filter_len = (rrc_span * sps) as usize | 1;
        let mut rrc_coeffs = vec![0.0f32; filter_len];
        let rolloff = 0.35;

        // Generate RRC coefficients
        for k in 0..filter_len {
            let t = (k as f32 - (filter_len as f32 - 1.0) / 2.0) / sps;
            rrc_coeffs[k] = rrc_value(t, rolloff);
        }
        // Normalize
        let sum: f32 = rrc_coeffs.iter().sum();
        if sum.abs() > 1e-10 {
            for c in &mut rrc_coeffs {
                *c /= sum;
            }
        }

        Self {
            sample_rate,
            baud,
            is_8psk,
            v26_alt,
            carrier_phase: 0.0,
            carrier_freq: carrier_rad,
            carrier_nominal: carrier_rad,
            i_filter: vec![0.0; filter_len],
            q_filter: vec![0.0; filter_len],
            rrc_coeffs,
            filter_len,
            filter_idx: 0,
            samples_per_symbol: sps,
            timing_acc: 0.0,
            prev_i: 0.0,
            prev_q: 0.0,
            mid_i: 0.0,
            mid_q: 0.0,
            prev_phase_idx: 0,
            hdlc: HdlcDecoder::new(chan, subchan, 0, false),
            decoded_frames: Vec::new(),
            chan,
            subchan,
            bit_acc: 0,
            bits_in_acc: 0,
            symbol_count: 0,
            good_symbols: 0,
            data_detect: false,
            pll_nudge_total: 0,
            pll_symbol_count: 0,
        }
    }

    /// Process one audio sample.
    pub fn process_sample(&mut self, sam: i32) {
        let fsam = sam as f32 / 16384.0;

        // Mix down to baseband
        let i_bb = fsam * self.carrier_phase.cos();
        let q_bb = fsam * (-self.carrier_phase.sin());

        // Advance carrier NCO
        self.carrier_phase += self.carrier_freq;
        if self.carrier_phase > 2.0 * PI {
            self.carrier_phase -= 2.0 * PI;
        }

        // Push into matched filter
        let idx = self.filter_idx % self.filter_len;
        self.i_filter[idx] = i_bb;
        self.q_filter[idx] = q_bb;
        self.filter_idx += 1;

        // Apply RRC filter
        let mut i_filt = 0.0f32;
        let mut q_filt = 0.0f32;
        for k in 0..self.filter_len {
            let buf_idx = (self.filter_idx + k) % self.filter_len;
            i_filt += self.i_filter[buf_idx] * self.rrc_coeffs[k];
            q_filt += self.q_filter[buf_idx] * self.rrc_coeffs[k];
        }

        // Gardner timing recovery
        self.timing_acc += 1.0;

        // Midpoint sample
        if (self.timing_acc - self.samples_per_symbol / 2.0).abs() < 0.5 {
            self.mid_i = i_filt;
            self.mid_q = q_filt;
        }

        // Symbol sample
        if self.timing_acc >= self.samples_per_symbol {
            self.timing_acc -= self.samples_per_symbol;

            // Gardner TED
            let ted_i = self.mid_i * (self.prev_i - i_filt);
            let ted_q = self.mid_q * (self.prev_q - q_filt);
            let ted = ted_i + ted_q;

            // Adjust timing
            let timing_gain = 0.01;
            self.timing_acc += ted * timing_gain;

            // Costas loop carrier recovery
            let phase_err = if self.is_8psk {
                // 8-PSK: use decision-directed
                let phase = q_filt.atan2(i_filt);
                let sector = ((phase / (2.0 * PI) * 8.0).round() as i32).rem_euclid(8) as usize;
                let ideal_phase = sector as f32 * 2.0 * PI / 8.0;
                let mut err = phase - ideal_phase;
                if err > PI { err -= 2.0 * PI; }
                if err < -PI { err += 2.0 * PI; }
                err
            } else {
                // QPSK Costas loop
                let sgn_i = if i_filt >= 0.0 { 1.0f32 } else { -1.0 };
                let sgn_q = if q_filt >= 0.0 { 1.0f32 } else { -1.0 };
                sgn_i * q_filt - sgn_q * i_filt
            };

            let carrier_gain = 0.002;
            self.carrier_freq = self.carrier_nominal + phase_err * carrier_gain;

            // Decode symbol
            self.decode_symbol(i_filt, q_filt);

            self.prev_i = i_filt;
            self.prev_q = q_filt;
        }
    }

    fn decode_symbol(&mut self, i: f32, q: f32) {
        let phase = q.atan2(i);
        let (bits_per_sym, phase_idx) = if self.is_8psk {
            let idx = ((phase / (2.0 * PI) * 8.0).round() as i32).rem_euclid(8) as usize;
            (3usize, idx)
        } else {
            let idx = ((phase / (2.0 * PI) * 4.0).round() as i32).rem_euclid(4) as usize;
            (2usize, idx)
        };

        // Differential decode: compare to previous symbol
        let diff = (phase_idx + if self.is_8psk { 8 } else { 4 } - self.prev_phase_idx)
            % if self.is_8psk { 8 } else { 4 };

        let bits = if self.is_8psk {
            PSK8_GRAY[diff]
        } else if self.v26_alt == V26Alternative::A {
            QPSK_GRAY_A[diff]
        } else {
            QPSK_GRAY_B[diff]
        };

        self.prev_phase_idx = phase_idx;

        // DCD: track symbol amplitude
        let amp = (i * i + q * q).sqrt();
        self.symbol_count += 1;
        if amp > 0.1 {
            self.good_symbols += 1;
        }
        if self.symbol_count >= 32 {
            self.data_detect = self.good_symbols > 16;
            self.symbol_count = 0;
            self.good_symbols = 0;
        }

        // Feed bits to HDLC LSB first
        for bit_pos in 0..bits_per_sym {
            let bit = (bits >> bit_pos) & 1;
            if let Some(frame) = self.hdlc.process_bit(
                bit != 0,
                &mut self.pll_nudge_total,
                &mut self.pll_symbol_count,
            ) {
                self.decoded_frames.push(frame);
            }
        }
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

/// Root Raised Cosine pulse shape.
fn rrc_value(t: f32, alpha: f32) -> f32 {
    if t.abs() < 1e-6 {
        return 1.0 + alpha * (4.0 / PI - 1.0);
    }
    let at = alpha * t;
    if (at.abs() - 0.5).abs() < 1e-6 {
        return alpha / (2.0f32).sqrt()
            * ((1.0 + 2.0 / PI) * (PI / (4.0 * alpha)).sin()
                + (1.0 - 2.0 / PI) * (PI / (4.0 * alpha)).cos());
    }
    let num = (PI * t * (1.0 - alpha)).sin() + 4.0 * at * (PI * t * (1.0 + alpha)).cos();
    let den = PI * t * (1.0 - (4.0 * at).powi(2));
    if den.abs() < 1e-10 {
        return 0.0;
    }
    num / den
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn psk_demod_creates() {
        let _demod = PskDemodulator::new(48000, 2400, 1800, V26Alternative::B, 0, 0);
    }

    #[test]
    fn psk8_demod_creates() {
        let _demod = PskDemodulator::new(48000, 4800, 1800, V26Alternative::Unspecified, 0, 0);
    }

    #[test]
    fn psk_processes_silence() {
        let mut demod = PskDemodulator::new(48000, 2400, 1800, V26Alternative::B, 0, 0);
        for _ in 0..48000 {
            demod.process_sample(0);
        }
        assert!(demod.take_frames().is_empty());
        assert!(!demod.data_detect());
    }

    #[test]
    fn rrc_value_at_zero() {
        let v = rrc_value(0.0, 0.35);
        // RRC(0, α) = 1 + α*(4/π - 1)
        let expected = 1.0 + 0.35 * (4.0 / std::f32::consts::PI - 1.0);
        assert!((v - expected).abs() < 0.01, "got {}, expected {}", v, expected);
    }
}
