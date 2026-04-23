//! Transmit-path DSP.
//!
//! Turns an AX.25 frame into a buffer of audio samples ready to hand to the
//! sound card. Phase A of the TX work is pure DSP — no I/O, no threading,
//! no IPC wiring; just the mathematical building blocks plus thorough
//! round-trip tests against the existing [`crate::hdlc::HdlcDecoder`] and
//! [`crate::demod_afsk::AfskDemodulator`].

pub mod afsk_mod;
mod error;
pub mod hdlc_encode;
pub(crate) mod ptt;

pub use error::TxError;

/// Build a self-contained mono i16 audio buffer for transmitting one AX.25
/// frame as AFSK at the given `baud` rate and tone pair.
///
/// `frame_bytes` is the AX.25 frame **without** its FCS — the encoder
/// computes and appends the CRC-16/X.25 FCS. `txdelay_ms` and `txtail_ms`
/// bracket the frame with the corresponding duration of HDLC flag bytes at
/// the configured baud; `txdelay_ms` is the *total* audio preamble and is
/// what covers the radio's transmitter attack time (no separate PTT-to-audio
/// dead period).
///
/// `sample_rate` is the output sample rate in Hz — typically 48000. Zero
/// `sample_rate` or zero `baud` returns [`TxError::InvalidSampleRate`].
pub fn build_samples(
    frame_bytes: &[u8],
    txdelay_ms: u32,
    txtail_ms: u32,
    sample_rate: u32,
    baud: u32,
    mark_freq: u32,
    space_freq: u32,
) -> Result<Vec<i16>, TxError> {
    let preamble = flags_for_ms(txdelay_ms, baud);
    let postamble = flags_for_ms(txtail_ms, baud);
    let bits = hdlc_encode::encode(frame_bytes, preamble, postamble);
    afsk_mod::modulate(&bits, sample_rate, baud, mark_freq, space_freq)
}

/// Number of `0x7e` flag bytes needed to cover `ms` milliseconds of airtime
/// at the given baud. Each flag is 8 bits, so one flag occupies
/// `8000 / baud` ms on the air; we round up so the preamble is never
/// shorter than the caller asked for.
fn flags_for_ms(ms: u32, baud: u32) -> usize {
    if baud == 0 {
        return 0;
    }
    ((ms as u64) * baud as u64).div_ceil(8000) as usize
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::demod_afsk::AfskDemodulator;
    use crate::types::AfskProfile;

    /// A plausible AX.25 UI frame: destination APRS, source KG7HFX-0,
    /// control 0x03 (UI), PID 0xF0 (no layer 3), short info. The bytes
    /// pass the minimum-length check but are otherwise just fuel for the
    /// round-trip test.
    fn sample_beacon() -> Vec<u8> {
        vec![
            0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0x60, // APRS
            0x96, 0x8e, 0x6e, 0x90, 0x90, 0xb0, 0x61, // KG7HFX-0
            0x03, 0xf0, // control, PID
            b'!', b'4', b'7', b'0', b'3', b'.', b'0', b'0', b'N', b'/', b'1', b'2', b'2', b'3',
            b'0', b'.', b'0', b'0', b'W', b'>', b't', b'e', b's', b't',
        ]
    }

    #[test]
    fn flags_for_ms_rounds_up_at_1200_baud() {
        assert_eq!(flags_for_ms(0, 1200), 0);
        assert_eq!(flags_for_ms(1, 1200), 1); // 0.15 → 1
        assert_eq!(flags_for_ms(100, 1200), 15);
        assert_eq!(flags_for_ms(300, 1200), 45);
        assert_eq!(flags_for_ms(53, 1200), 8); // 7.95 → 8
    }

    #[test]
    fn flags_for_ms_rounds_up_at_300_baud() {
        // At 300 baud each flag byte lasts 8/300 s ≈ 26.67 ms, so 300 ms of
        // preamble needs ceil(300 * 300 / 8000) = 12 flags. This is the
        // asymmetry that used to silently break HF APRS beacons.
        assert_eq!(flags_for_ms(0, 300), 0);
        assert_eq!(flags_for_ms(26, 300), 1); // 0.975 → 1
        assert_eq!(flags_for_ms(27, 300), 2); // 1.0125 → 2
        assert_eq!(flags_for_ms(300, 300), 12);
        assert_eq!(flags_for_ms(1000, 300), 38); // 37.5 → 38
    }

    #[test]
    fn build_samples_length_equals_encoded_bits_times_samples_per_bit_at_48k() {
        // At 48 kHz the fractional samples-per-bit accumulator is degenerate
        // — exactly 40 samples per bit — so `build_samples` must produce
        // `bit_count * 40` samples with no extra padding or dropped tail.
        let frame = sample_beacon();
        let preamble = flags_for_ms(300, 1200);
        let postamble = flags_for_ms(100, 1200);
        let bits = hdlc_encode::encode(&frame, preamble, postamble);
        let samples = build_samples(&frame, 300, 100, 48_000, 1200, 1200, 2200).unwrap();
        assert_eq!(samples.len(), bits.len() * 40);
    }

    #[test]
    fn build_samples_is_unbiased_and_peak_bounded() {
        let frame = sample_beacon();
        let samples = build_samples(&frame, 300, 100, 48_000, 1200, 1200, 2200).unwrap();

        // No DC bias — sum should be well under a few percent of peak×count.
        let sum: i64 = samples.iter().map(|&s| s as i64).sum();
        let bound = (samples.len() as i64) * 16_384 / 100;
        assert!(
            sum.abs() < bound,
            "DC offset {} exceeded tolerance {}",
            sum,
            bound
        );

        // Peak within 5% of the target amplitude.
        let peak = samples
            .iter()
            .map(|&s| s.unsigned_abs() as i32)
            .max()
            .unwrap();
        assert!(
            (peak - 16_384).abs() <= 16_384 / 20,
            "peak amplitude {} outside 5% of target",
            peak
        );
    }

    #[test]
    fn build_samples_round_trips_through_the_afsk_demodulator() {
        let frame = sample_beacon();
        let samples = build_samples(&frame, 300, 100, 48_000, 1200, 1200, 2200).unwrap();

        let mut demod = AfskDemodulator::new(48_000, 1200, 1200, 2200, AfskProfile::A, 0, 0);
        for &s in &samples {
            demod.process_sample(s as i32);
        }
        let frames = demod.take_frames();
        assert!(
            frames.iter().any(|f| f.data == frame),
            "demodulator decoded {} frames, none matched the input",
            frames.len()
        );
    }

    #[test]
    fn hf_aprs_300_baud_round_trips_through_matched_demodulator() {
        // Regression test for issue #22: the TX path used to emit 1200 baud
        // Bell 202 regardless of channel config, so an HF APRS channel
        // (300 baud, 1600/1800 Hz) beacon produced audio the matching
        // demodulator could not decode. This test builds TX audio at 300
        // baud / 1600 / 1800 and requires the 300-baud demodulator to
        // recover the original frame.
        let frame = sample_beacon();
        let samples = build_samples(&frame, 300, 100, 48_000, 300, 1600, 1800).unwrap();

        let mut demod = AfskDemodulator::new(48_000, 300, 1600, 1800, AfskProfile::A, 0, 0);
        for &s in &samples {
            demod.process_sample(s as i32);
        }
        let frames = demod.take_frames();
        assert!(
            frames.iter().any(|f| f.data == frame),
            "300-baud demodulator decoded {} frames, none matched the input",
            frames.len()
        );
    }

    #[test]
    fn zero_sample_rate_returns_invalid_sample_rate_error() {
        let err = build_samples(&sample_beacon(), 300, 100, 0, 1200, 1200, 2200).unwrap_err();
        assert_eq!(err, TxError::InvalidSampleRate);
    }
}
