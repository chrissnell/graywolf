//! Reed-Solomon codec for IL2P.
//!
//! Uses the same GF(2^8) field and algorithm as FX.25 RS codec.
//! Header: RS(15,13) with 2 parity symbols.
//! Payload: RS(n,k) with 16 parity symbols (shortened as needed).

use crate::fx25::rs::RsCodec;

/// IL2P uses FCR=0 (unlike FX.25 which uses FCR=1).
const IL2P_FCR: usize = 0;

/// Encode IL2P header (13 bytes) → 2 parity bytes.
pub fn rs_encode_header(data: &[u8; 13]) -> [u8; 2] {
    let rs = RsCodec::with_fcr(2, IL2P_FCR);
    let parity = rs.encode(data);
    [parity[0], parity[1]]
}

/// Decode IL2P header (15 bytes in-place). Returns true on success.
pub fn rs_decode_header(block: &mut [u8; 15]) -> bool {
    let rs = RsCodec::with_fcr(2, IL2P_FCR);
    let mut v = block.to_vec();
    match rs.decode(&mut v) {
        Some(true) => {
            block.copy_from_slice(&v);
            true
        }
        _ => false,
    }
}

/// Encode IL2P payload block → parity bytes.
pub fn rs_encode_payload(data: &[u8], ncheck: usize) -> Vec<u8> {
    let rs = RsCodec::with_fcr(ncheck, IL2P_FCR);
    rs.encode(data)
}

/// Decode IL2P payload block in-place. Returns true on success.
pub fn rs_decode_payload(block: &mut Vec<u8>, _ndata: usize, ncheck: usize) -> bool {
    let rs = RsCodec::with_fcr(ncheck, IL2P_FCR);
    matches!(rs.decode(block), Some(true))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn header_rs_roundtrip() {
        let data: [u8; 13] = [0x41, 0x50, 0x52, 0x53, 0x20, 0x20, 0x00,
                               0x4E, 0x33, 0x4C, 0x4C, 0x07, 0x2A];
        let parity = rs_encode_header(&data);
        let mut block = [0u8; 15];
        block[..13].copy_from_slice(&data);
        block[13..].copy_from_slice(&parity);

        assert!(rs_decode_header(&mut block));
        assert_eq!(&block[..13], &data);
    }

    #[test]
    fn header_rs_correct_error() {
        let data: [u8; 13] = [0x41, 0x50, 0x52, 0x53, 0x20, 0x20, 0x00,
                               0x4E, 0x33, 0x4C, 0x4C, 0x07, 0x2A];
        let parity = rs_encode_header(&data);
        let mut block = [0u8; 15];
        block[..13].copy_from_slice(&data);
        block[13..].copy_from_slice(&parity);

        // Corrupt one byte
        block[5] ^= 0xFF;
        assert!(rs_decode_header(&mut block));
        assert_eq!(&block[..13], &data);
    }

    #[test]
    fn payload_rs_roundtrip() {
        let data = vec![0xABu8; 100];
        let parity = rs_encode_payload(&data, 16);
        assert_eq!(parity.len(), 16);

        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&parity);

        assert!(rs_decode_payload(&mut block, 100, 16));
        assert_eq!(&block[..100], &data[..]);
    }

    #[test]
    fn payload_rs_correct_errors() {
        let data = vec![0x55u8; 200];
        let parity = rs_encode_payload(&data, 16);

        let mut block = Vec::new();
        block.extend_from_slice(&data);
        block.extend_from_slice(&parity);

        // Corrupt 3 bytes
        block[10] ^= 0xFF;
        block[50] ^= 0xAA;
        block[100] ^= 0x55;

        assert!(rs_decode_payload(&mut block, 200, 16));
        assert_eq!(&block[..200], &data[..]);
    }
}
