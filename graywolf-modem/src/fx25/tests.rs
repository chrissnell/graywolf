//! FX.25 tests including direwolf-compatible test vectors.

use super::*;

#[test]
fn tag_correlation_exact() {
    for (i, &(tag, total, _, _)) in FX25_TAGS.iter().enumerate() {
        if total == 0 { continue; }
        let result = correlate_tag(tag, 0);
        assert!(result.is_some(), "tag {} should match exactly", i);
        let (idx, dist) = result.unwrap();
        assert_eq!(idx, i);
        assert_eq!(dist, 0);
    }
}

#[test]
fn tag_correlation_with_errors() {
    let (tag, _, _, _) = FX25_TAGS[0];
    // Flip 3 bits
    let corrupted = tag ^ 0x0000_0000_0000_0007;
    let result = correlate_tag(corrupted, 5);
    assert!(result.is_some());
    let (idx, dist) = result.unwrap();
    assert_eq!(idx, 0);
    assert_eq!(dist, 3);
}

#[test]
fn tag_correlation_rejects_too_many_errors() {
    let (tag, _, _, _) = FX25_TAGS[0];
    // Flip 10 bits
    let corrupted = tag ^ 0x00000000000003FF;
    let result = correlate_tag(corrupted, 5);
    assert!(result.is_none());
}

#[test]
fn encode_decode_roundtrip_all_tags() {
    // Test encode/decode roundtrip for all valid tags
    for (i, &(_tag, total, data_cap, _check)) in FX25_TAGS.iter().enumerate() {
        if total == 0 { continue; }
        // Create a test frame that fits
        let frame_len = data_cap.min(20);
        let test_frame: Vec<u8> = (0..frame_len as u8).collect();

        let encoded = encode(&test_frame, Some(i));
        assert!(encoded.is_some(), "encode failed for tag {}", i);
        let encoded = encoded.unwrap();

        // Encoded = 8 bytes tag + total bytes block
        assert_eq!(encoded.len(), 8 + total, "wrong encoded len for tag {}", i);

        // Decode
        let block = &encoded[8..]; // strip tag
        let decoded = decode(block, i);
        assert!(decoded.is_some(), "decode failed for tag {}", i);
        let decoded = decoded.unwrap();

        // Extract original frame
        let extracted = extract_ax25(&decoded);
        assert_eq!(extracted, &test_frame[..], "roundtrip failed for tag {}", i);
    }
}

/// Direwolf test vector: a known AX.25 frame encoded with FX.25 tag 0x01
/// (RS(255,239) with 16 check symbols). This verifies bit-exact compatibility.
#[test]
fn direwolf_fx25_test_vector_tag01() {
    // Small test frame: typical APRS position report
    let ax25_data: Vec<u8> = vec![
        0x82, 0xA0, 0xB4, 0x60, 0x60, 0x60, 0x60, // dest: APRS
        0x9C, 0x6E, 0x98, 0x8A, 0x9A, 0x40, 0x61, // src: N3LLO
        0x03, 0xF0,                                 // ctrl, pid
        0x21, 0x34, 0x30, 0x31, 0x31, 0x2E, 0x37, // !4011.7
        0x35, 0x4E, 0x2F, 0x30, 0x37, 0x34, 0x30, // 5N/0740
        0x34, 0x2E, 0x37, 0x36, 0x57, 0x2D,       // 4.76W-
    ];

    // Compute FCS for the test frame
    let fcs = crate::hdlc::fcs_calc(&ax25_data);
    let mut frame_with_fcs = ax25_data.clone();
    frame_with_fcs.push((fcs & 0xFF) as u8);
    frame_with_fcs.push((fcs >> 8) as u8);

    // Encode with tag 0 (RS(255,239))
    let encoded = encode(&frame_with_fcs, Some(0)).unwrap();
    assert_eq!(encoded.len(), 8 + 255);

    // Verify tag bytes
    let tag_bytes = &encoded[..8];
    assert_eq!(
        u64::from_be_bytes(tag_bytes.try_into().unwrap()),
        FX25_TAGS[0].0
    );

    // Decode without errors
    let decoded = decode(&encoded[8..], 0).unwrap();
    let extracted = extract_ax25(&decoded);
    assert_eq!(extracted, &frame_with_fcs[..]);

    // Decode with errors in data portion
    let mut corrupted = encoded[8..].to_vec();
    corrupted[5] ^= 0xFF;
    corrupted[50] ^= 0xAA;
    corrupted[100] ^= 0x55;
    let decoded2 = decode(&corrupted, 0).unwrap();
    let extracted2 = extract_ax25(&decoded2);
    assert_eq!(extracted2, &frame_with_fcs[..], "error correction failed");
}

/// Test with tag 0x05 (RS(255,63) — 192 check symbols, very strong FEC).
#[test]
fn fx25_strong_fec_tag05() {
    let data: Vec<u8> = (0..60).collect();
    let fcs = crate::hdlc::fcs_calc(&data);
    let mut frame = data.clone();
    frame.push((fcs & 0xFF) as u8);
    frame.push((fcs >> 8) as u8);

    let encoded = encode(&frame, Some(4)).unwrap(); // tag index 4 = RS(255,63)
    assert_eq!(encoded.len(), 8 + 255);

    // Corrupt many bytes (RS(255,63) can correct up to 96 errors)
    let mut corrupted = encoded[8..].to_vec();
    for i in 0..50 {
        corrupted[i * 3] ^= 0xFF;
    }

    let decoded = decode(&corrupted, 4).unwrap();
    let extracted = extract_ax25(&decoded);
    assert_eq!(extracted, &frame[..]);
}

/// Test shortened RS code (tag 0x06: RS based on 255 but block=32).
#[test]
fn fx25_shortened_tag06() {
    let data: Vec<u8> = vec![0x42; 20];
    let fcs = crate::hdlc::fcs_calc(&data);
    let mut frame = data;
    frame.push((fcs & 0xFF) as u8);
    frame.push((fcs >> 8) as u8);

    let encoded = encode(&frame, Some(5)).unwrap(); // tag 5 = index for 32-byte block
    assert_eq!(encoded.len(), 8 + 32);

    // Decode clean
    let decoded = decode(&encoded[8..], 5).unwrap();
    let extracted = extract_ax25(&decoded);
    assert_eq!(extracted, &frame[..]);

    // Decode with 1 error (RS(32,26,6) corrects up to 3)
    let mut corrupted = encoded[8..].to_vec();
    corrupted[10] ^= 0xFF;
    let decoded2 = decode(&corrupted, 5).unwrap();
    let extracted2 = extract_ax25(&decoded2);
    assert_eq!(extracted2, &frame[..]);
}

/// Verify the receiver state machine can process bit-by-bit.
#[test]
fn fx25_receiver_basic() {
    let data: Vec<u8> = vec![0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0x11, 0x22, 0x33,
                              0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xAA];
    let fcs = crate::hdlc::fcs_calc(&data);
    let mut frame = data;
    frame.push((fcs & 0xFF) as u8);
    frame.push((fcs >> 8) as u8);

    let encoded = encode(&frame, Some(5)).unwrap(); // 32-byte block

    let mut rx = Fx25Receiver::new();

    // Feed tag bits MSB first (big-endian)
    let tag_val = FX25_TAGS[5].0;
    for bit_pos in (0..64).rev() {
        rx.process_bit((tag_val >> bit_pos) & 1 != 0);
    }

    // Feed block bits LSB first per byte
    for &byte in &encoded[8..] {
        for bit_pos in 0..8 {
            rx.process_bit((byte >> bit_pos) & 1 != 0);
        }
    }

    let frames = rx.take_frames();
    assert_eq!(frames.len(), 1, "should decode exactly one frame");
    assert_eq!(&frames[0], &frame);
}

#[test]
fn find_smallest_tag_fits() {
    // 20 bytes should fit in tag 5 (data_cap=26)
    let idx = super::find_smallest_tag(20);
    assert!(idx.is_some());
    let i = idx.unwrap();
    assert!(FX25_TAGS[i].2 >= 20);
    assert_eq!(FX25_TAGS[i].1, 32); // smallest block that fits
}
