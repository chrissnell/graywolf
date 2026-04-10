//! DSP filter generation functions.
//!
//! Provides windowed-sinc low-pass and band-pass filter kernels, Root Raised
//! Cosine (RRC) filters, and mark/space correlation tables. These are called
//! once during demodulator initialization to populate the filter coefficient
//! arrays used in real-time convolution.

use std::f32::consts::PI;

use crate::types::{WindowType, MAX_FILTER_SIZE};

/// Filter window shape function.
/// Returns the window multiplier for tap index `j` in a filter of `size` taps.
pub fn window(wtype: WindowType, size: usize, j: usize) -> f32 {
    let center = 0.5 * (size as f32 - 1.0);
    let j = j as f32;
    let size_f = size as f32;

    match wtype {
        WindowType::Cosine => ((j - center) / size_f * PI).cos(),
        WindowType::Hamming => {
            0.53836 - 0.46164 * (j * 2.0 * PI / (size_f - 1.0)).cos()
        }
        WindowType::Blackman => {
            0.42659 - 0.49656 * (j * 2.0 * PI / (size_f - 1.0)).cos()
                + 0.076849 * (j * 4.0 * PI / (size_f - 1.0)).cos()
        }
        WindowType::FlatTop => {
            1.0 - 1.93 * (j * 2.0 * PI / (size_f - 1.0)).cos()
                + 1.29 * (j * 4.0 * PI / (size_f - 1.0)).cos()
                - 0.388 * (j * 6.0 * PI / (size_f - 1.0)).cos()
                + 0.028 * (j * 8.0 * PI / (size_f - 1.0)).cos()
        }
        WindowType::Truncated => 1.0,
    }
}

/// Generate a windowed sinc lowpass filter kernel.
/// `fc` is the cutoff frequency as a fraction of the sampling frequency.
/// The filter is normalized for unity gain at DC.
pub fn gen_lowpass(fc: f32, lp_filter: &mut [f32], wtype: WindowType) {
    let filter_size = lp_filter.len();
    assert!(filter_size >= 3 && filter_size <= MAX_FILTER_SIZE);

    let center = 0.5 * (filter_size as f32 - 1.0);

    for j in 0..filter_size {
        let jf = j as f32;
        let sinc = if (jf - center).abs() < 1e-6 {
            2.0 * fc
        } else {
            (2.0 * PI * fc * (jf - center)).sin() / (PI * (jf - center))
        };
        let shape = window(wtype, filter_size, j);
        lp_filter[j] = sinc * shape;
    }

    let g: f32 = lp_filter[..filter_size].iter().sum();
    if g.abs() > 1e-10 {
        for j in 0..filter_size {
            lp_filter[j] /= g;
        }
    }
}

/// Generate a windowed sinc bandpass filter kernel.
/// `f1` and `f2` are cutoff frequencies as fractions of the sampling frequency.
/// The filter is normalized for unity gain at the passband center.
pub fn gen_bandpass(f1: f32, f2: f32, bp_filter: &mut [f32], wtype: WindowType) {
    let filter_size = bp_filter.len();
    assert!(filter_size >= 3 && filter_size <= MAX_FILTER_SIZE);

    let center = 0.5 * (filter_size as f32 - 1.0);

    for j in 0..filter_size {
        let jf = j as f32;
        let sinc = if (jf - center).abs() < 1e-6 {
            2.0 * (f2 - f1)
        } else {
            (2.0 * PI * f2 * (jf - center)).sin() / (PI * (jf - center))
                - (2.0 * PI * f1 * (jf - center)).sin() / (PI * (jf - center))
        };
        let shape = window(wtype, filter_size, j);
        bp_filter[j] = sinc * shape;
    }

    let w = 2.0 * PI * (f1 + f2) / 2.0;
    let mut g: f32 = 0.0;
    for j in 0..filter_size {
        g += 2.0 * bp_filter[j] * ((j as f32 - center) * w).cos();
    }

    if g.abs() > 1e-10 {
        for j in 0..filter_size {
            bp_filter[j] /= g;
        }
    }
}

/// Generate mark/space filter tables (port of gen_ms in dsp.c).
///
/// Produces windowed sin and cos correlation tables for a specific tone frequency,
/// normalized for unity gain. Used by older demodulator profiles that correlate
/// directly with mark/space filters (pre-1.7 approach).
///
/// - `fc` — Tone frequency in Hz (mark or space).
/// - `sps` — Audio samples per second.
/// - `sin_table` / `cos_table` — Output filter tables (length = `filter_size`).
/// - `filter_size` — Number of filter taps.
/// - `wtype` — Window function type for filter shaping.
pub fn gen_ms(
    fc: i32,
    sps: i32,
    sin_table: &mut [f32],
    cos_table: &mut [f32],
    filter_size: usize,
    wtype: WindowType,
) {
    assert_eq!(sin_table.len(), filter_size);
    assert_eq!(cos_table.len(), filter_size);

    let mut gs: f32 = 0.0;
    let mut gc: f32 = 0.0;

    for j in 0..filter_size {
        let center = 0.5 * (filter_size as f32 - 1.0);
        let am = ((j as f32 - center) / sps as f32) * fc as f32 * 2.0 * PI;

        let shape = window(wtype, filter_size, j);

        sin_table[j] = am.sin() * shape;
        cos_table[j] = am.cos() * shape;

        gs += sin_table[j] * am.sin();
        gc += cos_table[j] * am.cos();
    }

    if gs.abs() > 1e-10 {
        for j in 0..filter_size {
            sin_table[j] /= gs;
        }
    }
    if gc.abs() > 1e-10 {
        for j in 0..filter_size {
            cos_table[j] /= gc;
        }
    }
}

/// Root Raised Cosine function.
/// `t` is time in units of symbol duration.
/// `a` is the roll-off factor, between 0 and 1.
pub fn rrc(t: f32, a: f32) -> f32 {
    let sinc = if t > -0.001 && t < 0.001 {
        1.0
    } else {
        (PI * t).sin() / (PI * t)
    };

    let win = if (a * t).abs() > 0.499 && (a * t).abs() < 0.501 {
        PI / 4.0
    } else {
        let denom = 1.0 - (2.0 * a * t).powi(2);
        (PI * a * t).cos() / denom
    };

    sinc * win
}

/// Generate a Root Raised Cosine lowpass filter, normalized for unity gain.
pub fn gen_rrc_lowpass(pfilter: &mut [f32], rolloff: f32, samples_per_symbol: f32) {
    let filter_taps = pfilter.len();

    for k in 0..filter_taps {
        let t = (k as f32 - (filter_taps as f32 - 1.0) / 2.0) / samples_per_symbol;
        pfilter[k] = rrc(t, rolloff);
    }

    let sum: f32 = pfilter[..filter_taps].iter().sum();
    if sum.abs() > 1e-10 {
        for k in 0..filter_taps {
            pfilter[k] /= sum;
        }
    }
}
