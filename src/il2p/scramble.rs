//! IL2P scrambling/descrambling.
//!
//! IL2P uses a synchronous scrambler with polynomial x^9 + x^4 + 1
//! for DC balance. The same scrambler is used for both header and payload.
//! Ported from direwolf il2p_scramble.c.

/// IL2P scrambler: x^9 + x^4 + 1.
/// Operates byte-at-a-time for efficiency.
pub fn il2p_scramble(data: &[u8]) -> Vec<u8> {
    let mut state: u16 = 0x1FF; // initial state: all ones (9 bits)
    let mut out = Vec::with_capacity(data.len());

    for &byte in data {
        let mut scrambled = 0u8;
        for bit_pos in 0..8 {
            let input_bit = (byte >> bit_pos) & 1;
            // Feedback taps: bit 8 (x^9) and bit 3 (x^4)
            let feedback = ((state >> 8) ^ (state >> 3)) & 1;
            let output_bit = input_bit ^ feedback as u8;
            scrambled |= output_bit << bit_pos;
            state = ((state << 1) | output_bit as u16) & 0x1FF;
        }
        out.push(scrambled);
    }
    out
}

/// IL2P descrambler: inverse of scramble.
pub fn il2p_descramble(data: &[u8]) -> Vec<u8> {
    let mut state: u16 = 0x1FF;
    let mut out = Vec::with_capacity(data.len());

    for &byte in data {
        let mut descrambled = 0u8;
        for bit_pos in 0..8 {
            let input_bit = (byte >> bit_pos) & 1;
            let feedback = ((state >> 8) ^ (state >> 3)) & 1;
            let output_bit = input_bit ^ feedback as u8;
            descrambled |= output_bit << bit_pos;
            state = ((state << 1) | input_bit as u16) & 0x1FF;
        }
        out.push(descrambled);
    }
    out
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn scramble_descramble_roundtrip() {
        let data = b"Hello IL2P scrambler test!";
        let scrambled = il2p_scramble(data);
        assert_ne!(&scrambled, data); // should be different
        let recovered = il2p_descramble(&scrambled);
        assert_eq!(&recovered, data);
    }

    #[test]
    fn scramble_empty() {
        let data: &[u8] = &[];
        let scrambled = il2p_scramble(data);
        assert!(scrambled.is_empty());
    }

    #[test]
    fn scramble_known_pattern() {
        // All zeros through the scrambler should produce a non-trivial pattern
        // (the LFSR initial state generates a pseudo-random sequence)
        let data = vec![0u8; 32];
        let scrambled = il2p_scramble(&data);
        // At least some bytes should be non-zero
        assert!(scrambled.iter().any(|&b| b != 0));
        let recovered = il2p_descramble(&scrambled);
        assert_eq!(recovered, data);
    }
}
