//! IL2P (Improved Layer 2 Protocol) codec.
//!
//! Ported from direwolf's il2p_codec.c, il2p_header.c, il2p_payload.c,
//! il2p_scramble.c. IL2P provides a more efficient and robust layer 2
//! framing than standard AX.25 HDLC with Reed-Solomon FEC.
//!
//! IL2P features:
//! - Sync word detection (3-byte pattern)
//! - RS-encoded header (15 bytes: 13 data + 2 parity)
//! - RS-encoded payload blocks (up to 1023 bytes)
//! - Scrambling for DC balance

mod scramble;
mod header;
mod payload;
mod rs_il2p;
#[cfg(test)]
mod tests;

pub use scramble::{il2p_scramble, il2p_descramble};
pub use header::{Il2pHeader, encode_header, decode_header};
pub use payload::{encode_payload, decode_payload, payload_block_count, payload_block_sizes};

/// IL2P sync word: 3 bytes, bit-reversed from the spec.
/// This is the pattern that precedes every IL2P frame.
pub const IL2P_SYNC_WORD: [u8; 3] = [0xF1, 0x5E, 0x48];

/// Maximum IL2P payload size.
pub const IL2P_MAX_PAYLOAD: usize = 1023;

/// IL2P header size (before RS encoding): 13 bytes.
pub const IL2P_HEADER_SIZE: usize = 13;

/// IL2P header RS-encoded size: 15 bytes (13 + 2 parity).
pub const IL2P_HEADER_ENCODED_SIZE: usize = 15;

/// IL2P payload RS block sizes.
pub const IL2P_PAYLOAD_MAX_BLOCK: usize = 239;
pub const IL2P_PAYLOAD_PARITY_PER_BLOCK: usize = 16;

/// Encode a complete IL2P frame from AX.25 data.
///
/// Returns the complete IL2P frame: sync_word + encoded_header + encoded_payload.
/// The AX.25 data should be the frame without FCS.
pub fn encode(ax25_data: &[u8]) -> Option<Vec<u8>> {
    if ax25_data.len() < 15 { // minimum AX.25: 2 addresses + ctrl
        return None;
    }

    // Build IL2P header from AX.25 addresses and control
    let hdr = Il2pHeader::from_ax25(ax25_data)?;
    let info_start = hdr.ax25_info_offset;

    let payload_data = if info_start < ax25_data.len() {
        &ax25_data[info_start..]
    } else {
        &[]
    };

    if payload_data.len() > IL2P_MAX_PAYLOAD {
        return None;
    }

    // Encode header with RS
    let encoded_header = encode_header(&hdr)?;

    // Scramble header
    let scrambled_header = il2p_scramble(&encoded_header);

    // Encode payload blocks with RS
    let encoded_payload = encode_payload(payload_data);

    // Scramble payload
    let scrambled_payload = il2p_scramble(&encoded_payload);

    // Assemble: sync + header + payload
    let mut frame = Vec::with_capacity(3 + scrambled_header.len() + scrambled_payload.len());
    frame.extend_from_slice(&IL2P_SYNC_WORD);
    frame.extend_from_slice(&scrambled_header);
    frame.extend_from_slice(&scrambled_payload);

    Some(frame)
}

/// Decode a complete IL2P frame.
///
/// Input: raw bytes after sync word detection (header + payload).
/// Returns the decoded AX.25 frame data (without FCS).
pub fn decode(data: &[u8]) -> Option<Vec<u8>> {
    if data.len() < IL2P_HEADER_ENCODED_SIZE {
        return None;
    }

    // Descramble header
    let header_bytes = il2p_descramble(&data[..IL2P_HEADER_ENCODED_SIZE]);

    // RS decode header
    let hdr = decode_header(&header_bytes)?;

    // Determine payload size from header
    let payload_len = hdr.payload_len;
    if payload_len == 0 {
        // Header-only frame, reconstruct AX.25
        return hdr.to_ax25();
    }

    // Calculate expected encoded payload size
    let block_sizes = payload_block_sizes(payload_len);
    let encoded_payload_len: usize = block_sizes
        .iter()
        .map(|&(data_sz, parity_sz)| data_sz + parity_sz)
        .sum();

    let payload_start = IL2P_HEADER_ENCODED_SIZE;
    if data.len() < payload_start + encoded_payload_len {
        return None;
    }

    // Descramble payload
    let payload_bytes = il2p_descramble(&data[payload_start..payload_start + encoded_payload_len]);

    // RS decode payload blocks
    let payload = decode_payload(&payload_bytes, payload_len)?;

    // Reconstruct AX.25 frame
    let mut ax25 = hdr.to_ax25()?;
    ax25.extend_from_slice(&payload);

    Some(ax25)
}

/// IL2P receiver state machine.
pub struct Il2pReceiver {
    // Sync detection
    sync_accum: u32, // 24-bit shift register
    state: Il2pState,

    // Byte accumulation
    byte_acc: u8,
    bit_count: usize,

    // Frame buffer
    frame_buf: Vec<u8>,
    bytes_needed: usize,

    // Header decode result
    header: Option<Il2pHeader>,

    // Output
    decoded_frames: Vec<Vec<u8>>,

    // Max sync word bit errors
    max_sync_errors: u32,
}

#[derive(Clone, Copy, PartialEq)]
enum Il2pState {
    SearchingSync,
    CollectingHeader,
    CollectingPayload,
}

impl Il2pReceiver {
    pub fn new() -> Self {
        Self {
            sync_accum: 0,
            state: Il2pState::SearchingSync,
            byte_acc: 0,
            bit_count: 0,
            frame_buf: Vec::with_capacity(1200),
            bytes_needed: 0,
            header: None,
            decoded_frames: Vec::new(),
            max_sync_errors: 1,
        }
    }

    /// Feed one demodulated bit.
    pub fn process_bit(&mut self, bit: bool) {
        match self.state {
            Il2pState::SearchingSync => {
                self.sync_accum = ((self.sync_accum << 1) | u32::from(bit)) & 0xFFFFFF;

                let sync_word = ((IL2P_SYNC_WORD[0] as u32) << 16)
                    | ((IL2P_SYNC_WORD[1] as u32) << 8)
                    | (IL2P_SYNC_WORD[2] as u32);

                let dist = (self.sync_accum ^ sync_word).count_ones();
                if dist <= self.max_sync_errors {
                    self.state = Il2pState::CollectingHeader;
                    self.frame_buf.clear();
                    self.byte_acc = 0;
                    self.bit_count = 0;
                    self.bytes_needed = IL2P_HEADER_ENCODED_SIZE;
                }
            }
            Il2pState::CollectingHeader | Il2pState::CollectingPayload => {
                if bit {
                    self.byte_acc |= 1 << (self.bit_count & 7);
                }
                self.bit_count += 1;

                if (self.bit_count & 7) == 0 {
                    self.frame_buf.push(self.byte_acc);
                    self.byte_acc = 0;

                    if self.state == Il2pState::CollectingHeader
                        && self.frame_buf.len() >= IL2P_HEADER_ENCODED_SIZE
                    {
                        // Try to decode header to determine payload size
                        let header_bytes = il2p_descramble(&self.frame_buf[..IL2P_HEADER_ENCODED_SIZE]);
                        if let Some(hdr) = decode_header(&header_bytes) {
                            if hdr.payload_len == 0 {
                                // Header-only frame
                                if let Some(ax25) = hdr.to_ax25() {
                                    self.decoded_frames.push(ax25);
                                }
                                self.reset_to_search();
                            } else {
                                let block_sizes = payload_block_sizes(hdr.payload_len);
                                let encoded_len: usize = block_sizes
                                    .iter()
                                    .map(|&(d, p)| d + p)
                                    .sum();
                                self.bytes_needed = IL2P_HEADER_ENCODED_SIZE + encoded_len;
                                self.header = Some(hdr);
                                self.state = Il2pState::CollectingPayload;
                            }
                        } else {
                            self.reset_to_search();
                        }
                    } else if self.state == Il2pState::CollectingPayload
                        && self.frame_buf.len() >= self.bytes_needed
                    {
                        // Full frame collected, decode
                        if let Some(ax25) = decode(&self.frame_buf) {
                            self.decoded_frames.push(ax25);
                        }
                        self.reset_to_search();
                    }
                }
            }
        }
    }

    fn reset_to_search(&mut self) {
        self.state = Il2pState::SearchingSync;
        self.sync_accum = 0;
        self.header = None;
    }

    pub fn take_frames(&mut self) -> Vec<Vec<u8>> {
        std::mem::take(&mut self.decoded_frames)
    }

    pub fn reset(&mut self) {
        self.reset_to_search();
        self.frame_buf.clear();
    }
}
