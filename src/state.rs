//! Demodulator state structures.
//!
//! These mirror the C `demodulator_state_s` struct from `fsk_demod_state.h`.
//! All fields are preserved — even those not used by AFSK — so this can serve
//! as the foundation for porting other modem types (baseband, PSK, etc.).

use crate::filter_buf::FilterBuf;
use crate::types::*;

/// Per-slicer PLL and DCD state.
///
/// One instance per slicer within a demodulator subchannel.
/// Corresponds to the anonymous struct in `demodulator_state_s.slicer[]`.
#[derive(Clone, Debug, Default)]
pub struct SlicerState {
    /// PLL phase accumulator. Bit is sampled on signed overflow (positive → negative).
    pub data_clock_pll: i32,

    /// Previous `data_clock_pll`, used to detect the sampling overflow.
    pub prev_d_c_pll: i32,

    /// Count of symbols since frame start, for baud rate error measurement.
    pub pll_symbol_count: i32,

    /// Accumulated PLL nudge amount over current frame, for speed error.
    pub pll_nudge_total: i64,

    /// Previous demodulated data bit (0 or 1), for transition detection.
    pub prev_demod_data: i32,

    /// Previous demodulator output as float, retained for signal analysis.
    pub prev_demod_out_f: f32,

    /// Descrambler LFSR state (9600 baud G3RUH).
    pub lfsr: i32,

    /// Transition near expected PLL phase this symbol.
    pub good_flag: bool,

    /// Transition far from expected PLL phase this symbol.
    pub bad_flag: bool,

    /// Rolling 8-bit history of good transitions.
    pub good_hist: u8,

    /// Rolling 8-bit history of bad transitions.
    pub bad_hist: u8,

    /// Rolling 32-bit score: good-minus-bad history.
    pub score: u32,

    /// True when DPLL is locked to incoming signal.
    pub data_detect: bool,
}

/// AFSK-specific modem state.
///
/// Contains oscillator phases, I/Q delay lines, and FM discriminator state
/// for the version 1.7+ AFSK demodulator.
///
/// Delay lines use [`FilterBuf`] — a circular buffer with O(1) push — instead
/// of raw arrays with O(n) shifting.
#[derive(Clone, Debug)]
pub struct AfskState {
    // Mark local oscillator
    pub m_osc_phase: u32,
    pub m_osc_delta: u32,

    // Space local oscillator
    pub s_osc_phase: u32,
    pub s_osc_delta: u32,

    // Center local oscillator (Profile B)
    pub c_osc_phase: u32,
    pub c_osc_delta: u32,

    // I/Q delay lines — FilterBuf for O(1) push + contiguous slice
    pub m_i_buf: FilterBuf,
    pub m_q_buf: FilterBuf,
    pub s_i_buf: FilterBuf,
    pub s_q_buf: FilterBuf,
    pub c_i_buf: FilterBuf,
    pub c_q_buf: FilterBuf,

    /// Use Root Raised Cosine rather than generic low-pass.
    pub use_rrc: bool,

    /// RRC filter width in symbol times.
    pub rrc_width_sym: f32,

    /// RRC roll-off factor, 0..1.
    pub rrc_rolloff: f32,

    /// Previous instantaneous phase for FM discriminator (Profile B).
    pub prev_phase: f32,

    /// Normalization factor for FM discriminator output.
    pub normalize_rpsam: f32,
}

impl Default for AfskState {
    fn default() -> Self {
        Self {
            m_osc_phase: 0,
            m_osc_delta: 0,
            s_osc_phase: 0,
            s_osc_delta: 0,
            c_osc_phase: 0,
            c_osc_delta: 0,
            m_i_buf: FilterBuf::new(),
            m_q_buf: FilterBuf::new(),
            s_i_buf: FilterBuf::new(),
            s_q_buf: FilterBuf::new(),
            c_i_buf: FilterBuf::new(),
            c_q_buf: FilterBuf::new(),
            use_rrc: false,
            rrc_width_sym: 0.0,
            rrc_rolloff: 0.0,
            prev_phase: 0.0,
            normalize_rpsam: 0.0,
        }
    }
}

/// Full demodulator state.
///
/// Maps to `struct demodulator_state_s` in the C code. All fields from the
/// original struct are present so this can serve as the foundation for porting
/// other modem types in the future.
#[derive(Clone, Debug)]
pub struct DemodulatorState {
    // --- Set once during initialization ---

    pub modem_type: ModemType,
    pub profile: AfskProfile,

    /// DPLL step per audio sample. Accumulator overflows at the bit boundary.
    pub pll_step_per_sample: i32,

    // --- Low-pass filter ---

    pub lp_window: WindowType,
    pub lpf_use_fir: bool,
    pub lpf_iir: f32,
    pub lpf_baud: f32,
    pub lp_filter_width_sym: f32,
    pub lp_filter_taps: usize,
    /// LPF kernel coefficients (not a delay line — no FilterBuf).
    pub lp_filter: [f32; MAX_FILTER_SIZE],

    // --- AGC ---

    pub agc_fast_attack: f32,
    pub agc_slow_decay: f32,

    // --- Signal level display ---

    pub quick_attack: f32,
    pub sluggish_decay: f32,

    pub hysteresis: f32,
    pub num_slicers: usize,

    // --- PLL inertia ---

    pub pll_locked_inertia: f32,
    pub pll_searching_inertia: f32,

    // --- Bandpass pre-filter ---

    pub use_prefilter: bool,
    pub prefilter_baud: f32,
    pub pre_filter_len_sym: f32,
    pub pre_window: WindowType,
    pub pre_filter_taps: usize,
    /// Prefilter kernel coefficients (not a delay line).
    pub pre_filter: [f32; MAX_FILTER_SIZE],
    /// Prefilter input delay line.
    pub pre_filter_buf: FilterBuf,

    // --- PSK ---

    pub lo_phase: u32,

    // --- Audio level tracking ---

    pub alevel_rec_peak: f32,
    pub alevel_rec_valley: f32,
    pub alevel_mark_peak: f32,
    pub alevel_space_peak: f32,

    // --- AGC peak/valley ---

    pub m_peak: f32,
    pub s_peak: f32,
    pub m_valley: f32,
    pub s_valley: f32,

    /// Previous mark/space amplitudes for derivative analysis.
    pub m_amp_prev: f32,
    pub s_amp_prev: f32,

    // --- Per-slicer ---

    pub slicer: [SlicerState; MAX_SLICERS],

    // --- AFSK-specific ---

    pub afsk: AfskState,
}

impl Default for DemodulatorState {
    fn default() -> Self {
        Self {
            modem_type: ModemType::Afsk,
            profile: AfskProfile::A,
            pll_step_per_sample: 0,
            lp_window: WindowType::Truncated,
            lpf_use_fir: false,
            lpf_iir: 0.0,
            lpf_baud: 0.0,
            lp_filter_width_sym: 0.0,
            lp_filter_taps: 0,
            lp_filter: [0.0; MAX_FILTER_SIZE],
            agc_fast_attack: 0.0,
            agc_slow_decay: 0.0,
            quick_attack: 0.0,
            sluggish_decay: 0.0,
            hysteresis: 0.0,
            num_slicers: 1,
            pll_locked_inertia: 0.0,
            pll_searching_inertia: 0.0,
            use_prefilter: false,
            prefilter_baud: 0.0,
            pre_filter_len_sym: 0.0,
            pre_window: WindowType::Truncated,
            pre_filter_taps: 0,
            pre_filter: [0.0; MAX_FILTER_SIZE],
            pre_filter_buf: FilterBuf::new(),
            lo_phase: 0,
            alevel_rec_peak: 0.0,
            alevel_rec_valley: 0.0,
            alevel_mark_peak: 0.0,
            alevel_space_peak: 0.0,
            m_peak: 0.0,
            s_peak: 0.0,
            m_valley: 0.0,
            s_valley: 0.0,
            m_amp_prev: 0.0,
            s_amp_prev: 0.0,
            slicer: std::array::from_fn(|_| SlicerState::default()),
            afsk: AfskState::default(),
        }
    }
}
