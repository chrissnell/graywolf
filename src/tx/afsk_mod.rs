//! Bell 202 AFSK modulator.
//!
//! Turns a stream of NRZI line-state bits into mono `i16` audio via a
//! phase-continuous fixed-point numerically-controlled oscillator driving a
//! 256-entry sine lookup table. Same shape as direwolf's `gen_tone.c`: no
//! low-pass filter, no pulse shaping — the phase continuity alone keeps the
//! spectrum clean enough to ship.
//!
//! Mark tone (1200 Hz) carries line-state `1`, space (2200 Hz) carries `0`.
//! Baud rate is fixed at 1200 since this modulator only targets Bell 202.

use super::TxError;

const BAUD: u32 = 1200;
const MARK_FREQ: u32 = 1200;
const SPACE_FREQ: u32 = 2200;

const SINE_TABLE_LEN: usize = 256;

/// Target peak amplitude — roughly `0.5 * i16::MAX`, leaving ~6 dB of
/// headroom for the downstream soundcard gain stage.
const TARGET_AMPLITUDE: i16 = 16_384;

/// Modulate a stream of NRZI line-state bits into mono i16 audio at
/// `sample_rate` Hz.
///
/// Each element of `bits` is a single bit (`0` or `1`). A `1` is emitted as
/// the mark tone (1200 Hz), a `0` as the space tone (2200 Hz). The oscillator
/// phase is carried across bit boundaries so there are no discontinuities at
/// symbol transitions.
///
/// The output length is `bits.len() * sample_rate / baud`, give or take one
/// sample per bit of fractional rounding — it is exact when `sample_rate`
/// is an integer multiple of 1200 (as it is at 24000, 48000, and 96000).
///
/// Returns [`TxError::InvalidSampleRate`] if `sample_rate` is zero.
pub fn modulate(bits: &[u8], sample_rate: u32) -> Result<Vec<i16>, TxError> {
    if sample_rate == 0 {
        return Err(TxError::InvalidSampleRate);
    }

    let table = build_sine_table();

    // Phase increment per sample for each tone. TICKS_PER_CYCLE = 2^32 so
    // the top byte of the accumulator is a direct 256-entry table index.
    let mark_delta = (((MARK_FREQ as u64) << 32) / sample_rate as u64) as u32;
    let space_delta = (((SPACE_FREQ as u64) << 32) / sample_rate as u64) as u32;

    // Fractional-samples-per-bit accumulator. `samples_per_bit_q32` is
    // (sample_rate << 32) / baud, so adding it once per bit spills the
    // integer part into the top 32 bits and keeps the fractional part
    // below. At 48000/1200 this is exactly `40 << 32`, so each bit emits
    // exactly 40 samples; at 44100/1200 it averages 36.75 samples per bit
    // by alternating 36 and 37 across the stream.
    let samples_per_bit_q32: u64 = ((sample_rate as u64) << 32) / BAUD as u64;

    let mut phase: u32 = 0;
    let mut frac: u64 = 0;
    let samples_per_bit_approx = (sample_rate as usize).div_ceil(BAUD as usize) + 1;
    let mut out = Vec::with_capacity(bits.len() * samples_per_bit_approx);

    for &bit in bits {
        let delta = if bit != 0 { mark_delta } else { space_delta };
        // frac is bounded above by (previous frac mod 2^32) + samples_per_bit_q32,
        // which for any practical sample rate (< 2^20 Hz) stays well under
        // 2^53 — no wrap concern.
        frac += samples_per_bit_q32;
        let n_samples = (frac >> 32) as u32;
        frac &= 0xffff_ffff;
        for _ in 0..n_samples {
            let idx = (phase >> 24) as usize;
            out.push(table[idx]);
            phase = phase.wrapping_add(delta);
        }
    }
    Ok(out)
}

fn build_sine_table() -> [i16; SINE_TABLE_LEN] {
    let mut table = [0i16; SINE_TABLE_LEN];
    for (i, slot) in table.iter_mut().enumerate() {
        let angle = (i as f64) * std::f64::consts::TAU / SINE_TABLE_LEN as f64;
        *slot = (angle.sin() * TARGET_AMPLITUDE as f64).round() as i16;
    }
    table
}

#[cfg(test)]
mod tests {
    use super::*;

    /// Goertzel algorithm: single-bin magnitude at `target_freq`, returning
    /// the squared magnitude so we avoid a sqrt on the hot path. Used only
    /// in tests to assert where the modulator puts its energy.
    fn goertzel_power(samples: &[i16], target_freq: f32, sample_rate: f32) -> f32 {
        let n = samples.len() as f32;
        let k = (0.5 + n * target_freq / sample_rate).floor();
        let omega = 2.0 * std::f32::consts::PI * k / n;
        let coeff = 2.0 * omega.cos();
        let mut s_prev = 0.0f32;
        let mut s_prev2 = 0.0f32;
        for &s in samples {
            let s_curr = s as f32 + coeff * s_prev - s_prev2;
            s_prev2 = s_prev;
            s_prev = s_curr;
        }
        s_prev * s_prev + s_prev2 * s_prev2 - coeff * s_prev * s_prev2
    }

    #[test]
    fn one_hundred_zero_bits_at_48k_yields_exactly_four_thousand_samples() {
        let samples = modulate(&[0; 100], 48_000).unwrap();
        assert_eq!(samples.len(), 4000);
    }

    #[test]
    fn fractional_samples_per_bit_at_44100_averages_36_75() {
        // 44100/1200 = 36.75 — the Q32 accumulator must average exactly
        // that by alternating 36 and 37 samples per bit. Over 100 bits the
        // total is exactly 3675 samples. This is the *only* test that
        // exercises the fractional path; at 48 kHz the accumulator is
        // degenerate (exactly 40 samples per bit) and wouldn't catch a
        // rounding bug.
        let samples = modulate(&[0; 100], 44_100).unwrap();
        assert_eq!(samples.len(), 3675);

        // Sanity check that the tone still lands on 2200 Hz after going
        // through the fractional accumulator. 12 bits at 44100/1200 is
        // exactly 441 samples, and 441 places both 1200 and 2200 on
        // integer Goertzel bins (12 and 22 respectively), so the ratio
        // isn't muddied by scalloping.
        let short = modulate(&[0; 12], 44_100).unwrap();
        assert_eq!(short.len(), 441);
        let p_space = goertzel_power(&short, 2200.0, 44_100.0);
        let p_mark = goertzel_power(&short, 1200.0, 44_100.0);
        assert!(
            p_space > p_mark * 10_000.0,
            "space/mark ratio {} at 44100 Hz — phase increment may be wrong",
            p_space / p_mark.max(1.0)
        );
    }

    #[test]
    fn zero_sample_rate_returns_invalid_sample_rate_error() {
        assert_eq!(
            modulate(&[0; 8], 0).unwrap_err(),
            TxError::InvalidSampleRate
        );
    }

    #[test]
    fn pure_space_tone_has_no_dc_bias() {
        let samples = modulate(&[0; 100], 48_000).unwrap();
        let sum: i64 = samples.iter().map(|&s| s as i64).sum();
        let bound = (samples.len() as i64) * (TARGET_AMPLITUDE as i64) / 100;
        assert!(
            sum.abs() < bound,
            "DC offset {} exceeded tolerance {}",
            sum,
            bound
        );
    }

    #[test]
    fn peak_amplitude_is_within_five_percent_of_target() {
        let samples = modulate(&[0, 1, 0, 1, 0, 1, 0, 1, 0, 1, 0, 1], 48_000).unwrap();
        let peak = samples
            .iter()
            .map(|&s| s.unsigned_abs() as i32)
            .max()
            .unwrap();
        let target = TARGET_AMPLITUDE as i32;
        let tol = target / 20; // 5%
        assert!(
            (peak - target).abs() <= tol,
            "peak {} outside 5% of target {}",
            peak,
            target
        );
    }

    #[test]
    fn pure_space_tone_concentrates_energy_at_2200_hz() {
        // 10_000:1 power ratio = -40 dBc, matching the phase-A spec. The
        // phase-continuous NCO plus 256-entry sine table comfortably
        // clears this bar; if a future refactor drops shaping or
        // quantises the phase accumulator it'll show up here first.
        let samples = modulate(&[0; 240], 48_000).unwrap();
        let p_space = goertzel_power(&samples, 2200.0, 48_000.0);
        let p_mark = goertzel_power(&samples, 1200.0, 48_000.0);
        let p_off1 = goertzel_power(&samples, 800.0, 48_000.0);
        let p_off2 = goertzel_power(&samples, 3200.0, 48_000.0);
        assert!(
            p_space > p_mark * 10_000.0,
            "space/mark ratio too low: {} vs {}",
            p_space,
            p_mark
        );
        assert!(p_space > p_off1 * 10_000.0);
        assert!(p_space > p_off2 * 10_000.0);
    }

    #[test]
    fn pure_mark_tone_concentrates_energy_at_1200_hz() {
        let samples = modulate(&[1; 240], 48_000).unwrap();
        let p_mark = goertzel_power(&samples, 1200.0, 48_000.0);
        let p_space = goertzel_power(&samples, 2200.0, 48_000.0);
        let p_off1 = goertzel_power(&samples, 400.0, 48_000.0);
        let p_off2 = goertzel_power(&samples, 2000.0, 48_000.0);
        assert!(
            p_mark > p_space * 10_000.0,
            "mark/space ratio too low: {} vs {}",
            p_mark,
            p_space
        );
        assert!(p_mark > p_off1 * 10_000.0);
        assert!(p_mark > p_off2 * 10_000.0);
    }

    #[test]
    fn phase_is_continuous_across_bit_transitions() {
        // Adjacent samples at a Bell 202 transition should differ by at
        // most the maximum per-sample delta of the higher tone, plus a
        // little slack for the sine table's finite resolution. Sine table
        // entries are TARGET_AMPLITUDE scaled, so the step bound is
        // TARGET_AMPLITUDE * sin(2π * 2200/48000) ≈ 4625. Allow 2x for
        // table quantisation.
        let samples = modulate(&[1, 0, 1, 0, 1, 0, 1, 0], 48_000).unwrap();
        let max_allowed = (TARGET_AMPLITUDE as f64
            * (2.0 * std::f64::consts::PI * 2200.0 / 48_000.0).sin()
            * 2.0) as i32;
        for window in samples.windows(2) {
            let d = (window[1] as i32 - window[0] as i32).abs();
            assert!(
                d <= max_allowed,
                "sample jump {} exceeded phase-continuity bound {}",
                d,
                max_allowed
            );
        }
    }
}
