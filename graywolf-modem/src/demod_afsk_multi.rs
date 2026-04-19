//! Multi-configuration AFSK demodulator.
//!
//! Runs several [`AfskDemodulator`] instances over the same audio stream in
//! parallel (conceptually — processing is actually serialized per sample) and
//! emits a deduplicated stream of decoded frames.
//!
//! # Attribution
//!
//! Two of the building blocks the recommended ensemble relies on — the
//! decision-feedback AGC inside Profile A and the hard-limiter-before-
//! bandpass correlator used by the `A9HL` variant — are based on designs
//! published by Ion Todirel (W7ION) on the APRS Users Facebook group.
//! His reference implementation at <https://github.com/iontodirel/libmodem>
//! was the source of both techniques. The ensemble architecture that
//! combines them across Profile A / A-with-HL / Profile B is also his
//! "hydra" idea, adapted here to graywolf's existing profile ports.
//!
//! # Why this exists
//!
//! No single demodulator configuration is best across all radio conditions:
//!
//! - Profile A with a hard-limiter is the best single configuration on clean
//!   audio (about +4 packets on the WA8LMF Track 1 reference at Bell 202),
//!   but collapses on de-emphasized audio because the hard-limiter captures
//!   the stronger mark tone and erases the weaker space tone.
//! - Profile A without a hard-limiter is the best single configuration on
//!   de-emphasized audio but leaves a few packets on the table when audio
//!   is flat.
//! - Profile B (FM discriminator) catches a small residual set that the
//!   amplitude-comparison profile A never sees.
//!
//! Running three demodulators in parallel and unioning their outputs lands
//! within 0.3% of the best theoretical coverage our demodulator family can
//! achieve, regardless of channel condition.
//!
//! # Deduplication
//!
//! The same transmission will usually be decoded by two or three of the
//! demodulators, and within multi-slicer variants also by several slicers
//! in the same demod. We dedup by _(frame content, sample offset within a
//! configurable window)_ so that:
//!
//! - A rebroadcast of the same frame several seconds later counts as two
//!   events (two real transmissions).
//! - The same physical transmission decoded by two demods that happened to
//!   fire their closing-flag detection a few samples apart counts as one.
//!
//! The default window is 3 symbol times (110 samples at 44.1 kHz / 1200
//! baud), matching Direwolf's `multi_modem.c` PROCESS_AFTER_BITS. This is
//! wide enough to collapse the same transmission decoded by several
//! slicers within one symbol period, yet narrow enough that legitimate
//! fast rebroadcasts (digipeater hops, APRS-IS reinjection) count as
//! separate events — matching Direwolf's event-counting semantics exactly.
//!
//! # Typical use
//!
//! ```no_run
//! use graywolf_demod::demod_afsk_multi::{MultiAfskDemodulator, MultiConfig};
//! use graywolf_demod::types::AfskProfile;
//!
//! let mut demod = MultiAfskDemodulator::new(
//!     44100, 1200, 1200, 2200,
//!     &[
//!         MultiConfig { profile: AfskProfile::A, slicers: 9, hard_limit: false },
//!         MultiConfig { profile: AfskProfile::A, slicers: 9, hard_limit: true  },
//!         MultiConfig { profile: AfskProfile::B, slicers: 9, hard_limit: false },
//!     ],
//! );
//!
//! for sample in audio_samples() {
//!     demod.process_sample(sample);
//! }
//!
//! for frame in demod.take_frames() {
//!     // exactly one frame per real transmission event
//! }
//! # fn audio_samples() -> Vec<i32> { vec![] }
//! ```

use std::collections::HashMap;

use crate::demod_afsk::AfskDemodulator;
use crate::hdlc::DecodedFrame;
use crate::types::AfskProfile;

/// Sample-offset window used to merge same-content frames emitted close
/// together in time. Matches Direwolf's `multi_modem.c` PROCESS_AFTER_BITS
/// constant of 3 symbol times, which at 1200 baud / 44100 sps is ≈110
/// samples (~2.5 ms).
///
/// This is narrow enough that legitimate rapid rebroadcasts (digipeaters,
/// APRS-IS injection) count as separate events, and wide enough to collapse
/// the same transmission decoded by multiple slicers or parallel profiles
/// within one symbol period into a single event.
///
/// The value is expressed in samples rather than milliseconds so it scales
/// directly with the configured sample rate. Callers running at non-44.1 kHz
/// rates or at non-1200 baud should override via
/// [`MultiAfskDemodulator::set_window_samples`].
pub const DEFAULT_WINDOW_SAMPLES: u64 = 110;

/// Recommended 3-demodulator ensemble with broad coverage across channel
/// conditions.
///
/// - Profile A, 9 slicers, no hard-limit — universal workhorse, dominant on
///   de-emphasized audio where a hard-limiter would capture the mark tone.
/// - Profile A, 9 slicers, hard-limit on — small Track 1-style bonus on
///   flat audio; contributes nothing on de-emphasized audio but costs
///   nothing either once unioned.
/// - Profile B, 9 slicers, no hard-limit — FM-discriminator topology that
///   occasionally catches a packet the amplitude-comparison demods miss.
///
/// Measured at ~1.6% of one CPU core processing real-time 44.1 kHz audio on
/// an M-series Mac. Reaches 99.7% of the all-10-variant coverage ceiling
/// across all four WA8LMF reference tracks.
pub const RECOMMENDED_3DEMOD: [MultiConfig; 3] = [
    MultiConfig { profile: AfskProfile::A, slicers: 9, hard_limit: false },
    MultiConfig { profile: AfskProfile::A, slicers: 9, hard_limit: true  },
    MultiConfig { profile: AfskProfile::B, slicers: 9, hard_limit: false },
];

/// Recommended 2-demodulator ensemble. About half the CPU of
/// [`RECOMMENDED_3DEMOD`] and within 3 events (of ~1000) on every reference
/// track. Drop Profile B if CPU is at a premium.
pub const RECOMMENDED_2DEMOD: [MultiConfig; 2] = [
    MultiConfig { profile: AfskProfile::A, slicers: 9, hard_limit: false },
    MultiConfig { profile: AfskProfile::A, slicers: 9, hard_limit: true  },
];

/// One demodulator configuration in a [`MultiAfskDemodulator`].
#[derive(Clone, Copy, Debug)]
pub struct MultiConfig {
    pub profile: AfskProfile,
    pub slicers: usize,
    pub hard_limit: bool,
}

/// Ensemble of AFSK demodulators sharing one audio stream, with cross-demod
/// deduplication of output frames.
pub struct MultiAfskDemodulator {
    demods: Vec<AfskDemodulator>,
    /// last-seen sample offset per frame content; used for cross-demod dedup
    last_seen: HashMap<Vec<u8>, u64>,
    /// accumulated deduped frame output, ready for `take_frames`
    out: Vec<DecodedFrame>,
    window_samples: u64,
}

impl MultiAfskDemodulator {
    /// Create a new multi-demodulator. `configs` must be non-empty.
    ///
    /// # Panics
    ///
    /// Panics if `configs` is empty.
    pub fn new(
        samples_per_sec: u32,
        baud: u32,
        mark_freq: u32,
        space_freq: u32,
        configs: &[MultiConfig],
    ) -> Self {
        assert!(!configs.is_empty(), "MultiAfskDemodulator needs at least one config");
        let demods: Vec<AfskDemodulator> = configs
            .iter()
            .enumerate()
            .map(|(i, c)| {
                let mut d = AfskDemodulator::new(
                    samples_per_sec,
                    baud,
                    mark_freq,
                    space_freq,
                    c.profile,
                    0,
                    i,
                );
                if c.slicers > 1 {
                    d.set_num_slicers(c.slicers);
                }
                if c.hard_limit {
                    d.set_hard_limit(true);
                }
                d
            })
            .collect();
        Self {
            demods,
            last_seen: HashMap::new(),
            out: Vec::new(),
            window_samples: DEFAULT_WINDOW_SAMPLES,
        }
    }

    /// Override the cross-demod dedup window (in audio samples).
    pub fn set_window_samples(&mut self, window: u64) {
        self.window_samples = window;
    }

    /// How many demodulator configurations this instance is running.
    pub fn num_configs(&self) -> usize {
        self.demods.len()
    }

    /// Feed one audio sample through every contained demodulator. Any
    /// frames emitted go through the cross-demod dedup filter and accumulate
    /// in the output buffer, which you can drain with [`take_frames`].
    #[inline]
    pub fn process_sample(&mut self, sam: i32) {
        for d in &mut self.demods {
            d.process_sample(sam);
        }
        // Collect freshly emitted frames from each demodulator and dedup
        // across the ensemble using (content, sample_offset within window).
        for d in &mut self.demods {
            for frame in d.take_frames() {
                let prev = self.last_seen.get(&frame.data).copied();
                let keep = match prev {
                    Some(p) => frame.sample_offset.saturating_sub(p) >= self.window_samples,
                    None => true,
                };
                if keep {
                    self.last_seen.insert(frame.data.clone(), frame.sample_offset);
                    self.out.push(frame);
                }
            }
        }
    }

    /// Drain deduped output frames accumulated since the last call.
    pub fn take_frames(&mut self) -> Vec<DecodedFrame> {
        std::mem::take(&mut self.out)
    }

    /// Total deduped frames currently buffered (not yet drained).
    pub fn frame_count(&self) -> usize {
        self.out.len()
    }

    /// Whether any underlying demodulator reports Data Carrier Detect.
    pub fn data_detect_any(&self) -> bool {
        self.demods.iter().any(|d| d.data_detect_any())
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::{DEFAULT_BAUD, DEFAULT_MARK_FREQ, DEFAULT_SAMPLES_PER_SEC, DEFAULT_SPACE_FREQ};

    #[test]
    fn constructs_with_one_config() {
        let cfg = [MultiConfig {
            profile: AfskProfile::A,
            slicers: 1,
            hard_limit: false,
        }];
        let m = MultiAfskDemodulator::new(
            DEFAULT_SAMPLES_PER_SEC,
            DEFAULT_BAUD,
            DEFAULT_MARK_FREQ,
            DEFAULT_SPACE_FREQ,
            &cfg,
        );
        assert_eq!(m.num_configs(), 1);
        assert_eq!(m.frame_count(), 0);
    }

    #[test]
    #[should_panic]
    fn rejects_empty_configs() {
        let _ = MultiAfskDemodulator::new(
            DEFAULT_SAMPLES_PER_SEC,
            DEFAULT_BAUD,
            DEFAULT_MARK_FREQ,
            DEFAULT_SPACE_FREQ,
            &[],
        );
    }

    #[test]
    fn silence_produces_no_frames() {
        let cfg = [
            MultiConfig { profile: AfskProfile::A, slicers: 1, hard_limit: false },
            MultiConfig { profile: AfskProfile::A, slicers: 1, hard_limit: true  },
        ];
        let mut m = MultiAfskDemodulator::new(
            DEFAULT_SAMPLES_PER_SEC,
            DEFAULT_BAUD,
            DEFAULT_MARK_FREQ,
            DEFAULT_SPACE_FREQ,
            &cfg,
        );
        for _ in 0..DEFAULT_SAMPLES_PER_SEC {
            m.process_sample(0);
        }
        assert!(m.take_frames().is_empty());
    }
}
