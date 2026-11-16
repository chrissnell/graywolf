use super::*;

fn frame() -> Vec<u8> {
    vec![
        0x82, 0xa0, 0xa4, 0xa6, 0x40, 0x40, 0x60, 0x96, 0xa6, 0x72, 0x9c, 0x40, 0x40, 0x61, 3,
        0xf0, 0x00, 0xff, 0x7e, 0x7e, 0xff, 0x00, 0x7e,
    ]
}
fn receive(wire: &[u8]) -> Vec<RecoveredFrame> {
    let mut rx = Fx25Receiver::new();
    wire.iter()
        .flat_map(|b| (0..8).map(move |s| b & (1 << s) != 0))
        .filter_map(|b| rx.process_bit(b))
        .collect()
}
#[test]
fn standard_tags_and_reserved_rejection() {
    for (i, &(tag, total, _, _)) in FX25_TAGS.iter().enumerate() {
        assert_eq!(
            correlate_tag(tag, 0).map(|r| r.0),
            if total == 0 { None } else { Some(i) }
        );
        if total > 0 {
            assert_eq!(correlate_tag(tag ^ 0xff, 8), Some((i, 8)));
        }
    }
    assert!(decode(&[], 16).is_none());
    assert!(encode(&frame(), Some(0)).is_none());
    assert!(encode(&frame(), Some(99)).is_none());
}
#[test]
fn all_formats_correct_at_symbol_limit_and_preserve_endings() {
    for idx in 1..=11 {
        let original = encode(&frame(), Some(idx)).unwrap();
        assert_eq!(receive(&original)[0].frame.data, frame());
        let mut damaged = original.clone();
        for n in 0..FX25_TAGS[idx].3 / 2 {
            damaged[8 + n] ^= (n + 1) as u8;
        }
        assert_eq!(receive(&damaged)[0].frame.data, frame(), "tag {idx}");
        for n in 0..FX25_TAGS[idx].3 / 2 + 8 {
            damaged[8 + n] ^= 0xa5;
        }
        assert!(receive(&damaged).is_empty(), "tag {idx}");
    }
}
#[test]
fn bad_fcs_even_with_valid_rs_is_rejected() {
    let wire = encode(&frame(), Some(1)).unwrap();
    let mut data = decode(&wire[8..], 1).unwrap();
    data[5] ^= 1; // re-encode bad HDLC contents under valid RS
    let mut bad = wire[..8].to_vec();
    bad.extend_from_slice(&data);
    bad.extend_from_slice(&codec(16).encode(&data));
    assert!(receive(&bad).is_empty());
}
#[test]
fn reset_partial_noise_and_back_to_back() {
    let wire = encode(&frame(), Some(1)).unwrap();
    let mut rx = Fx25Receiver::new();
    for b in wire[..30]
        .iter()
        .flat_map(|b| (0..8).map(move |s| b & (1 << s) != 0))
    {
        rx.process_bit(b);
    }
    rx.reset();
    let mut stream = vec![0x55; 500];
    stream.extend_from_slice(&wire[..30]);
    stream.extend_from_slice(&wire);
    stream.extend_from_slice(&wire);
    assert_eq!(receive(&stream).len(), 2);
    for _ in 0..100_000 {
        rx.process_bit(false);
    }
    assert!(rx.block.len() <= 255);
}
#[test]
fn lengths_include_stuffing_and_never_fallback() {
    assert!(encode(&vec![0; 235], None).is_some());
    assert!(encode(&vec![0; 236], None).is_none());
    assert!(encode(&vec![255; 235], None).is_none());
    for idx in 1..=11 {
        let mut maximum = 0;
        for len in 15..=235 {
            if let Some(wire) = encode(&vec![0; len], Some(idx)) {
                maximum = len;
                assert_eq!(receive(&wire)[0].frame.data, vec![0; len]);
            }
        }
        assert!(maximum > 15);
        assert!(encode(&vec![0; maximum + 1], Some(idx)).is_none());
    }
}

#[test]
fn independent_direwolf_wire_vectors() {
    let fixture: serde_json::Value =
        serde_json::from_str(include_str!("../../testdata/fx25/direwolf.json")).unwrap();
    for v in fixture["wire_vectors"].as_array().unwrap() {
        fn bytes(s: &str) -> Vec<u8> {
            (0..s.len())
                .step_by(2)
                .map(|i| u8::from_str_radix(&s[i..i + 2], 16).unwrap())
                .collect()
        }
        let frame = bytes(v["frame"].as_str().unwrap());
        let wire = bytes(v["wire"].as_str().unwrap());
        assert_eq!(
            encode(&frame, Some(v["tag"].as_u64().unwrap() as usize)).unwrap(),
            wire
        );
        assert_eq!(receive(&wire)[0].frame.data, frame);
    }
}

#[test]
fn deterministic_noise_and_reserved_streams_do_not_deliver() {
    let mut rx = Fx25Receiver::new();
    let mut rng = 0x12345678u32;
    for _ in 0..1_000_000 {
        rng ^= rng << 13;
        rng ^= rng >> 17;
        rng ^= rng << 5;
        assert!(rx.process_bit(rng & 1 != 0).is_none());
    }
    for idx in [0, 12, 13, 14, 15] {
        let mut wire = FX25_TAGS[idx].0.to_le_bytes().to_vec();
        wire.extend_from_slice(&[0; 255]);
        assert!(receive(&wire).is_empty());
    }
}

#[test]
fn metadata_counts_data_and_parity_corrections() {
    for tag in 1..=11 {
        let mut wire = encode(&frame(), Some(tag)).unwrap();
        let clean = receive(&wire).remove(0);
        assert_eq!(clean.frame.fx25.as_ref().unwrap().corrected_symbols, 0);
        wire[8 + 10] ^= 0x55;
        let end = wire.len() - 1;
        wire[end] ^= 0xaa;
        let repaired = receive(&wire).remove(0);
        assert_eq!(repaired.frame.fx25.as_ref().unwrap().corrected_symbols, 2);
    }
}
#[test]
fn fixed_parity_selects_standard_modes_and_rejects_overflow() {
    for parity in [16, 32, 64] {
        let wire = encode_with_parity(&frame(), parity).unwrap();
        assert_eq!(
            receive(&wire)[0].frame.fx25.as_ref().unwrap().parity_bytes,
            parity
        );
    }
    assert!(encode_with_parity(&frame(), 8).is_none());
    assert!(encode_with_parity(&vec![0; 220], 64).is_none());
    assert!(encode_with_parity(&vec![0; 220], 32).is_none());
    assert!(encode_with_parity(&vec![0; 220], 16).is_some());
}
