//! Transmit-path DSP.
//!
//! Turns an AX.25 frame into a buffer of audio samples ready to hand to the
//! sound card. Phase A of the TX work is pure DSP — no I/O, no threading,
//! no IPC wiring; just the mathematical building blocks plus thorough
//! round-trip tests against the existing [`crate::hdlc::HdlcDecoder`] and
//! [`crate::demod_afsk::AfskDemodulator`].

pub mod afsk_mod;
pub mod canned;
mod error;
pub mod hdlc_encode;
pub(crate) mod ptt;
pub mod ptt_android_consts;
#[cfg(any(target_os = "android", feature = "android-test-stub"))]
pub(crate) mod ptt_android;

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

/// Build native Bell 202 FX.25 audio. Oversized frames use ordinary AX.25,
/// matching Dire Wolf. Unsupported rates/parity still return errors.
pub fn build_fx25_samples(
    frame: &[u8],
    txdelay_ms: u32,
    txtail_ms: u32,
    sample_rate: u32,
    baud: u32,
    mark_freq: u32,
    space_freq: u32,
) -> Result<Vec<i16>, TxError> {
    build_fx25_samples_with_parity(
        frame,
        txdelay_ms,
        txtail_ms,
        sample_rate,
        baud,
        mark_freq,
        space_freq,
        0,
    )
}

/// Same production path with an explicit standard parity count (0 = automatic).
#[allow(clippy::too_many_arguments)]
pub fn build_fx25_samples_with_parity(
    frame: &[u8],
    txdelay_ms: u32,
    txtail_ms: u32,
    sample_rate: u32,
    baud: u32,
    mark_freq: u32,
    space_freq: u32,
    parity: u32,
) -> Result<Vec<i16>, TxError> {
    Ok(build_fx25_transmission(frame, txdelay_ms, txtail_ms, sample_rate,
        baud, mark_freq, space_freq, parity)?.samples)
}

/// PCM and its actual encoding, frozen together before queueing.
pub struct BuiltTransmission {
    pub samples: Vec<i16>,
    pub encoding: crate::ipc::proto::TransmitEncoding,
}

#[allow(clippy::too_many_arguments)]
pub fn build_fx25_transmission(
    frame: &[u8], txdelay_ms: u32, txtail_ms: u32, sample_rate: u32,
    baud: u32, mark_freq: u32, space_freq: u32, parity: u32,
) -> Result<BuiltTransmission, TxError> {
    if (baud, mark_freq, space_freq) != (1200, 1200, 2200) {
        return Err(TxError::UnsupportedFx25);
    }
    if !matches!(parity, 0 | 16 | 32 | 64) {
        return Err(TxError::UnsupportedFx25Parity);
    }
    if frame.len() < 15 { return Err(TxError::InvalidFx25Frame); }
    let Some(wire) = crate::fx25::encode_with_parity(frame, parity) else {
        return Ok(BuiltTransmission {
            samples: build_samples(frame, txdelay_ms, txtail_ms, sample_rate, baud, mark_freq, space_freq)?,
            encoding: crate::ipc::proto::TransmitEncoding {
                encoding: "ax25".into(), fx25_requested: true,
                fallback_reason: "frame_too_large".into(), ..Default::default()
            },
        });
    };
    let tag_value = u64::from_le_bytes(wire[..8].try_into().unwrap());
    let tag = crate::fx25::FX25_TAGS.iter().position(|t| t.0 == tag_value).unwrap();
    let (_, _, data, check) = crate::fx25::FX25_TAGS[tag];
    let encoding = crate::ipc::proto::TransmitEncoding {
        encoding: "fx25".into(), tag: tag as u32, data_bytes: data as u32,
        parity_bytes: check as u32, fx25_requested: true, ..Default::default()
    };
    let bytes = std::iter::repeat_n(0x7e, flags_for_ms(txdelay_ms, baud).max(1))
        .chain(wire)
        .chain(std::iter::repeat_n(
            0x7e,
            flags_for_ms(txtail_ms, baud).max(1),
        ));
    let mut line = 0;
    let bits: Vec<u8> = bytes
        .flat_map(|b| (0..8).map(move |n| (b >> n) & 1))
        .map(|bit| {
            if bit == 0 {
                line ^= 1;
            }
            line
        })
        .collect();
    Ok(BuiltTransmission {
        samples: afsk_mod::modulate(&bits, sample_rate, baud, mark_freq, space_freq)?, encoding,
    })
}

/// Prepend `ms` milliseconds of silence (at `sample_rate`) to `samples`.
///
/// Used by the Android Digirig Lite tone PTT path: the AFSK (left channel)
/// stays silent during the lead-in while the right-channel keying tone plays,
/// so the radio keys before the HDLC preamble. Mirrors the desktop
/// `DIGIRIG_TONE_LEAD_MS` silent lead-in. `pub(crate)` so the android arm can
/// call it; `#[allow(dead_code)]` because non-android desktop builds never do.
#[allow(dead_code)]
pub(crate) fn prepend_silence(samples: Vec<i16>, sample_rate: u32, ms: u32) -> Vec<i16> {
    let lead = (sample_rate as u64 * ms as u64 / 1000) as usize;
    let mut buf = Vec::with_capacity(lead + samples.len());
    buf.resize(lead, 0i16);
    buf.extend_from_slice(&samples);
    buf
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

    #[test]
    fn prepend_silence_adds_exact_lead() {
        let out = prepend_silence(vec![7i16; 10], 22050, 500);
        assert_eq!(out.len(), 11035); // 11025 silent + 10 original
        assert!(out[..11025].iter().all(|&s| s == 0));
        assert_eq!(&out[11025..], &[7i16; 10]);
    }

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

#[cfg(test)]
mod fx25_transmit_metadata_tests {
    use super::*;
    #[test]
    fn fx25_actual_format_and_explicit_size_fallback() {
        for parity in [0, 16, 32, 64] {
            let frame = vec![0; 150];
            let built = build_fx25_transmission(&frame,300,100,48000,1200,1200,2200,parity).unwrap();
            assert_eq!(built.encoding.encoding,"fx25");
            let (_,_,data,check)=crate::fx25::FX25_TAGS[built.encoding.tag as usize];
            assert_eq!(built.encoding.data_bytes,data as u32);
            assert_eq!(built.encoding.parity_bytes,check as u32);
            assert_eq!(built.samples,build_fx25_samples_with_parity(&frame,300,100,48000,1200,1200,2200,parity).unwrap());
            // Fallback includes frames beyond the absolute maximum and
            // fixed-mode overflows which still fit a different parity family.
            for frame in [vec![0;300],vec![255;235]] {
                let b=build_fx25_transmission(&frame,300,100,48000,1200,1200,2200,parity).unwrap();
                assert_eq!(b.samples,build_samples(&frame,300,100,48000,1200,1200,2200).unwrap());
                assert_eq!(b.encoding.encoding,"ax25");
                assert_eq!(b.encoding.fallback_reason,"frame_too_large");
                assert_eq!(b.encoding.tag,0);
                assert!(b.encoding.fx25_requested);
            }
        }
        let frame=vec![0;220];
        for parity in [32,64] {
            let b=build_fx25_transmission(&frame,300,100,48000,1200,1200,2200,parity).unwrap();
            assert_eq!(b.encoding.fallback_reason,"frame_too_large");
        }
        assert!(matches!(build_fx25_transmission(&[0;100],300,100,48000,300,1600,1800,0),Err(TxError::UnsupportedFx25)));
        assert!(matches!(build_fx25_transmission(&[0;100],300,100,48000,1200,1200,2200,15),Err(TxError::UnsupportedFx25Parity)));
        assert!(matches!(build_fx25_transmission(&[0;14],300,100,48000,1200,1200,2200,0),Err(TxError::InvalidFx25Frame)));
    }
}
