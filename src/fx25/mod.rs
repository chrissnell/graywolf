//! FX.25 Forward Error Correction for AX.25.
//!
//! Implements the FX.25 specification using Reed-Solomon coding over GF(2^8)
//! with the CCSDS polynomial. Ported from direwolf's fx25_init.c / fx25_rec.c.
//!
//! FX.25 wraps an AX.25 frame in a correlation tag followed by RS-coded data.
//! The correlation tag identifies the RS code parameters (block size, check
//! symbol count). The receiver correlates against all known tags to detect
//! FX.25 frames, then applies RS error correction.

pub mod rs;
#[cfg(test)]
mod tests;

/// FX.25 correlation tags — 64-bit patterns that identify the FEC parameters.
/// Tag value → (total_block_bytes, data_bytes, check_bytes).
pub const FX25_TAGS: [(u64, usize, usize, usize); 16] = [
    (0x566ED2717946107E, 255, 239, 16),  // Tag 0x01
    (0xB74DB7DF8A532F3E, 255, 223, 32),  // Tag 0x02
    (0x26FF60A600C2FEBE, 255, 191, 64),  // Tag 0x03
    (0xC7DC0508F3D9B09E, 255, 127, 128), // Tag 0x04
    (0x8F056EB4369660EE, 255,  63, 192), // Tag 0x05
    (0x6E260B1AC5835FAE, 32,   26,   6), // Tag 0x06
    (0xFF94DC634F1CFF4E, 64,   58,   6), // Tag 0x07
    (0x1EB7B9CDBC09C00E, 128, 122,   6), // Tag 0x08
    (0xDBF869BD2DBB1776, 255, 249,   6), // Tag 0x09
    (0x3ADB0C13DEAE2836, 32,   20,  12), // Tag 0x0A
    (0xAB69DB6A543188D6, 64,   52,  12), // Tag 0x0B
    (0x4A4ABEC4A724B796, 128, 116,  12), // Tag 0x0C
    (0x0293D578626B67E6, 255, 243,  12), // Tag 0x0D
    (0xE3B0B0D6917E58A6, 255, 229,  26), // Tag 0x0E (non-standard)
    (0x720267AF1BE1F846, 255, 205,  50), // Tag 0x0F (non-standard)
    (0x93210201E8F4F306,  0,    0,   0), // Tag 0x10: undefined/reserved
];

/// Number of correlation tag bits.
pub const TAG_BITS: usize = 64;

/// Find the best matching tag given 64 received bits.
/// Returns (tag_index, hamming_distance) if distance <= max_errors.
pub fn correlate_tag(received: u64, max_errors: u32) -> Option<(usize, u32)> {
    let mut best_idx = 0;
    let mut best_dist = 65u32;

    for (i, &(tag, total, _data, _check)) in FX25_TAGS.iter().enumerate() {
        if total == 0 { continue; } // skip reserved
        let dist = (received ^ tag).count_ones();
        if dist < best_dist {
            best_dist = dist;
            best_idx = i;
        }
    }

    if best_dist <= max_errors {
        Some((best_idx, best_dist))
    } else {
        None
    }
}

/// FX.25 frame encoder.
///
/// Given raw AX.25 frame data (with FCS), selects the smallest suitable
/// FX.25 tag, pads the data, appends RS check symbols, and returns the
/// complete FX.25 frame (correlation tag + coded block).
pub fn encode(ax25_with_fcs: &[u8], tag_hint: Option<usize>) -> Option<Vec<u8>> {
    // Find smallest tag that fits
    let data_len = ax25_with_fcs.len();
    let tag_idx = if let Some(hint) = tag_hint {
        if hint < FX25_TAGS.len() && FX25_TAGS[hint].2 >= data_len {
            hint
        } else {
            find_smallest_tag(data_len)?
        }
    } else {
        find_smallest_tag(data_len)?
    };

    let (tag_val, total, data_cap, check) = FX25_TAGS[tag_idx];
    if data_cap < data_len || total == 0 {
        return None;
    }

    // Pad data to data_cap with 0x7E (flag bytes, per FX.25 spec)
    let mut padded = vec![0x7E; data_cap];
    padded[..data_len].copy_from_slice(ax25_with_fcs);

    // Compute RS check symbols
    let rs_codec = rs::RsCodec::new(check);
    let check_symbols = rs_codec.encode(&padded);

    // Build output: 8-byte tag + data + check
    let mut output = Vec::with_capacity(8 + total);
    output.extend_from_slice(&tag_val.to_be_bytes());
    output.extend_from_slice(&padded);
    output.extend_from_slice(&check_symbols);

    Some(output)
}

/// FX.25 frame decoder.
///
/// Given a coded block (after correlation tag removal), applies RS error
/// correction. Returns the corrected data portion on success (caller
/// extracts AX.25 frame from the padded data).
pub fn decode(block: &[u8], tag_idx: usize) -> Option<Vec<u8>> {
    let (_tag_val, total, data_cap, check) = FX25_TAGS[tag_idx];
    if total == 0 || block.len() < total {
        return None;
    }

    let block = &block[..total];
    let rs_codec = rs::RsCodec::new(check);

    // Attempt RS decode
    let mut corrected = block.to_vec();
    if rs_codec.decode(&mut corrected)? {
        // Extract data portion, find the AX.25 frame (strip trailing 0x7E padding)
        let data = &corrected[..data_cap];
        Some(data.to_vec())
    } else {
        None
    }
}

/// Extract the AX.25 frame from decoded FX.25 data.
/// Strips trailing 0x7E pad bytes.
pub fn extract_ax25(data: &[u8]) -> &[u8] {
    let mut end = data.len();
    while end > 0 && data[end - 1] == 0x7E {
        end -= 1;
    }
    &data[..end]
}

fn find_smallest_tag(data_len: usize) -> Option<usize> {
    let mut best_idx = None;
    let mut best_total = usize::MAX;

    for (i, &(_tag, total, data_cap, _check)) in FX25_TAGS.iter().enumerate() {
        if total == 0 { continue; }
        if data_cap >= data_len && total < best_total {
            best_total = total;
            best_idx = Some(i);
        }
    }
    best_idx
}

/// FX.25 receiver state machine.
///
/// Accumulates bits, correlates against known tags, collects the coded
/// block, and attempts RS decode. Outputs corrected AX.25 frames.
pub struct Fx25Receiver {
    // Bit shift register for tag correlation
    tag_accum: u64,
    bits_received: usize,

    // State: searching for tag or collecting block
    state: Fx25State,

    // Block collection
    block_buf: Vec<u8>,
    block_byte_acc: u8,
    block_bit_count: usize,
    block_bytes_needed: usize,
    tag_idx: usize,

    // Output
    decoded_frames: Vec<Vec<u8>>,

    // Max tag correlation errors to accept
    max_tag_errors: u32,
}

#[derive(Clone, Copy, PartialEq)]
enum Fx25State {
    SearchingTag,
    CollectingBlock,
}

impl Fx25Receiver {
    pub fn new() -> Self {
        Self {
            tag_accum: 0,
            bits_received: 0,
            state: Fx25State::SearchingTag,
            block_buf: Vec::new(),
            block_byte_acc: 0,
            block_bit_count: 0,
            block_bytes_needed: 0,
            tag_idx: 0,
            decoded_frames: Vec::new(),
            max_tag_errors: 5,
        }
    }

    /// Feed one demodulated bit.
    pub fn process_bit(&mut self, bit: bool) {
        match self.state {
            Fx25State::SearchingTag => {
                self.tag_accum = (self.tag_accum << 1) | u64::from(bit);
                self.bits_received += 1;

                if self.bits_received >= TAG_BITS {
                    if let Some((idx, _dist)) = correlate_tag(self.tag_accum, self.max_tag_errors) {
                        let (_tag, total, _data, _check) = FX25_TAGS[idx];
                        if total > 0 {
                            self.state = Fx25State::CollectingBlock;
                            self.block_buf.clear();
                            self.block_byte_acc = 0;
                            self.block_bit_count = 0;
                            self.block_bytes_needed = total;
                            self.tag_idx = idx;
                        }
                    }
                }
            }
            Fx25State::CollectingBlock => {
                // Accumulate bits LSB first into bytes
                if bit {
                    self.block_byte_acc |= 1 << (self.block_bit_count & 7);
                }
                self.block_bit_count += 1;

                if (self.block_bit_count & 7) == 0 {
                    self.block_buf.push(self.block_byte_acc);
                    self.block_byte_acc = 0;

                    if self.block_buf.len() >= self.block_bytes_needed {
                        // Attempt decode
                        if let Some(data) = decode(&self.block_buf, self.tag_idx) {
                            let ax25 = extract_ax25(&data);
                            if ax25.len() >= 15 {
                                self.decoded_frames.push(ax25.to_vec());
                            }
                        }
                        self.state = Fx25State::SearchingTag;
                        self.bits_received = 0;
                        self.tag_accum = 0;
                    }
                }
            }
        }
    }

    /// Take decoded frames (AX.25 data with FCS).
    pub fn take_frames(&mut self) -> Vec<Vec<u8>> {
        std::mem::take(&mut self.decoded_frames)
    }

    /// Reset state.
    pub fn reset(&mut self) {
        self.state = Fx25State::SearchingTag;
        self.tag_accum = 0;
        self.bits_received = 0;
        self.block_buf.clear();
    }
}
