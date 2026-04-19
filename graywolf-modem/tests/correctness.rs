use graywolf_demod::demod_afsk::AfskDemodulator;
use graywolf_demod::dsp;
use graywolf_demod::hdlc;
use graywolf_demod::types::*;

#[test]
fn test_fcs_calc_known_values() {
    let crc = hdlc::fcs_calc(&[]);
    assert_eq!(crc, 0x0000); // 0xffff ^ 0xffff

    let data = b"123456789";
    let crc = hdlc::fcs_calc(data);
    assert_eq!(crc, 0x906E);
}

#[test]
fn test_crc16_incremental() {
    let data = b"123456789";

    let batch_crc = hdlc::fcs_calc(data);

    let mut crc: u16 = 0xffff;
    for &byte in data.iter() {
        crc = hdlc::crc16_step(crc, byte);
    }
    let incremental_crc = hdlc::crc16_finalize(crc);

    assert_eq!(batch_crc, incremental_crc);
}

#[test]
fn test_descramble() {
    let mut state: i32 = 0;
    let input_bits = [1, 0, 1, 1, 0, 0, 1, 0, 1, 1, 1, 0, 0, 1, 0, 1];
    let mut output = Vec::new();
    for &bit in &input_bits {
        output.push(hdlc::descramble(bit, &mut state));
    }
    assert_eq!(output.len(), input_bits.len());
}

#[test]
fn test_window_truncated() {
    for j in 0..10 {
        let w = dsp::window(WindowType::Truncated, 10, j);
        assert!((w - 1.0).abs() < 1e-6);
    }
}

#[test]
fn test_window_hamming_symmetry() {
    let size = 11;
    for j in 0..size / 2 {
        let w1 = dsp::window(WindowType::Hamming, size, j);
        let w2 = dsp::window(WindowType::Hamming, size, size - 1 - j);
        assert!(
            (w1 - w2).abs() < 1e-5,
            "Hamming window not symmetric at j={}",
            j
        );
    }
}

#[test]
fn test_gen_lowpass_unity_dc_gain() {
    let mut filter = [0.0f32; 51];
    dsp::gen_lowpass(0.1, &mut filter, WindowType::Hamming);

    let sum: f32 = filter.iter().sum();
    assert!(
        (sum - 1.0).abs() < 1e-4,
        "lowpass filter DC gain = {}, expected 1.0",
        sum
    );
}

#[test]
fn test_gen_ms_normalized() {
    let mut sin_table = [0.0f32; 101];
    let mut cos_table = [0.0f32; 101];
    dsp::gen_ms(1200, 44100, &mut sin_table, &mut cos_table, 101, WindowType::Hamming);

    let mut gs: f32 = 0.0;
    let mut gc: f32 = 0.0;
    for j in 0..101 {
        let center = 0.5 * (101.0 - 1.0);
        let am = ((j as f32 - center) / 44100.0) * 1200.0 * 2.0 * std::f32::consts::PI;
        gs += sin_table[j] * am.sin();
        gc += cos_table[j] * am.cos();
    }

    assert!(
        (gs - 1.0).abs() < 0.1,
        "gen_ms sin table correlation = {}, expected ~1.0",
        gs
    );
    assert!(
        (gc - 1.0).abs() < 0.1,
        "gen_ms cos table correlation = {}, expected ~1.0",
        gc
    );
}

#[test]
fn test_rrc_center_value() {
    let val = dsp::rrc(0.0, 0.5);
    assert!(
        (val - 1.0).abs() < 1e-4,
        "rrc(0, 0.5) = {}, expected 1.0",
        val
    );
}

#[test]
fn test_hdlc_flag_detection() {
    let mut decoder = hdlc::HdlcDecoder::new(0, 0, 0, false);

    let flag_raw_bits = [true, true, true, true, true, true, true, false];

    let mut dummy_nudge: i64 = 0;
    let mut dummy_count: i32 = 0;

    for _ in 0..4 {
        for &bit in &flag_raw_bits {
            let result = decoder.process_bit(bit, &mut dummy_nudge, &mut dummy_count);
            assert!(result.is_none(), "unexpected frame from flag sequence");
        }
    }
}

#[test]
fn test_hdlc_is_gathering() {
    let mut decoder = hdlc::HdlcDecoder::new(0, 0, 0, false);
    let mut dummy_nudge: i64 = 0;
    let mut dummy_count: i32 = 0;

    assert!(!decoder.is_gathering());

    let flag_raw_bits = [true, true, true, true, true, true, true, false];
    for &bit in &flag_raw_bits {
        decoder.process_bit(bit, &mut dummy_nudge, &mut dummy_count);
    }

    assert!(decoder.is_gathering());
}

#[test]
fn test_raw_bit_buffer() {
    let mut buf = hdlc::RawBitBuffer::new(0, 0, 0, false, 0, 0);
    assert!(buf.is_empty());
    assert_eq!(buf.len(), 0);

    buf.append_bit(1);
    buf.append_bit(0);
    buf.append_bit(1);
    assert_eq!(buf.len(), 3);
    assert_eq!(buf.get_bit(0), 1);
    assert_eq!(buf.get_bit(1), 0);
    assert_eq!(buf.get_bit(2), 1);

    for _ in 0..8 {
        buf.append_bit(1);
    }
    assert_eq!(buf.len(), 11);
    buf.chop8();
    assert_eq!(buf.len(), 3);
}

#[test]
fn test_composite_dcd() {
    let mut dcd = hdlc::CompositeDcd::new();
    dcd.set_num_subchans(0, 2);

    assert!(!dcd.data_detect_any(0));

    let change = dcd.dcd_change(0, 0, 0, true);
    assert_eq!(change, Some((0, true)));
    assert!(dcd.data_detect_any(0));

    let change = dcd.dcd_change(0, 0, 0, false);
    assert_eq!(change, Some((0, false)));
    assert!(!dcd.data_detect_any(0));

    dcd.dcd_change(0, 0, 0, true);
    dcd.dcd_change(0, 1, 0, true);
    let change = dcd.dcd_change(0, 0, 0, false);
    assert!(change.is_none());
    assert!(dcd.data_detect_any(0));
}

#[test]
fn test_demodulator_creation() {
    let demod = AfskDemodulator::new(44100, 1200, 1200, 2200, AfskProfile::A, 0, 0);
    assert_eq!(demod.frame_count(), 0);
    assert!(demod.state.lp_filter_taps > 0);
    assert!(demod.state.pre_filter_taps > 0);
}

#[test]
fn test_demodulator_profile_b_creation() {
    let demod = AfskDemodulator::new(44100, 1200, 1200, 2200, AfskProfile::B, 0, 0);
    assert_eq!(demod.frame_count(), 0);
    assert!(demod.state.afsk.normalize_rpsam > 0.0);
}

#[test]
fn test_demodulator_multi_slicer() {
    let mut demod = AfskDemodulator::new(44100, 1200, 1200, 2200, AfskProfile::A, 0, 0);
    demod.set_num_slicers(5);
    assert_eq!(demod.state.num_slicers, 5);

    for _ in 0..44100 {
        demod.process_sample(0);
    }
    assert_eq!(demod.frame_count(), 0);
}

#[test]
fn test_demodulator_dcd_changes() {
    let mut demod = AfskDemodulator::new(44100, 1200, 1200, 2200, AfskProfile::A, 0, 0);

    for _ in 0..44100 {
        demod.process_sample(0);
    }

    let changes = demod.take_dcd_changes();
    assert!(!demod.data_detect_any());
    let _ = changes;
}

#[test]
fn test_process_silence() {
    let mut demod = AfskDemodulator::new(44100, 1200, 1200, 2200, AfskProfile::A, 0, 0);

    for _ in 0..44100 {
        demod.process_sample(0);
    }

    assert_eq!(demod.frame_count(), 0);
}

#[test]
fn test_demodulator_audio_level() {
    let mut demod = AfskDemodulator::new(44100, 1200, 1200, 2200, AfskProfile::A, 0, 0);

    for i in 0..44100 {
        let t = i as f32 / 44100.0;
        let sample = (16000.0 * (2.0 * std::f32::consts::PI * 1200.0 * t).sin()) as i32;
        demod.process_sample(sample);
    }

    let (mark_peak, _space_peak) = demod.audio_level();
    assert!(mark_peak > 0.0, "mark peak should be positive after tone input");
}

#[test]
fn test_decoded_frame_metadata() {
    let frame = hdlc::DecodedFrame {
        chan: 1,
        subchan: 2,
        slice: 3,
        data: vec![0xAA, 0xBB],
        retry: RetryType::None,
        quality: 85,
        audio_level_mark: 0.5,
        audio_level_space: 0.3,
        speed_error: 0.02,
        sample_offset: 0,
    };

    assert_eq!(frame.chan, 1);
    assert_eq!(frame.subchan, 2);
    assert_eq!(frame.slice, 3);
    assert_eq!(frame.data, vec![0xAA, 0xBB]);
    assert_eq!(frame.quality, 85);
}
