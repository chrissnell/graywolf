//! AFSK demodulator — the core signal processing engine.
//!
//! Implements two demodulation profiles:
//!
//! - **Profile A**: Mixes the input with separate mark and space local
//!   oscillators, low-pass filters the I/Q products, computes amplitudes
//!   via `hypot`, and applies AGC before comparing. Single-slicer uses a
//!   decision-feedback AGC (mark/space references tracked independently);
//!   multi-slicer uses the classic peak/valley envelope tracker.
//!
//! - **Profile B**: Mixes with a single center-frequency oscillator, low-pass
//!   filters, and measures the instantaneous frequency via phase-rate (FM
//!   discriminator).
//!
//! Both profiles feed their demodulated output into a digital PLL for clock
//! recovery, with DCD (Data Carrier Detect) scoring and HDLC frame extraction.

use std::f32::consts::PI;

use crate::dsp;
use crate::hdlc::{DecodedFrame, HdlcDecoder};
use crate::state::DemodulatorState;
use crate::types::*;

// Multi-slicer gain range: space amplitude is scaled by factors
// logarithmically spaced from MIN_G to MAX_G.
const MIN_G: f32 = 0.5;
const MAX_G: f32 = 4.0;

// Decision-feedback AGC: exponential-average rates for the mark and space
// reference levels. Asymmetric (mark tracks faster) because mark tones are
// more frequent on typical packet traffic and benefit from tighter tracking.
//
// Technique credit: Ion Todirel (W7ION), described in posts on the APRS
// Users Facebook group. Coefficients match his published design.
const DFB_ALPHA_MARK: f32 = 0.008;
const DFB_ALPHA_SPACE: f32 = 0.005;

// ---------------------------------------------------------------------------
// Cosine lookup table — 256 entries indexed by the top 8 bits of a u32 phase
// accumulator, giving ~1.4° resolution. Shared across all demodulators.
// ---------------------------------------------------------------------------

fn build_fcos256_table() -> [f32; 256] {
    let mut table = [0.0f32; 256];
    for j in 0..256 {
        table[j] = (j as f32 * 2.0 * PI / 256.0).cos();
    }
    table
}

#[inline(always)]
fn fcos256(table: &[f32; 256], phase: u32) -> f32 {
    table[((phase >> 24) & 0xff) as usize]
}

#[inline(always)]
fn fsin256(table: &[f32; 256], phase: u32) -> f32 {
    table[(((phase >> 24) as u8).wrapping_sub(64)) as usize]
}

// ---------------------------------------------------------------------------
// FIR convolution — operates on slices so it works with both FilterBuf
// output and raw kernel arrays.
// ---------------------------------------------------------------------------

/// Dot-product of two equal-length slices. Used for FIR filtering.
///
/// Uses eight independent accumulators (two NEON vector widths) so the CPU
/// can fully pipeline FMA operations. Apple Silicon has 4-cycle FMA latency
/// with 2 NEON pipes, so 8 independent chains keep both pipes saturated.
#[inline(always)]
fn convolve(data: &[f32], filter: &[f32]) -> f32 {
    debug_assert_eq!(data.len(), filter.len());
    let len = data.len();
    let chunks = len / 8;
    let mut a0 = 0.0f32;
    let mut a1 = 0.0f32;
    let mut a2 = 0.0f32;
    let mut a3 = 0.0f32;
    let mut a4 = 0.0f32;
    let mut a5 = 0.0f32;
    let mut a6 = 0.0f32;
    let mut a7 = 0.0f32;

    for i in 0..chunks {
        let b = i * 8;
        // SAFETY: b+7 < chunks*8 <= len, and both slices have length >= len.
        unsafe {
            a0 += *data.get_unchecked(b) * *filter.get_unchecked(b);
            a1 += *data.get_unchecked(b + 1) * *filter.get_unchecked(b + 1);
            a2 += *data.get_unchecked(b + 2) * *filter.get_unchecked(b + 2);
            a3 += *data.get_unchecked(b + 3) * *filter.get_unchecked(b + 3);
            a4 += *data.get_unchecked(b + 4) * *filter.get_unchecked(b + 4);
            a5 += *data.get_unchecked(b + 5) * *filter.get_unchecked(b + 5);
            a6 += *data.get_unchecked(b + 6) * *filter.get_unchecked(b + 6);
            a7 += *data.get_unchecked(b + 7) * *filter.get_unchecked(b + 7);
        }
    }
    for i in (chunks * 8)..len {
        // SAFETY: i < len.
        unsafe {
            a0 += *data.get_unchecked(i) * *filter.get_unchecked(i);
        }
    }
    (a0 + a4) + (a1 + a5) + (a2 + a6) + (a3 + a7)
}

// ---------------------------------------------------------------------------
// Automatic Gain Control — IIR envelope tracker.
// ---------------------------------------------------------------------------

/// IIR envelope tracker with fast attack / slow decay.
///
/// Tracks the peak and valley of the input signal, then normalizes the input
/// to approximately ±0.5. The result is clipped to the envelope to prevent
/// spikes from corrupting the demodulated output.
#[inline(always)]
fn agc(
    input: f32,
    fast_attack: f32,
    slow_decay: f32,
    peak: &mut f32,
    valley: &mut f32,
) -> f32 {
    if input >= *peak {
        *peak = input * fast_attack + *peak * (1.0 - fast_attack);
    } else {
        *peak = input * slow_decay + *peak * (1.0 - slow_decay);
    }

    if input <= *valley {
        *valley = input * fast_attack + *valley * (1.0 - fast_attack);
    } else {
        *valley = input * slow_decay + *valley * (1.0 - slow_decay);
    }

    let clamped = input.clamp(*valley, *peak);

    if *peak > *valley {
        (clamped - 0.5 * (*peak + *valley)) / (*peak - *valley)
    } else {
        0.0
    }
}

// ---------------------------------------------------------------------------
// DCD state change event
// ---------------------------------------------------------------------------

/// Notification that the Data Carrier Detect state changed for a slicer.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub struct DcdChange {
    pub chan: usize,
    pub subchan: usize,
    pub slice: usize,
    pub data_detect: bool,
}

// ---------------------------------------------------------------------------
// AfskDemodulator — the main public type
// ---------------------------------------------------------------------------

/// AFSK demodulator. Owns the DSP state, HDLC decoders, and output buffers.
///
/// Create with [`new`](Self::new), feed audio with [`process_sample`](Self::process_sample),
/// and collect results with [`take_frames`](Self::take_frames).
pub struct AfskDemodulator {
    pub state: DemodulatorState,
    chan: usize,
    subchan: usize,
    hdlc: Vec<HdlcDecoder>,
    decoded_frames: Vec<DecodedFrame>,
    dcd_changes: Vec<DcdChange>,
    fcos256_table: [f32; 256],
    space_gain: [f32; MAX_SUBCHANS],

    // Decision-feedback AGC state (Profile A single-slicer path).
    // Tracks mark and space amplitude references independently, updating
    // whichever one matched the previous soft decision's sign. Seeded to a
    // tiny non-zero value so the first division never hits zero.
    dfb_mark_ref: f32,
    dfb_space_ref: f32,
    dfb_last_soft: f32,

    /// Hard-limit the audio sample to sign(x) before the bandpass prefilter.
    /// Discards amplitude information and retains only zero-crossing timing.
    /// Useful on flat (non-emphasized) audio where the BPF cleanly rejects
    /// the square-wave harmonics. May hurt on de-emphasized audio where the
    /// space tone is weaker than the mark and gets captured-out.
    hard_limit: bool,

    /// Monotonic audio-sample counter, written into each emitted frame's
    /// `sample_offset` so downstream code can dedup across demodulators by
    /// matching identical frame content within a short time window.
    sample_counter: u64,
}

impl AfskDemodulator {
    /// Create and initialize a new AFSK demodulator.
    ///
    /// Equivalent to `demod_afsk_init()` in the C code. Configures filters,
    /// oscillators, AGC, and PLL parameters based on the selected profile.
    ///
    /// # Panics
    ///
    /// Panics if `samples_per_sec` or `baud` are zero or if the computed filter
    /// sizes exceed [`MAX_FILTER_SIZE`].
    pub fn new(
        samples_per_sec: u32,
        baud: u32,
        mark_freq: u32,
        space_freq: u32,
        profile: AfskProfile,
        chan: usize,
        subchan: usize,
    ) -> Self {
        let fcos256_table = build_fcos256_table();
        let mut state = DemodulatorState {
            num_slicers: 1,
            modem_type: ModemType::Afsk,
            ..DemodulatorState::default()
        };

        Self::configure_profile(&mut state, profile, samples_per_sec, baud, mark_freq, space_freq);
        Self::configure_pll(&mut state, samples_per_sec, baud);
        Self::build_prefilter(&mut state, samples_per_sec, baud, mark_freq, space_freq);
        Self::build_lowpass(&mut state, samples_per_sec, baud);

        let space_gain = Self::build_space_gain_table();

        let hdlc: Vec<HdlcDecoder> = (0..state.num_slicers)
            .map(|s| HdlcDecoder::new(chan, subchan, s, false))
            .collect();

        AfskDemodulator {
            state,
            chan,
            subchan,
            hdlc,
            decoded_frames: Vec::new(),
            dcd_changes: Vec::new(),
            fcos256_table,
            space_gain,
            dfb_mark_ref: 0.001,
            dfb_space_ref: 0.001,
            dfb_last_soft: 0.0,
            hard_limit: false,
            sample_counter: 0,
        }
    }

    /// Enable or disable audio hard-limiting before the bandpass prefilter.
    /// Default is off. See the `hard_limit` field documentation for tradeoffs.
    pub fn set_hard_limit(&mut self, enable: bool) {
        self.hard_limit = enable;
    }

    // --- Init helpers (called once during construction) ---------------------

    fn configure_profile(
        state: &mut DemodulatorState,
        profile: AfskProfile,
        samples_per_sec: u32,
        baud: u32,
        mark_freq: u32,
        space_freq: u32,
    ) {
        match profile {
            AfskProfile::A => {
                state.profile = AfskProfile::A;
                state.use_prefilter = true;

                if baud > 600 {
                    state.prefilter_baud = 0.155;
                    state.pre_filter_len_sym = 383.0 * 1200.0 / 44100.0;
                    state.pre_window = WindowType::Truncated;
                } else {
                    state.prefilter_baud = 0.87;
                    state.pre_filter_len_sym = 1.857;
                    state.pre_window = WindowType::Cosine;
                }

                state.afsk.m_osc_phase = 0;
                state.afsk.m_osc_delta =
                    (2.0f64.powi(32) * mark_freq as f64 / samples_per_sec as f64).round() as u32;
                state.afsk.s_osc_phase = 0;
                state.afsk.s_osc_delta =
                    (2.0f64.powi(32) * space_freq as f64 / samples_per_sec as f64).round() as u32;

                state.afsk.use_rrc = true;
                if state.afsk.use_rrc {
                    state.afsk.rrc_width_sym = 2.80;
                    state.afsk.rrc_rolloff = 0.20;
                } else {
                    state.lpf_baud = 0.14;
                    state.lp_filter_width_sym = 1.388;
                    state.lp_window = WindowType::Truncated;
                }

                state.agc_fast_attack = 0.70;
                state.agc_slow_decay = 0.000090;
                state.quick_attack = state.agc_fast_attack * 0.2;
                state.sluggish_decay = state.agc_slow_decay * 0.2;

                state.pll_locked_inertia = 0.74;
                state.pll_searching_inertia = 0.50;
            }
            AfskProfile::B => {
                state.profile = AfskProfile::B;
                state.use_prefilter = true;

                if baud > 600 {
                    state.prefilter_baud = 0.19;
                    state.pre_filter_len_sym = 8.163;
                    state.pre_window = WindowType::Truncated;
                } else {
                    state.prefilter_baud = 0.87;
                    state.pre_filter_len_sym = 1.857;
                    state.pre_window = WindowType::Cosine;
                }

                state.afsk.c_osc_phase = 0;
                state.afsk.c_osc_delta = (2.0f64.powi(32)
                    * 0.5
                    * (mark_freq + space_freq) as f64
                    / samples_per_sec as f64)
                    .round() as u32;

                state.afsk.use_rrc = true;
                if state.afsk.use_rrc {
                    state.afsk.rrc_width_sym = 2.00;
                    state.afsk.rrc_rolloff = 0.40;
                } else {
                    state.lpf_baud = 0.5;
                    state.lp_filter_width_sym = 1.714286;
                    state.lp_window = WindowType::Truncated;
                }

                let freq_diff = (mark_freq as i32 - space_freq as i32).unsigned_abs();
                state.afsk.normalize_rpsam =
                    1.0 / (0.5 * freq_diff as f32 * 2.0 * PI / samples_per_sec as f32);

                state.agc_fast_attack = 0.70;
                state.agc_slow_decay = 0.000090;
                state.quick_attack = state.agc_fast_attack * 0.2;
                state.sluggish_decay = state.agc_slow_decay * 0.2;

                state.pll_locked_inertia = 0.74;
                state.pll_searching_inertia = 0.50;

                state.alevel_mark_peak = -1.0;
                state.alevel_space_peak = -1.0;
            }
        }
    }

    fn configure_pll(state: &mut DemodulatorState, samples_per_sec: u32, baud: u32) {
        let effective_baud = if baud == 521 { 520.83f64 } else { baud as f64 };
        state.pll_step_per_sample =
            (TICKS_PER_PLL_CYCLE * effective_baud / samples_per_sec as f64).round() as i32;
    }

    fn build_prefilter(
        state: &mut DemodulatorState,
        samples_per_sec: u32,
        baud: u32,
        mark_freq: u32,
        space_freq: u32,
    ) {
        if !state.use_prefilter {
            return;
        }

        state.pre_filter_taps = ((state.pre_filter_len_sym * samples_per_sec as f32
            / baud as f32) as usize)
            | 1; // odd is better

        if state.pre_filter_taps > MAX_FILTER_SIZE {
            state.pre_filter_taps = (MAX_FILTER_SIZE - 1) | 1;
        }

        let f1 = (mark_freq.min(space_freq) as f32 - state.prefilter_baud * baud as f32)
            / samples_per_sec as f32;
        let f2 = (mark_freq.max(space_freq) as f32 + state.prefilter_baud * baud as f32)
            / samples_per_sec as f32;

        dsp::gen_bandpass(
            f1,
            f2,
            &mut state.pre_filter[..state.pre_filter_taps],
            state.pre_window,
        );
    }

    fn build_lowpass(state: &mut DemodulatorState, samples_per_sec: u32, baud: u32) {
        if state.afsk.use_rrc {
            state.lp_filter_taps = ((state.afsk.rrc_width_sym * samples_per_sec as f32
                / baud as f32) as usize)
                | 1;
            if state.lp_filter_taps > MAX_FILTER_SIZE {
                state.lp_filter_taps = (MAX_FILTER_SIZE - 1) | 1;
            }
            dsp::gen_rrc_lowpass(
                &mut state.lp_filter[..state.lp_filter_taps],
                state.afsk.rrc_rolloff,
                samples_per_sec as f32 / baud as f32,
            );
        } else {
            state.lp_filter_taps = (state.lp_filter_width_sym * samples_per_sec as f32
                / baud as f32)
                .round() as usize;
            if state.lp_filter_taps > MAX_FILTER_SIZE {
                state.lp_filter_taps = (MAX_FILTER_SIZE - 1) | 1;
            }
            let fc = baud as f32 * state.lpf_baud / samples_per_sec as f32;
            dsp::gen_lowpass(
                fc,
                &mut state.lp_filter[..state.lp_filter_taps],
                state.lp_window,
            );
        }
    }

    fn build_space_gain_table() -> [f32; MAX_SUBCHANS] {
        let mut table = [0.0f32; MAX_SUBCHANS];
        table[0] = MIN_G;
        let step = 10.0f32.powf((MAX_G / MIN_G).log10() / (MAX_SUBCHANS as f32 - 1.0));
        for j in 1..MAX_SUBCHANS {
            table[j] = table[j - 1] * step;
        }
        table
    }

    // --- Per-sample processing ---------------------------------------------

    /// Process one audio sample through the demodulator.
    ///
    /// `sam` is a signed audio sample (typically 16-bit range).
    /// Decoded frames accumulate internally; retrieve them with
    /// [`take_frames`](Self::take_frames).
    #[inline(always)]
    pub fn process_sample(&mut self, sam: i32) {
        let fsam = sam as f32 / 16384.0;

        match self.state.profile {
            AfskProfile::A => self.process_profile_a(fsam),
            AfskProfile::B => self.process_profile_b(fsam),
        }

        self.sample_counter = self.sample_counter.wrapping_add(1);
    }

    /// Profile A: dual local oscillator, amplitude comparison.
    #[inline(always)]
    fn process_profile_a(&mut self, mut fsam: f32) {
        let taps = self.state.lp_filter_taps;

        if self.hard_limit {
            fsam = if fsam >= 0.0 { 1.0 } else { -1.0 };
        }

        // Bandpass prefilter
        if self.state.use_prefilter {
            let pre_taps = self.state.pre_filter_taps;
            self.state.pre_filter_buf.push(fsam);
            let pre_slice = self.state.pre_filter_buf.as_slice();
            fsam = convolve(&pre_slice[..pre_taps], &self.state.pre_filter[..pre_taps]);
        }

        // Mix with Mark local oscillator, push into I/Q delay lines
        let m_phase = self.state.afsk.m_osc_phase;
        self.state.afsk.m_i_buf.push(fsam * fcos256(&self.fcos256_table, m_phase));
        self.state.afsk.m_q_buf.push(fsam * fsin256(&self.fcos256_table, m_phase));
        self.state.afsk.m_osc_phase = self.state.afsk.m_osc_phase.wrapping_add(self.state.afsk.m_osc_delta);

        // Mix with Space local oscillator
        let s_phase = self.state.afsk.s_osc_phase;
        self.state.afsk.s_i_buf.push(fsam * fcos256(&self.fcos256_table, s_phase));
        self.state.afsk.s_q_buf.push(fsam * fsin256(&self.fcos256_table, s_phase));
        self.state.afsk.s_osc_phase = self.state.afsk.s_osc_phase.wrapping_add(self.state.afsk.s_osc_delta);

        // Low-pass filter I/Q and compute amplitudes
        let lp = &self.state.lp_filter[..taps];
        let m_i = convolve(&self.state.afsk.m_i_buf.as_slice()[..taps], lp);
        let m_q = convolve(&self.state.afsk.m_q_buf.as_slice()[..taps], lp);
        let m_amp = (m_i * m_i + m_q * m_q).sqrt();

        let s_i = convolve(&self.state.afsk.s_i_buf.as_slice()[..taps], lp);
        let s_q = convolve(&self.state.afsk.s_q_buf.as_slice()[..taps], lp);
        let s_amp = (s_i * s_i + s_q * s_q).sqrt();

        // Track mark/space amplitude peaks for signal level display
        Self::track_level(
            m_amp,
            &mut self.state.alevel_mark_peak,
            self.state.quick_attack,
            self.state.sluggish_decay,
        );
        Self::track_level(
            s_amp,
            &mut self.state.alevel_space_peak,
            self.state.quick_attack,
            self.state.sluggish_decay,
        );

        if self.state.num_slicers <= 1 {
            // Decision-feedback AGC (technique per Ion Todirel W7ION, APRS
            // Users Facebook group): update the reference matching the prior
            // decision's dominant tone, then emit the normalized soft output.
            // Holds the other reference steady to avoid cross-talk when only
            // one tone is active.
            if self.dfb_last_soft > 0.0 {
                self.dfb_mark_ref =
                    DFB_ALPHA_MARK * m_amp + (1.0 - DFB_ALPHA_MARK) * self.dfb_mark_ref;
            } else {
                self.dfb_space_ref =
                    DFB_ALPHA_SPACE * s_amp + (1.0 - DFB_ALPHA_SPACE) * self.dfb_space_ref;
            }
            let soft = m_amp / self.dfb_mark_ref - s_amp / self.dfb_space_ref;
            self.dfb_last_soft = soft;
            self.nudge_pll(0, soft, 1.0);
        } else {
            agc(m_amp, self.state.agc_fast_attack, self.state.agc_slow_decay,
                &mut self.state.m_peak, &mut self.state.m_valley);
            agc(s_amp, self.state.agc_fast_attack, self.state.agc_slow_decay,
                &mut self.state.s_peak, &mut self.state.s_valley);

            for slice in 0..self.state.num_slicers {
                let demod_out = m_amp - s_amp * self.space_gain[slice];
                let amp = (0.5
                    * (self.state.m_peak - self.state.m_valley
                        + (self.state.s_peak - self.state.s_valley) * self.space_gain[slice]))
                    .max(1e-7);
                self.nudge_pll(slice, demod_out, amp);
            }
        }
    }

    /// Profile B: single local oscillator, FM discriminator.
    #[inline(always)]
    fn process_profile_b(&mut self, mut fsam: f32) {
        let taps = self.state.lp_filter_taps;

        if self.hard_limit {
            fsam = if fsam >= 0.0 { 1.0 } else { -1.0 };
        }

        // Bandpass prefilter
        if self.state.use_prefilter {
            let pre_taps = self.state.pre_filter_taps;
            self.state.pre_filter_buf.push(fsam);
            let pre_slice = self.state.pre_filter_buf.as_slice();
            fsam = convolve(&pre_slice[..pre_taps], &self.state.pre_filter[..pre_taps]);
        }

        // Mix with center-frequency oscillator
        let c_phase = self.state.afsk.c_osc_phase;
        self.state.afsk.c_i_buf.push(fsam * fcos256(&self.fcos256_table, c_phase));
        self.state.afsk.c_q_buf.push(fsam * fsin256(&self.fcos256_table, c_phase));
        self.state.afsk.c_osc_phase = self.state.afsk.c_osc_phase.wrapping_add(self.state.afsk.c_osc_delta);

        // Low-pass filter
        let lp = &self.state.lp_filter[..taps];
        let c_i = convolve(&self.state.afsk.c_i_buf.as_slice()[..taps], lp);
        let c_q = convolve(&self.state.afsk.c_q_buf.as_slice()[..taps], lp);

        // FM discriminator: instantaneous frequency ≈ d(phase)/dt
        let phase = c_q.atan2(c_i);
        let mut rate = phase - self.state.afsk.prev_phase;
        if rate > PI {
            rate -= 2.0 * PI;
        } else if rate < -PI {
            rate += 2.0 * PI;
        }
        self.state.afsk.prev_phase = phase;

        let norm_rate = rate * self.state.afsk.normalize_rpsam;

        if self.state.num_slicers <= 1 {
            self.nudge_pll(0, norm_rate, 1.0);
        } else {
            for slice in 0..self.state.num_slicers {
                let offset =
                    -0.5 + slice as f32 / (self.state.num_slicers as f32 - 1.0);
                self.nudge_pll(slice, norm_rate + offset, 1.0);
            }
        }
    }

    /// Fast attack / slow decay level tracker for signal display.
    #[inline(always)]
    fn track_level(amp: f32, peak: &mut f32, attack: f32, decay: f32) {
        if amp >= *peak {
            *peak = amp * attack + *peak * (1.0 - attack);
        } else {
            *peak = amp * decay + *peak * (1.0 - decay);
        }
    }

    // --- PLL and DCD -------------------------------------------------------

    /// Digital PLL clock recovery and bit sampling.
    #[inline(always)]
    fn nudge_pll(&mut self, slice: usize, demod_out: f32, amplitude: f32) {
        let s = &mut self.state.slicer[slice];
        s.prev_d_c_pll = s.data_clock_pll;

        // Unsigned wrapping add, then reinterpret as signed — matches C behavior.
        s.data_clock_pll =
            (s.data_clock_pll as u32).wrapping_add(self.state.pll_step_per_sample as u32) as i32;

        // Bit sampling: fires when the accumulator overflows positive → negative.
        if s.data_clock_pll < 0 && s.prev_d_c_pll > 0 {
            let quality = (demod_out.abs() * 100.0 / amplitude).min(100.0) as i32;
            let raw_bit = demod_out > 0.0;

            self.hdlc[slice].set_audio_level(
                self.state.alevel_mark_peak,
                self.state.alevel_space_peak,
            );

            if let Some(mut frame) = self.hdlc[slice].process_bit(
                raw_bit,
                &mut self.state.slicer[slice].pll_nudge_total,
                &mut self.state.slicer[slice].pll_symbol_count,
            ) {
                frame.quality = quality;
                frame.sample_offset = self.sample_counter;
                self.decoded_frames.push(frame);
            }

            self.state.slicer[slice].pll_symbol_count += 1;
            self.pll_dcd_each_symbol(slice);
        }

        self.state.slicer[slice].prev_demod_out_f = demod_out;

        // On signal transitions, nudge PLL phase toward zero.
        let demod_data = i32::from(demod_out > 0.0);
        if demod_data != self.state.slicer[slice].prev_demod_data {
            self.pll_dcd_signal_transition(slice, self.state.slicer[slice].data_clock_pll);

            let before = self.state.slicer[slice].data_clock_pll as i64;
            let inertia = if self.state.slicer[slice].data_detect {
                self.state.pll_locked_inertia
            } else {
                self.state.pll_searching_inertia
            };
            self.state.slicer[slice].data_clock_pll =
                (self.state.slicer[slice].data_clock_pll as f32 * inertia) as i32;
            let after = self.state.slicer[slice].data_clock_pll as i64;
            self.state.slicer[slice].pll_nudge_total += after - before;
        }

        self.state.slicer[slice].prev_demod_data = demod_data;
    }

    /// Score whether a signal transition is near the expected PLL phase.
    #[inline(always)]
    fn pll_dcd_signal_transition(&mut self, slice: usize, dpll_phase: i32) {
        let threshold = DCD_GOOD_WIDTH * 1024 * 1024;
        if dpll_phase > -threshold && dpll_phase < threshold {
            self.state.slicer[slice].good_flag = true;
        } else {
            self.state.slicer[slice].bad_flag = true;
        }
    }

    /// Evaluate DCD for one symbol period. Emits a [`DcdChange`] on transitions.
    #[inline(always)]
    fn pll_dcd_each_symbol(&mut self, slice: usize) {
        let s = &mut self.state.slicer[slice];

        s.good_hist = (s.good_hist << 1) | u8::from(s.good_flag);
        s.good_flag = false;
        s.bad_hist = (s.bad_hist << 1) | u8::from(s.bad_flag);
        s.bad_flag = false;

        let good_count = s.good_hist.count_ones() as i32;
        let bad_count = s.bad_hist.count_ones() as i32;
        s.score = (s.score << 1) | u32::from(good_count - bad_count >= 2);

        let popcount = s.score.count_ones();
        let new_detect = if popcount >= DCD_THRESH_ON {
            true
        } else if popcount <= DCD_THRESH_OFF {
            false
        } else {
            s.data_detect
        };

        if new_detect != s.data_detect {
            s.data_detect = new_detect;
            self.dcd_changes.push(DcdChange {
                chan: self.chan,
                subchan: self.subchan,
                slice,
                data_detect: new_detect,
            });
        }
    }

    // --- Public query / mutation API ----------------------------------------

    /// Take all decoded frames, leaving the internal buffer empty.
    #[must_use]
    pub fn take_frames(&mut self) -> Vec<DecodedFrame> {
        std::mem::take(&mut self.decoded_frames)
    }

    /// Sum of bad-FCS events across every slicer since the last call.
    /// Each slicer decodes the same bit stream differently, so a single
    /// corrupted transmission may bump this by up to num_slicers.
    #[must_use]
    pub fn take_bad_fcs(&mut self) -> u64 {
        self.hdlc.iter_mut().map(|d| d.take_bad_fcs()).sum()
    }

    /// Number of decoded frames accumulated so far.
    #[must_use]
    pub fn frame_count(&self) -> usize {
        self.decoded_frames.len()
    }

    /// Take all DCD state changes since the last call.
    #[must_use]
    pub fn take_dcd_changes(&mut self) -> Vec<DcdChange> {
        std::mem::take(&mut self.dcd_changes)
    }

    /// Whether data carrier is detected on a specific slicer.
    #[must_use]
    pub fn data_detect(&self, slice: usize) -> bool {
        self.state.slicer[slice].data_detect
    }

    /// Whether data carrier is detected on **any** slicer.
    #[must_use]
    pub fn data_detect_any(&self) -> bool {
        self.state.slicer[..self.state.num_slicers]
            .iter()
            .any(|s| s.data_detect)
    }

    /// Mark and space amplitude peaks for signal level display.
    #[must_use]
    pub fn audio_level(&self) -> (f32, f32) {
        (self.state.alevel_mark_peak, self.state.alevel_space_peak)
    }

    /// The channel and subchannel this demodulator is assigned to.
    #[must_use]
    pub fn chan_subchan(&self) -> (usize, usize) {
        (self.chan, self.subchan)
    }

    /// Change the number of slicers, reinitializing HDLC decoders.
    ///
    /// # Panics
    ///
    /// Panics if `n` is 0 or greater than [`MAX_SLICERS`].
    pub fn set_num_slicers(&mut self, n: usize) {
        assert!(n >= 1 && n <= MAX_SLICERS);
        self.state.num_slicers = n;
        self.hdlc = (0..n)
            .map(|s| HdlcDecoder::new(self.chan, self.subchan, s, false))
            .collect();
    }

    /// Set the bit-error correction level for all HDLC decoders.
    pub fn set_fix_bits(&mut self, level: RetryType) {
        for h in &mut self.hdlc {
            h.set_fix_bits(level);
        }
    }
}
