//! Standard FX.25 encapsulation (1200-baud Bell 202 integration only).
//!
//! Wire parameters: FX.25 draft 0.01 (2006-09-01) and Dire Wolf
//! eda1383f5fa9d8ba3cb27f99db1d2c79494404c9, fx25_{init,send,rec}.c.
//! See docs/fx25.md for provenance, limits and independent tests.
pub mod rs;
#[cfg(test)]
mod tests;

use crate::hdlc::{DecodedFrame, HdlcDecoder};
use std::sync::OnceLock;

/// Indexed by the on-air tag number. Reserved tags have zero capacities.
/// (tag, transmitted block bytes, transmitted data bytes, parity bytes).
pub const FX25_TAGS: [(u64, usize, usize, usize); 16] = [
    (0x566ED2717946107E, 0, 0, 0),
    (0xB74DB7DF8A532F3E, 255, 239, 16),
    (0x26FF60A600CC8FDE, 144, 128, 16),
    (0xC7DC0508F3D9B09E, 80, 64, 16),
    (0x8F056EB4369660EE, 48, 32, 16),
    (0x6E260B1AC5835FAE, 255, 223, 32),
    (0xFF94DC634F1CFF4E, 160, 128, 32),
    (0x1EB7B9CDBC09C00E, 96, 64, 32),
    (0xDBF869BD2DBB1776, 64, 32, 32),
    (0x3ADB0C13DEAE2836, 255, 191, 64),
    (0xAB69DB6A543188D6, 192, 128, 64),
    (0x4A4ABEC4A724B796, 128, 64, 64),
    (0x0293D578626B67E6, 0, 0, 0),
    (0xE3B0B0D6917E58A6, 0, 0, 0),
    (0x720267AF1BE1F846, 0, 0, 0),
    (0x93210201E8F4C706, 0, 0, 0),
];
pub const TAG_BITS: usize = 64;

fn codec(check: usize) -> &'static rs::RsCodec {
    static CODECS: OnceLock<[rs::RsCodec; 3]> = OnceLock::new();
    let codecs = CODECS.get_or_init(|| {
        [
            rs::RsCodec::new(16),
            rs::RsCodec::new(32),
            rs::RsCodec::new(64),
        ]
    });
    &codecs[match check {
        16 => 0,
        32 => 1,
        64 => 2,
        _ => unreachable!(),
    }]
}

pub fn correlate_tag(received: u64, max_errors: u32) -> Option<(usize, u32)> {
    // Tags are distance 32 apart. Never accept an ambiguous radius.
    FX25_TAGS
        .iter()
        .enumerate()
        .filter(|(_, t)| t.1 != 0)
        .map(|(i, t)| (i, (received ^ t.0).count_ones()))
        .find(|&(_, d)| d <= max_errors.min(8))
}

/// Encode raw AX.25 WITHOUT FCS. Default: Dire Wolf automatic standard-format selection.
/// A forced tag must fit exactly; there is no fallback or truncation.
pub fn encode(frame: &[u8], tag_hint: Option<usize>) -> Option<Vec<u8>> {
    if frame.len() < 15 || frame.len() > 235 {
        return None;
    }
    // Reuse the ordinary HDLC encoder for framing, FCS and stuffing, then
    // undo NRZI. The protected block contains DATA bits, not line states.
    let lines = crate::tx::hdlc_encode::encode(frame, 1, 1);
    let mut previous = 0;
    let mut bits: Vec<u8> = lines
        .into_iter()
        .map(|line| {
            let bit = u8::from(line == previous);
            previous = line;
            bit
        })
        .collect();
    let needed = bits.len().div_ceil(8);
    let idx = match tag_hint {
        Some(i) => i,
        None => [4, 3, 6, 9, 5, 1]
            .into_iter()
            .find(|&i| FX25_TAGS[i].2 >= needed)?,
    };
    let &(tag, total, cap, check) = FX25_TAGS.get(idx)?;
    if total == 0 || needed > cap {
        return None;
    }
    // Continue the closing flag pattern at its original bit phase.
    let mut shift = 0;
    while bits.len() < cap * 8 {
        bits.push((0x7e >> shift) & 1);
        shift = (shift + 1) & 7;
    }
    let mut data = vec![0; 255 - check];
    for (n, bit) in bits.into_iter().enumerate() {
        data[n / 8] |= bit << (n % 8);
    }
    // FX.25 shortening omits zero symbols AFTER data, BEFORE parity.
    // The shared IL2P codec's leading-zero convention remains unchanged.
    let parity = codec(check).encode(&data);
    let mut out = Vec::with_capacity(8 + total);
    out.extend_from_slice(&tag.to_le_bytes());
    out.extend_from_slice(&data[..cap]);
    out.extend_from_slice(&parity);
    Some(out)
}

/// Correct a complete radio block, restoring omitted trailing zero symbols.
pub fn decode(block: &[u8], tag_idx: usize) -> Option<Vec<u8>> {
    let &(_, total, cap, check) = FX25_TAGS.get(tag_idx)?;
    if total == 0 || block.len() != total {
        return None;
    }
    let mut full = vec![0; 255];
    full[..cap].copy_from_slice(&block[..cap]);
    full[255 - check..].copy_from_slice(&block[cap..]);
    codec(check).decode(&mut full)?;
    // Omitted symbols are known zeros, never accept a correction there.
    if full[cap..255 - check].iter().any(|&b| b != 0) {
        return None;
    }
    Some(full[..cap].to_vec())
}

/// A validated frame and its closing-flag position within the protected data.
/// The position lets production dedup use the SAME event time as plain HDLC,
/// even though parity recovery completes much later.
pub struct RecoveredFrame {
    pub frame: DecodedFrame,
    pub trailing_bits: usize,
}

fn extract_ax25(data: &[u8]) -> Option<(DecodedFrame, usize)> {
    if data.first() != Some(&0x7e) {
        return None;
    }
    let mut decoder = HdlcDecoder::new(0, 0, 0, false);
    let (mut line, mut nudge, mut symbols) = (false, 0, 0);
    for (i, bit) in data
        .iter()
        .flat_map(|b| (0..8).map(move |s| (b >> s) & 1))
        .enumerate()
    {
        if bit == 0 {
            line = !line;
        }
        if let Some(frame) = decoder.process_bit(line, &mut nudge, &mut symbols) {
            return Some((frame, i + 1));
        }
    }
    None
}

/// Bounded bit receiver: one <=255-byte block, <=11 popcounts per search bit.
/// Input is NRZI-decoded data, before HDLC unstuffing. Output is FCS validated.
/// Partial/false blocks expire after their announced length (<=2040 bits).
pub struct Fx25Receiver {
    tag_accum: u64,
    search_bits: usize,
    block: Vec<u8>,
    block_bits: usize,
    tag_idx: Option<usize>,
}
impl Default for Fx25Receiver {
    fn default() -> Self {
        Self::new()
    }
}
impl Fx25Receiver {
    pub fn new() -> Self {
        Self {
            tag_accum: 0,
            search_bits: 0,
            block: Vec::with_capacity(255),
            block_bits: 0,
            tag_idx: None,
        }
    }
    pub fn reset(&mut self) {
        self.tag_accum = 0;
        self.search_bits = 0;
        self.block.clear();
        self.block_bits = 0;
        self.tag_idx = None;
    }
    pub fn process_bit(&mut self, bit: bool) -> Option<RecoveredFrame> {
        // Keep searching during collection too: a truncated block must not
        // consume the next valid tag. Distance threshold matches Dire Wolf.
        self.tag_accum = (self.tag_accum >> 1) | (u64::from(bit) << 63);
        self.search_bits = (self.search_bits + 1).min(64);
        if self.search_bits == 64 {
            if let Some((idx, _)) = correlate_tag(self.tag_accum, 8) {
                self.tag_idx = Some(idx);
                self.block.clear();
                self.block_bits = 0;
                self.search_bits = 0;
                return None;
            }
        }
        let idx = self.tag_idx?;
        if self.block_bits.is_multiple_of(8) {
            self.block.push(0);
        }
        if bit {
            *self.block.last_mut()? |= 1 << (self.block_bits % 8);
        }
        self.block_bits += 1;
        let total = FX25_TAGS[idx].1;
        if self.block_bits != total * 8 {
            return None;
        }
        self.tag_idx = None;
        let data = decode(&self.block, idx)?;
        let (frame, end_bit) = extract_ax25(&data)?;
        Some(RecoveredFrame {
            frame,
            trailing_bits: total * 8 - end_bit,
        })
    }
}
