//! IL2P payload encoding/decoding with RS FEC.
//!
//! Payload is split into blocks of up to 239 bytes, each protected by
//! 16 RS parity symbols. The last block may be smaller.
//! Ported from direwolf il2p_payload.c.

use super::rs_il2p;

/// Maximum data bytes per RS block.
const MAX_BLOCK_DATA: usize = 239;
/// RS parity symbols per block.
const PARITY_PER_BLOCK: usize = 16;

/// Calculate number of RS blocks needed for a payload.
pub fn payload_block_count(payload_len: usize) -> usize {
    if payload_len == 0 {
        return 0;
    }
    (payload_len + MAX_BLOCK_DATA - 1) / MAX_BLOCK_DATA
}

/// Calculate data and parity sizes for each block.
/// Returns a Vec of (data_size, parity_size) tuples.
pub fn payload_block_sizes(payload_len: usize) -> Vec<(usize, usize)> {
    if payload_len == 0 {
        return Vec::new();
    }

    let num_blocks = payload_block_count(payload_len);
    let mut sizes = Vec::with_capacity(num_blocks);
    let mut remaining = payload_len;

    for _ in 0..num_blocks {
        let block_data = remaining.min(MAX_BLOCK_DATA);
        // Direwolf uses 16 parity symbols for ALL payload blocks (max_fec path)
        sizes.push((block_data, PARITY_PER_BLOCK));
        remaining -= block_data;
    }
    sizes
}

/// Encode payload data into RS-protected blocks.
/// Returns concatenated encoded blocks.
pub fn encode_payload(data: &[u8]) -> Vec<u8> {
    if data.is_empty() {
        return Vec::new();
    }

    let block_sizes = payload_block_sizes(data.len());
    let total_encoded: usize = block_sizes.iter().map(|&(d, p)| d + p).sum();
    let mut out = Vec::with_capacity(total_encoded);

    let mut offset = 0;
    for (data_size, parity_size) in block_sizes {
        let block_data = &data[offset..offset + data_size];
        let parity = rs_il2p::rs_encode_payload(block_data, parity_size);
        out.extend_from_slice(block_data);
        out.extend_from_slice(&parity);
        offset += data_size;
    }

    out
}

/// Decode RS-protected payload blocks.
/// Input: concatenated encoded blocks. Returns decoded payload.
pub fn decode_payload(encoded: &[u8], payload_len: usize) -> Option<Vec<u8>> {
    if payload_len == 0 {
        return Some(Vec::new());
    }

    let block_sizes = payload_block_sizes(payload_len);
    let mut decoded = Vec::with_capacity(payload_len);
    let mut offset = 0;

    for (data_size, parity_size) in block_sizes {
        let block_total = data_size + parity_size;
        if offset + block_total > encoded.len() {
            return None;
        }

        let block = &encoded[offset..offset + block_total];
        let mut block_buf = block.to_vec();

        if !rs_il2p::rs_decode_payload(&mut block_buf, data_size, parity_size) {
            return None;
        }

        decoded.extend_from_slice(&block_buf[..data_size]);
        offset += block_total;
    }

    Some(decoded)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn payload_block_count_basic() {
        assert_eq!(payload_block_count(0), 0);
        assert_eq!(payload_block_count(1), 1);
        assert_eq!(payload_block_count(239), 1);
        assert_eq!(payload_block_count(240), 2);
        assert_eq!(payload_block_count(478), 2);
        assert_eq!(payload_block_count(479), 3);
    }

    #[test]
    fn payload_encode_decode_small() {
        let data = b"Hello, IL2P payload!";
        let encoded = encode_payload(data);
        assert!(encoded.len() > data.len()); // should have parity

        let decoded = decode_payload(&encoded, data.len());
        assert!(decoded.is_some());
        assert_eq!(&decoded.unwrap(), data);
    }

    #[test]
    fn payload_encode_decode_multiblock() {
        // Payload larger than one block
        let data: Vec<u8> = (0..500).map(|i| (i & 0xFF) as u8).collect();
        let encoded = encode_payload(&data);

        let decoded = decode_payload(&encoded, data.len());
        assert!(decoded.is_some());
        assert_eq!(decoded.unwrap(), data);
    }

    #[test]
    fn payload_encode_decode_with_errors() {
        let data: Vec<u8> = vec![0xAA; 100];
        let mut encoded = encode_payload(&data);

        // Introduce a few errors (within RS correction capacity)
        if encoded.len() > 10 {
            encoded[5] ^= 0xFF;
            encoded[50] ^= 0x55;
        }

        let decoded = decode_payload(&encoded, data.len());
        assert!(decoded.is_some());
        assert_eq!(decoded.unwrap(), data);
    }
}
