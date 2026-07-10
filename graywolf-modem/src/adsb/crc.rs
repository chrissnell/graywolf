//! Mode S / ADS-B 24-bit CRC.
//!
//! Every Mode S downlink frame carries a 24-bit parity field in its trailing
//! three bytes. The parity is the remainder of the message polynomial divided
//! by the Mode S generator polynomial `0xFFF409` (ICAO Annex 10, Vol IV). For
//! extended squitter (DF17 / DF18) the parity has no interrogator overlay, so a
//! correctly received frame checksums to zero over all of its bytes.
//!
//! The table-driven byte-wise routine here matches dump1090's `modescrc`.

/// Mode S CRC generator polynomial (implicit `x^24` term omitted).
pub const GENERATOR_POLY: u32 = 0x00FF_F409;

/// Number of parity bits appended to every Mode S frame.
pub const PARITY_BITS: usize = 24;

/// Precomputed byte lookup table for the Mode S CRC.
static CRC_TABLE: [u32; 256] = build_table();

const fn build_table() -> [u32; 256] {
    let mut table = [0u32; 256];
    let mut i = 0;
    while i < 256 {
        let mut c = (i as u32) << 16;
        let mut j = 0;
        while j < 8 {
            if c & 0x0080_0000 != 0 {
                c = (c << 1) ^ GENERATOR_POLY;
            } else {
                c <<= 1;
            }
            j += 1;
        }
        table[i] = c & 0x00FF_FFFF;
        i += 1;
    }
    table
}

/// Compute the 24-bit Mode S checksum over `msg`.
///
/// A valid extended-squitter frame (with parity in the last three bytes)
/// returns `0`. The routine reduces `msg(x) * x^24 mod g(x)`, so the checksum
/// of just the data bytes (parity omitted) is exactly the parity to append.
pub fn checksum(msg: &[u8]) -> u32 {
    let mut rem: u32 = 0;
    for &b in msg {
        let idx = (b ^ ((rem >> 16) as u8)) as usize;
        rem = ((rem << 8) ^ CRC_TABLE[idx]) & 0x00FF_FFFF;
    }
    rem
}

/// Write the correct parity into the trailing three bytes of `frame`.
///
/// The parity is `data(x) * x^24 mod g(x)` — the checksum of the data bytes
/// alone — which makes the completed frame checksum to zero. `frame` must be
/// 7 or 14 bytes.
pub fn append_parity(frame: &mut [u8]) {
    let n = frame.len();
    debug_assert!(n >= 3, "frame too short for parity");
    let p = checksum(&frame[..n - 3]);
    frame[n - 3] = (p >> 16) as u8;
    frame[n - 2] = (p >> 8) as u8;
    frame[n - 1] = p as u8;
}

/// True when `frame` carries valid extended-squitter parity (checksum zero).
pub fn is_valid(frame: &[u8]) -> bool {
    checksum(frame) == 0
}
