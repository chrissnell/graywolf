//! AX.25 HDLC bit stream encoder for the TX path.
//!
//! Builds the complete NRZI line-state bit stream for a full transmission —
//! `[preamble flags] [frame bytes + FCS] [postamble flags]` — in a single
//! pass so the NRZI line state is continuous across every section. Encoding
//! the three sections independently and concatenating their outputs would
//! insert a spurious transition at the frame→postamble boundary whenever
//! the frame has an odd number of data-`0` bits, which is enough to make
//! the receiver lose sync. See the `tests` module below for a regression
//! test that pins this behaviour.

use crate::hdlc::{crc16_finalize, crc16_step};

const FLAG_BYTE: u8 = 0x7e;

/// Encode one full transmission into a stream of NRZI line-state bits.
///
/// `frame_bytes` is the AX.25 frame **without** its FCS. The encoder computes
/// the CRC-16/X.25 FCS and appends it (low byte then high byte) before
/// bit-stuffing and NRZI-encoding the run. `preamble_flags` and
/// `postamble_flags` are counts of `0x7e` flag bytes to bracket the frame
/// with; flags themselves are never bit-stuffed, since their six-in-a-row
/// `1` bits are the pattern the receiver synchronises on.
///
/// The returned `Vec<u8>` holds one bit per element (0 or 1), ready to feed
/// directly to the AFSK modulator.
pub fn encode(frame_bytes: &[u8], preamble_flags: usize, postamble_flags: usize) -> Vec<u8> {
    let estimated = (preamble_flags + postamble_flags) * 8 + (frame_bytes.len() + 2) * 10;
    let mut enc = HdlcEncoder::with_capacity(estimated);
    enc.push_flags(preamble_flags);
    enc.push_frame(frame_bytes);
    enc.push_flags(postamble_flags);
    enc.into_bits()
}

/// Streaming encoder that carries the NRZI line state across every section
/// of a transmission.
struct HdlcEncoder {
    bits: Vec<u8>,
    /// Current NRZI line state (0 or 1). Persists across every section —
    /// never reset by [`push_flags`] or [`push_frame`].
    nrzi: u8,
    /// Consecutive `1` data bits emitted inside the frame body. Used only
    /// to drive bit stuffing; reset at each flag byte, and never allowed
    /// to count the stuffed `0` itself.
    ones_run: u8,
}

impl HdlcEncoder {
    fn with_capacity(cap: usize) -> Self {
        Self {
            bits: Vec::with_capacity(cap),
            nrzi: 0,
            ones_run: 0,
        }
    }

    /// Append `n` flag bytes (`0x7e`). Flags are never bit-stuffed.
    fn push_flags(&mut self, n: usize) {
        for _ in 0..n {
            // Direwolf resets the stuff counter at the start of each flag
            // byte but keeps the NRZI accumulator running.
            self.ones_run = 0;
            for shift in 0..8 {
                let bit = (FLAG_BYTE >> shift) & 1;
                self.push_line_bit(bit);
            }
        }
        self.ones_run = 0;
    }

    /// Append a frame body: LSB-first bit-stuffed encoding of each byte,
    /// followed by the CRC-16/X.25 FCS (low byte, then high byte), also
    /// bit-stuffed.
    fn push_frame(&mut self, frame_bytes: &[u8]) {
        let mut crc: u16 = 0xffff;
        for &byte in frame_bytes {
            crc = crc16_step(crc, byte);
            self.push_frame_byte(byte);
        }
        let fcs = crc16_finalize(crc);
        self.push_frame_byte((fcs & 0xff) as u8);
        self.push_frame_byte((fcs >> 8) as u8);
    }

    fn into_bits(self) -> Vec<u8> {
        self.bits
    }

    fn push_frame_byte(&mut self, byte: u8) {
        for shift in 0..8 {
            let bit = (byte >> shift) & 1;
            self.push_stuffed_bit(bit);
        }
    }

    fn push_stuffed_bit(&mut self, bit: u8) {
        self.push_line_bit(bit);
        if bit == 1 {
            self.ones_run += 1;
            if self.ones_run == 5 {
                self.push_line_bit(0);
                self.ones_run = 0;
            }
        } else {
            self.ones_run = 0;
        }
    }

    fn push_line_bit(&mut self, data_bit: u8) {
        // NRZI: a data `0` toggles the line, a data `1` holds it.
        if data_bit == 0 {
            self.nrzi ^= 1;
        }
        self.bits.push(self.nrzi);
    }
}

#[cfg(test)]
mod tests {
    use super::HdlcEncoder;
    use super::*;
    use crate::hdlc::HdlcDecoder;

    /// Feed a bit stream through the receiver's HDLC decoder and collect
    /// every frame it emits.
    fn decode_bits(bits: &[u8]) -> Vec<Vec<u8>> {
        let mut decoder = HdlcDecoder::new(0, 0, 0, false);
        let mut nudge: i64 = 0;
        let mut symbols: i32 = 0;
        let mut out = Vec::new();
        for &b in bits {
            if let Some(frame) = decoder.process_bit(b != 0, &mut nudge, &mut symbols) {
                out.push(frame.data);
            }
        }
        out
    }

    /// Deterministic xorshift32 PRNG — avoids pulling `rand` as a dev-dep
    /// just to generate fuzz input for a round-trip test.
    fn xorshift(state: &mut u32) -> u32 {
        let mut x = *state;
        x ^= x << 13;
        x ^= x >> 17;
        x ^= x << 5;
        *state = x;
        x
    }

    fn random_frame(rng: &mut u32, min_len: usize, max_len: usize) -> Vec<u8> {
        let span = (max_len - min_len + 1) as u32;
        let len = min_len + (xorshift(rng) % span) as usize;
        (0..len).map(|_| xorshift(rng) as u8).collect()
    }

    #[test]
    fn encode_round_trips_one_hundred_random_frames() {
        let mut rng: u32 = 0x1234_5678;
        for _ in 0..100 {
            let frame = random_frame(&mut rng, 14, 330);
            let bits = encode(&frame, 8, 4);
            let decoded = decode_bits(&bits);
            assert!(
                decoded.iter().any(|f| f == &frame),
                "frame of len {} did not round-trip",
                frame.len()
            );
        }
    }

    #[test]
    fn literal_flag_byte_in_frame_round_trips() {
        let frame = vec![
            0x82, 0x40, 0x40, 0x40, 0x40, 0x40, 0x60, 0x96, 0xae, 0x6e, 0x8c, 0x96, 0xe2, 0x61,
            0x03, 0xf0, 0x7e, 0x7e, 0x7e,
        ];
        let bits = encode(&frame, 8, 4);
        let decoded = decode_bits(&bits);
        assert!(decoded.iter().any(|f| f == &frame));
    }

    #[test]
    fn run_of_six_one_bits_in_frame_round_trips() {
        // 0xfc LSB-first is 0,0,1,1,1,1,1,1 — six consecutive 1 bits,
        // which without stuffing would be indistinguishable from a flag.
        let frame = vec![
            0x82, 0x40, 0x40, 0x40, 0x40, 0x40, 0x60, 0x96, 0xae, 0x6e, 0x8c, 0x96, 0xe2, 0x61,
            0x03, 0xf0, 0xfc, 0xfc, 0xff,
        ];
        let bits = encode(&frame, 8, 4);
        let decoded = decode_bits(&bits);
        assert!(decoded.iter().any(|f| f == &frame));
    }

    #[test]
    fn frame_whose_fcs_contains_flag_byte_round_trips() {
        // Search for a frame whose FCS contains 0x7e in at least one byte,
        // so bit stuffing has to work inside the FCS as well as the body.
        let mut rng: u32 = 0xdead_beef;
        for _ in 0..4096 {
            let frame = random_frame(&mut rng, 17, 40);
            let mut crc: u16 = 0xffff;
            for &b in &frame {
                crc = crc16_step(crc, b);
            }
            let fcs = crc16_finalize(crc);
            if (fcs & 0xff) as u8 == 0x7e || (fcs >> 8) as u8 == 0x7e {
                let bits = encode(&frame, 8, 4);
                let decoded = decode_bits(&bits);
                assert!(
                    decoded.iter().any(|f| f == &frame),
                    "frame with 0x7e in FCS failed to round-trip"
                );
                return;
            }
        }
        panic!("no frame with 0x7e in FCS found in 4096 trials");
    }

    #[test]
    fn first_frame_data_bit_is_the_byte_lsb() {
        // 0x03 LSB-first is 1,1,0,0,0,0,0,0 — distinguishable from the
        // MSB-first ordering 0,0,0,0,0,0,1,1.
        let bits = encode(&[0x03], 0, 0);
        let mut prev: u8 = 0;
        let mut data = Vec::with_capacity(8);
        for &line in &bits[..8] {
            data.push(if line == prev { 1 } else { 0 });
            prev = line;
        }
        assert_eq!(data, vec![1, 1, 0, 0, 0, 0, 0, 0]);
    }

    /// Build a `[preamble][frame][postamble]` bit stream by encoding each
    /// section with its own fresh encoder, then concatenating the outputs.
    /// This is exactly the bug the single-pass API exists to prevent.
    fn concat_sections_independently(
        frame: &[u8],
        preamble_flags: usize,
        postamble_flags: usize,
    ) -> Vec<u8> {
        let mut a = HdlcEncoder::with_capacity(preamble_flags * 8);
        a.push_flags(preamble_flags);
        let mut b = HdlcEncoder::with_capacity(frame.len() * 10);
        b.push_frame(frame);
        let mut c = HdlcEncoder::with_capacity(postamble_flags * 8);
        c.push_flags(postamble_flags);
        a.into_bits()
            .into_iter()
            .chain(b.into_bits())
            .chain(c.into_bits())
            .collect()
    }

    #[test]
    fn independently_encoding_sections_and_concatenating_breaks_nrzi_continuity() {
        // Find a frame whose final NRZI state is not 0 — that's when the
        // single-pass and concatenated encodings produce different line
        // states at the frame→postamble boundary.
        let base = vec![
            0x82, 0x40, 0x40, 0x40, 0x40, 0x40, 0x60, 0x96, 0xae, 0x6e, 0x8c, 0x96, 0xe2, 0x61,
            0x03, 0xf0,
        ];
        let mut found = None;
        for tail in 0u8..=255 {
            let mut frame = base.clone();
            frame.push(tail);
            let single = encode(&frame, 50, 20);
            let concat = concat_sections_independently(&frame, 50, 20);
            if single != concat {
                found = Some((frame, single, concat));
                break;
            }
        }
        let (frame, single, concat) = found.expect("no frame tail exercises the NRZI boundary");

        let decoded_single = decode_bits(&single);
        assert!(
            decoded_single.iter().any(|f| f == &frame),
            "single-pass encode failed to round-trip"
        );

        let decoded_concat = decode_bits(&concat);
        assert!(
            !decoded_concat.iter().any(|f| f == &frame),
            "concatenated encode unexpectedly round-tripped — NRZI boundary \
             not actually broken, test premise invalid"
        );
    }
}
