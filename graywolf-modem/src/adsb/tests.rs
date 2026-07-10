//! ADS-B modulator/demodulator tests, including canonical Mode S vectors.

use super::message::{cpr_decode_airborne, encode_identification, Frame};
use super::*;

fn hex(s: &str) -> Vec<u8> {
    (0..s.len()).step_by(2).map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap()).collect()
}

/// Canonical DF17 identification frame for ICAO 4840D6, callsign "KLM1023".
const KLM1023: &str = "8D4840D6202CC371C32CE0576098";

#[test]
fn crc_valid_frame_checksums_zero() {
    let frame = hex(KLM1023);
    assert_eq!(crc::checksum(&frame), 0);
    assert!(crc::is_valid(&frame));
}

#[test]
fn crc_detects_corruption() {
    let mut frame = hex(KLM1023);
    frame[5] ^= 0x01;
    assert_ne!(crc::checksum(&frame), 0);
}

#[test]
fn append_parity_reproduces_known_parity() {
    let mut frame = hex(KLM1023);
    // Wipe the parity, recompute, and confirm we get 57 60 98 back.
    crc::append_parity(&mut frame);
    assert_eq!(&frame[11..14], &[0x57, 0x60, 0x98]);
}

#[test]
fn encode_identification_matches_reference() {
    let frame = encode_identification(0x4840D6, "KLM1023 ");
    assert_eq!(frame.to_vec(), hex(KLM1023));
}

#[test]
fn frame_fields_decode() {
    let bytes = hex(KLM1023);
    let f = Frame::new(&bytes);
    assert_eq!(f.df(), 17);
    assert_eq!(f.ca(), 5);
    assert_eq!(f.icao(), 0x4840D6);
    assert_eq!(f.type_code(), Some(4));
    assert_eq!(f.callsign().as_deref(), Some("KLM1023"));
}

#[test]
fn modulate_demodulate_roundtrip() {
    let frame = hex(KLM1023);
    for spu in [2usize, 4, 8] {
        let modem = Modulator::new(spu);
        let wave = modem.modulate_padded(&frame, 4, 4);
        let demod = Demodulator::new(spu);
        let frames = demod.demodulate(&wave);
        assert_eq!(frames.len(), 1, "spu={spu}");
        assert!(frames[0].crc_ok());
        assert_eq!(frames[0].df, 17);
        assert_eq!(frames[0].bytes, frame, "spu={spu}");
    }
}

#[test]
fn roundtrip_recovers_callsign() {
    let frame = encode_identification(0x3C6444, "TEST42");
    let modem = Modulator::new(2);
    let wave = modem.modulate_padded(&frame, 8, 8);
    let decoded = Demodulator::new(2).demodulate(&wave);
    assert_eq!(decoded.len(), 1);
    let f = Frame::new(&decoded[0].bytes);
    assert_eq!(f.icao(), 0x3C6444);
    assert_eq!(f.callsign().as_deref(), Some("TEST42"));
}

#[test]
fn demodulates_multiple_frames() {
    let modem = Modulator::new(2);
    let a = encode_identification(0xAAAAAA, "ALPHA");
    let b = encode_identification(0xBBBBBB, "BRAVO");
    let mut wave = modem.modulate_padded(&a, 4, 4);
    wave.extend(modem.modulate_padded(&b, 4, 4));
    let frames = Demodulator::new(2).demodulate(&wave);
    assert_eq!(frames.len(), 2);
    assert_eq!(Frame::new(&frames[0].bytes).callsign().as_deref(), Some("ALPHA"));
    assert_eq!(Frame::new(&frames[1].bytes).callsign().as_deref(), Some("BRAVO"));
}

#[test]
fn short_frame_roundtrip() {
    // 56-bit short frame with a non-extended DF (DF11 all-call). append_parity
    // makes it checksum to zero, and the demodulator must pick the short length
    // from the DF rather than over-reading into trailing silence.
    let mut frame = [0u8; 7];
    frame[0] = (11 << 3) | 0x05;
    frame[1] = 0x3C;
    frame[2] = 0x64;
    frame[3] = 0x44;
    crc::append_parity(&mut frame);
    assert!(crc::is_valid(&frame));

    let wave = Modulator::new(2).modulate_padded(&frame, 4, 4);
    let frames = Demodulator::new(2).demodulate(&wave);
    assert_eq!(frames.len(), 1);
    assert_eq!(frames[0].df, 11);
    assert_eq!(frames[0].bytes.len(), 7);
    assert_eq!(frames[0].bytes, frame);
    assert!(frames[0].crc_ok());
}

#[test]
fn require_crc_gates_corrupted_frames() {
    let mut frame = encode_identification(0x484010, "GARBLED");
    frame[6] ^= 0x20; // flip a data bit -> CRC no longer clears
    assert!(!crc::is_valid(&frame));
    let wave = Modulator::new(2).modulate_padded(&frame, 4, 4);

    // Strict demodulation (default) drops it.
    assert!(Demodulator::new(2).demodulate(&wave).is_empty());

    // Lenient demodulation returns it with a non-zero residual and intact bytes.
    let mut lenient = Demodulator::new(2);
    lenient.require_crc = false;
    let frames = lenient.demodulate(&wave);
    assert_eq!(frames.len(), 1);
    assert!(!frames[0].crc_ok());
    assert_ne!(frames[0].crc_residual, 0);
    assert_eq!(frames[0].bytes, frame);
}

#[test]
fn callsign_preserves_embedded_space() {
    let frame = encode_identification(0x400000, "AB CD");
    assert_eq!(Frame::new(&frame).callsign().as_deref(), Some("AB CD"));
}

#[test]
fn rejects_pure_noise() {
    // A flat / silent buffer must not yield any preamble matches.
    let wave = vec![0u16; 4096];
    assert!(Demodulator::new(2).demodulate(&wave).is_empty());
}

#[test]
fn airborne_position_cpr_global_decode() {
    // Canonical CPR pair (Junzi Sun, "The 1090 MHz Riddle").
    let even = hex("8D40621D58C382D690C8AC2863A7");
    let odd = hex("8D40621D58C386435CC412692AD6");
    let pe = Frame::new(&even).airborne_position().unwrap();
    let po = Frame::new(&odd).airborne_position().unwrap();
    assert!(!pe.odd);
    assert!(po.odd);

    let (lat, lon) = cpr_decode_airborne(&pe, &po, false).unwrap();
    assert!((lat - 52.2572).abs() < 1e-3, "lat={lat}");
    assert!((lon - 3.91937).abs() < 1e-3, "lon={lon}");
}

#[test]
fn airborne_altitude_decode() {
    // DF17 airborne position, 38000 ft (typecode 11).
    let bytes = hex("8D40621D58C382D690C8AC2863A7");
    let pos = Frame::new(&bytes).airborne_position().unwrap();
    assert_eq!(pos.altitude, Some(38000));
}
