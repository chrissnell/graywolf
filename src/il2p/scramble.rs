//! IL2P scrambling/descrambling.
//!
//! Ported from direwolf il2p_scramble.c (John Langner, WB2OSZ).
//! 10-bit LFSR with polynomial x^9 + x^4 + 1, MSB-first bit ordering,
//! and a 5-bit pipeline delay on transmit.

const INIT_TX_LSFR: i32 = 0x00F;
const INIT_RX_LSFR: i32 = 0x1F0;

/// Scramble one bit. Returns the output bit.
/// Direct port of direwolf's scramble_bit().
#[inline]
fn scramble_bit(input: i32, state: &mut i32) -> i32 {
    let out = ((*state >> 4) ^ *state) & 1;
    *state = ((((input ^ *state) & 1) << 9) | (*state ^ ((*state & 1) << 4))) >> 1;
    out
}

/// Descramble one bit. Returns the output bit.
/// Direct port of direwolf's descramble_bit().
#[inline]
fn descramble_bit(input: i32, state: &mut i32) -> i32 {
    let out = (input ^ *state) & 1;
    *state = ((*state >> 1) | ((input & 1) << 8)) ^ ((input & 1) << 3);
    out
}

/// Scramble a block for IL2P transmit.
/// Direct port of direwolf's il2p_scramble_block().
pub fn il2p_scramble(data: &[u8]) -> Vec<u8> {
    let len = data.len();
    let mut out = vec![0u8; len];
    let mut tx_lfsr_state = INIT_TX_LSFR;

    let mut skipping = true;
    let mut ob: usize = 0; // output byte index
    let mut om: u8 = 0x80; // output bit mask

    for ib in 0..len {
        let mut im: u8 = 0x80;
        while im != 0 {
            let s = scramble_bit(
                if data[ib] & im != 0 { 1 } else { 0 },
                &mut tx_lfsr_state,
            );
            // Stop skipping after 5 bits (byte 0, mask 0x04)
            if ib == 0 && im == 0x04 {
                skipping = false;
            }
            if !skipping {
                if s != 0 {
                    out[ob] |= om;
                }
                om >>= 1;
                if om == 0 {
                    om = 0x80;
                    ob += 1;
                }
            }
            im >>= 1;
        }
    }

    // Flush 5 remaining bits
    let mut x = tx_lfsr_state;
    for _ in 0..5 {
        let s = scramble_bit(0, &mut x);
        if s != 0 && ob < len {
            out[ob] |= om;
        }
        om >>= 1;
        if om == 0 {
            om = 0x80;
            ob += 1;
        }
    }

    out
}

/// Descramble a block for IL2P receive.
/// Direct port of direwolf's il2p_descramble_block().
pub fn il2p_descramble(data: &[u8]) -> Vec<u8> {
    let len = data.len();
    let mut out = vec![0u8; len];
    let mut rx_lfsr_state = INIT_RX_LSFR;

    for b in 0..len {
        let mut m: u8 = 0x80;
        while m != 0 {
            let d = descramble_bit(
                if data[b] & m != 0 { 1 } else { 0 },
                &mut rx_lfsr_state,
            );
            if d != 0 {
                out[b] |= m;
            }
            m >>= 1;
        }
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
        assert_ne!(&scrambled, data);
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
        let data = vec![0u8; 32];
        let scrambled = il2p_scramble(&data);
        assert!(scrambled.iter().any(|&b| b != 0));
        let recovered = il2p_descramble(&scrambled);
        assert_eq!(recovered, data);
    }
}
