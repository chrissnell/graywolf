//! HDLC frame decoder and supporting types.
//!
//! Extracts AX.25 frames from a stream of demodulated bits. Implements NRZI
//! decoding, flag/abort detection, bit unstuffing, and FCS-16 validation with
//! optional bit-error retry strategies.
//!
//! The decoder stores raw received bits alongside the direct byte accumulator
//! so that retry strategies (single/double bit-flip) can re-process the stored
//! bits when the initial FCS check fails.

use crate::types::*;

// Maximum number of raw bits in an AX.25 frame excluding flags.
// Worst case: bit stuffing after every 5 data bits.
const MAX_NUM_BITS: usize = MAX_FRAME_LEN * 8 * 6 / 5;

// --- CRC-16/CCITT ---

/// CRC-16/CCITT lookup table (reflected polynomial 0x8408).
static CCITT_TABLE: [u16; 256] = [
    0x0000, 0x1189, 0x2312, 0x329b, 0x4624, 0x57ad, 0x6536, 0x74bf,
    0x8c48, 0x9dc1, 0xaf5a, 0xbed3, 0xca6c, 0xdbe5, 0xe97e, 0xf8f7,
    0x1081, 0x0108, 0x3393, 0x221a, 0x56a5, 0x472c, 0x75b7, 0x643e,
    0x9cc9, 0x8d40, 0xbfdb, 0xae52, 0xdaed, 0xcb64, 0xf9ff, 0xe876,
    0x2102, 0x308b, 0x0210, 0x1399, 0x6726, 0x76af, 0x4434, 0x55bd,
    0xad4a, 0xbcc3, 0x8e58, 0x9fd1, 0xeb6e, 0xfae7, 0xc87c, 0xd9f5,
    0x3183, 0x200a, 0x1291, 0x0318, 0x77a7, 0x662e, 0x54b5, 0x453c,
    0xbdcb, 0xac42, 0x9ed9, 0x8f50, 0xfbef, 0xea66, 0xd8fd, 0xc974,
    0x4204, 0x538d, 0x6116, 0x709f, 0x0420, 0x15a9, 0x2732, 0x36bb,
    0xce4c, 0xdfc5, 0xed5e, 0xfcd7, 0x8868, 0x99e1, 0xab7a, 0xbaf3,
    0x5285, 0x430c, 0x7197, 0x601e, 0x14a1, 0x0528, 0x37b3, 0x263a,
    0xdecd, 0xcf44, 0xfddf, 0xec56, 0x98e9, 0x8960, 0xbbfb, 0xaa72,
    0x6306, 0x728f, 0x4014, 0x519d, 0x2522, 0x34ab, 0x0630, 0x17b9,
    0xef4e, 0xfec7, 0xcc5c, 0xddd5, 0xa96a, 0xb8e3, 0x8a78, 0x9bf1,
    0x7387, 0x620e, 0x5095, 0x411c, 0x35a3, 0x242a, 0x16b1, 0x0738,
    0xffcf, 0xee46, 0xdcdd, 0xcd54, 0xb9eb, 0xa862, 0x9af9, 0x8b70,
    0x8408, 0x9581, 0xa71a, 0xb693, 0xc22c, 0xd3a5, 0xe13e, 0xf0b7,
    0x0840, 0x19c9, 0x2b52, 0x3adb, 0x4e64, 0x5fed, 0x6d76, 0x7cff,
    0x9489, 0x8500, 0xb79b, 0xa612, 0xd2ad, 0xc324, 0xf1bf, 0xe036,
    0x18c1, 0x0948, 0x3bd3, 0x2a5a, 0x5ee5, 0x4f6c, 0x7df7, 0x6c7e,
    0xa50a, 0xb483, 0x8618, 0x9791, 0xe32e, 0xf2a7, 0xc03c, 0xd1b5,
    0x2942, 0x38cb, 0x0a50, 0x1bd9, 0x6f66, 0x7eef, 0x4c74, 0x5dfd,
    0xb58b, 0xa402, 0x9699, 0x8710, 0xf3af, 0xe226, 0xd0bd, 0xc134,
    0x39c3, 0x284a, 0x1ad1, 0x0b58, 0x7fe7, 0x6e6e, 0x5cf5, 0x4d7c,
    0xc60c, 0xd785, 0xe51e, 0xf497, 0x8028, 0x91a1, 0xa33a, 0xb2b3,
    0x4a44, 0x5bcd, 0x6956, 0x78df, 0x0c60, 0x1de9, 0x2f72, 0x3efb,
    0xd68d, 0xc704, 0xf59f, 0xe416, 0x90a9, 0x8120, 0xb3bb, 0xa232,
    0x5ac5, 0x4b4c, 0x79d7, 0x685e, 0x1ce1, 0x0d68, 0x3ff3, 0x2e7a,
    0xe70e, 0xf687, 0xc41c, 0xd595, 0xa12a, 0xb0a3, 0x8238, 0x93b1,
    0x6b46, 0x7acf, 0x4854, 0x59dd, 0x2d62, 0x3ceb, 0x0e70, 0x1ff9,
    0xf78f, 0xe606, 0xd49d, 0xc514, 0xb1ab, 0xa022, 0x92b9, 0x8330,
    0x7bc7, 0x6a4e, 0x58d5, 0x495c, 0x3de3, 0x2c6a, 0x1ef1, 0x0f78,
];

/// Compute CRC-16/CCITT FCS over a contiguous byte slice.
pub fn fcs_calc(data: &[u8]) -> u16 {
    let mut crc: u16 = 0xffff;
    for &byte in data {
        crc = (crc >> 8) ^ CCITT_TABLE[((crc ^ byte as u16) & 0xff) as usize];
    }
    crc ^ 0xffff
}

/// Incremental CRC-16/CCITT. Feed one byte at a time.
/// Start with `crc = 0xffff`, finalize with `crc ^ 0xffff`.
#[inline]
pub fn crc16_step(crc: u16, byte: u8) -> u16 {
    (crc >> 8) ^ CCITT_TABLE[((crc ^ byte as u16) & 0xff) as usize]
}

/// Finalize an incremental CRC-16/CCITT computation.
#[inline]
pub fn crc16_finalize(crc: u16) -> u16 {
    crc ^ 0xffff
}

// --- Descrambler (from demod_9600.h) ---

/// G3RUH / K9NG descrambler for 9600 baud.
///
/// `input` is one raw received bit (0 or 1).
/// `state` is the 17-bit shift register state (modified in place).
/// Returns the descrambled bit.
#[inline]
pub fn descramble(input: i32, state: &mut i32) -> i32 {
    let out = (input ^ (*state >> 16) ^ (*state >> 11)) & 1;
    *state = (*state << 1) | (input & 1);
    out
}

// --- Raw Received Bit Buffer ---

/// Storage for raw received bits, equivalent to `rrbb_t` in the C code.
///
/// Stores the raw (pre-NRZI) bits so they can be re-processed with
/// different error correction strategies by a retry decoder.
#[derive(Clone)]
pub struct RawBitBuffer {
    pub chan: usize,
    pub subchan: usize,
    pub slice: usize,

    pub is_scrambled: bool,
    pub descram_state: i32,
    pub prev_descram: i32,

    pub audio_level_mark: f32,
    pub audio_level_space: f32,
    pub speed_error: f32,

    bits: Vec<u8>,
}

impl RawBitBuffer {
    pub fn new(
        chan: usize,
        subchan: usize,
        slice: usize,
        is_scrambled: bool,
        descram_state: i32,
        prev_descram: i32,
    ) -> Self {
        Self {
            chan,
            subchan,
            slice,
            is_scrambled,
            descram_state,
            prev_descram,
            audio_level_mark: 0.0,
            audio_level_space: 0.0,
            speed_error: 0.0,
            bits: Vec::with_capacity(MAX_NUM_BITS),
        }
    }

    /// Reset the buffer to start accumulating a new frame.
    pub fn clear(&mut self, is_scrambled: bool, descram_state: i32, prev_descram: i32) {
        self.is_scrambled = is_scrambled;
        self.descram_state = descram_state;
        self.prev_descram = prev_descram;
        self.bits.clear();
    }

    /// Append one raw bit.
    #[inline]
    pub fn append_bit(&mut self, bit: u8) {
        if self.bits.len() < MAX_NUM_BITS {
            self.bits.push(bit);
        }
    }

    /// Remove the last 8 bits (the flag pattern).
    pub fn chop8(&mut self) {
        let new_len = self.bits.len().saturating_sub(8);
        self.bits.truncate(new_len);
    }

    /// Number of bits accumulated.
    #[inline]
    pub fn len(&self) -> usize {
        self.bits.len()
    }

    #[inline]
    pub fn is_empty(&self) -> bool {
        self.bits.is_empty()
    }

    /// Get bit at index.
    #[inline]
    pub fn get_bit(&self, index: usize) -> u8 {
        self.bits[index]
    }

    /// Access the raw bits slice.
    pub fn bits(&self) -> &[u8] {
        &self.bits
    }

    /// Flip (XOR) a single bit at the given index.
    ///
    /// Used by retry strategies that try correcting individual bit errors.
    ///
    /// # Panics
    ///
    /// Panics if `index >= len()`.
    #[inline]
    pub fn flip_bit(&mut self, index: usize) {
        self.bits[index] ^= 1;
    }
}

// --- Decoded Frame ---

/// A successfully decoded AX.25 frame with metadata.
#[derive(Clone, Debug)]
pub struct DecodedFrame {
    /// Radio channel.
    pub chan: usize,

    /// Demodulator subchannel.
    pub subchan: usize,

    /// Slicer number.
    pub slice: usize,

    /// Frame data (FCS stripped).
    pub data: Vec<u8>,

    /// How the frame was decoded (direct or which retry strategy).
    pub retry: RetryType,

    /// Demodulator quality metric (0-100).
    pub quality: i32,

    /// Mark signal level at time of capture.
    pub audio_level_mark: f32,

    /// Space signal level at time of capture.
    pub audio_level_space: f32,

    /// Received baud rate error as percentage.
    pub speed_error: f32,

    /// Approximate audio sample offset at frame emission (0 if not set).
    /// Populated by the demodulator, not the HDLC decoder itself.
    pub sample_offset: u64,
}

// --- EAS state ---

/// EAS (Emergency Alert System) decoder state, per HDLC decoder instance.
///
/// Accumulates raw bits to detect EAS ZCZC/NNNN preambles and extract
/// the EAS message. Not used by standard AX.25 HDLC decoding.
#[derive(Clone, Default)]
pub struct EasState {
    pub acc: u64,
    pub gathering: bool,
    pub plus_found: bool,
    pub fields_after_plus: i32,
    pub frame_buf: Vec<u8>,
}

// --- HDLC Decoder ---

/// Per-channel/subchannel/slice HDLC frame decoder.
///
/// This is the Rust equivalent of `struct hdlc_state_s` in the C code.
/// Extracts AX.25 frames from a stream of demodulated bits using NRZI
/// decoding, flag/abort detection, bit unstuffing, and FCS validation.
///
/// Supports both direct decoding (equivalent to OLD_WAY) and raw bit
/// storage for retry-based decoding (the default "new way" in C).
pub struct HdlcDecoder {
    // --- Context ---
    chan: usize,
    subchan: usize,
    slice: usize,
    is_scrambled: bool,

    // --- Core HDLC state ---

    /// Previous raw bit for NRZI decoding (0 or 1).
    prev_raw: u8,

    /// Descrambler shift register for 9600 baud.
    lfsr: i32,

    /// Previous descrambled bit for 9600 baud.
    prev_descram: i32,

    /// 8-bit pattern detector shift register.
    /// Used to detect flags (0x7E), aborts (0xFE), and bit stuffing.
    pat_det: u8,

    /// Last 32 raw bits to detect 4 consecutive flag patterns.
    flag4_det: u32,

    /// Accumulator for building up one octet from incoming bits.
    oacc: u8,

    /// Number of bits in `oacc` (0..7). -1 means "not accumulating".
    olen: i32,

    /// Frame buffer for direct decoding.
    frame_buf: [u8; MAX_FRAME_LEN],

    /// Number of octets in `frame_buf`.
    frame_len: usize,

    /// Raw received bit buffer for retry-based decoding.
    rrbb: RawBitBuffer,

    /// EAS decoder state (unused by AFSK, present for future EAS modem support).
    #[allow(dead_code)]
    eas: EasState,

    /// Maximum bit error correction level.
    fix_bits: RetryType,
}

impl Default for HdlcDecoder {
    fn default() -> Self {
        Self::new(0, 0, 0, false)
    }
}

impl HdlcDecoder {
    /// Create a new HDLC decoder for the given channel/subchannel/slice.
    pub fn new(chan: usize, subchan: usize, slice: usize, is_scrambled: bool) -> Self {
        Self {
            chan,
            subchan,
            slice,
            is_scrambled,
            prev_raw: 0,
            lfsr: 0,
            prev_descram: 0,
            pat_det: 0,
            flag4_det: 0,
            oacc: 0,
            olen: -1,
            frame_buf: [0u8; MAX_FRAME_LEN],
            frame_len: 0,
            rrbb: RawBitBuffer::new(chan, subchan, slice, is_scrambled, 0, 0),
            eas: EasState::default(),
            fix_bits: RetryType::None,
        }
    }

    /// Set the maximum retry/fix-bits level for CRC error recovery.
    pub fn set_fix_bits(&mut self, level: RetryType) {
        self.fix_bits = level;
    }

    /// Set audio level info to attach to decoded frames.
    pub fn set_audio_level(&mut self, mark_peak: f32, space_peak: f32) {
        self.rrbb.audio_level_mark = mark_peak;
        self.rrbb.audio_level_space = space_peak;
    }

    /// Process one raw demodulated bit.
    ///
    /// This is the Rust equivalent of `hdlc_rec_bit_new()` in the C code.
    ///
    /// - `raw` — The raw demodulated bit (true = 1, false = 0).
    /// - `pll_nudge_total` — Mutable reference to PLL nudge accumulator for speed error measurement.
    /// - `pll_symbol_count` — Mutable reference to PLL symbol counter.
    ///
    /// Returns `Some(DecodedFrame)` when a complete, valid frame is decoded.
    pub fn process_bit(
        &mut self,
        raw: bool,
        pll_nudge_total: &mut i64,
        pll_symbol_count: &mut i32,
    ) -> Option<DecodedFrame> {
        let raw_int: u8 = if raw { 1 } else { 0 };

        // NRZI / scrambled decoding
        let dbit: u8 = if self.is_scrambled {
            let descram = descramble(raw_int as i32, &mut self.lfsr);
            let d = if descram == self.prev_descram { 1u8 } else { 0u8 };
            self.prev_descram = descram;
            self.prev_raw = raw_int;
            d
        } else {
            let d = if raw_int == self.prev_raw { 1u8 } else { 0u8 };
            self.prev_raw = raw_int;
            d
        };

        // Shift into 8-bit pattern detector (newest bit at MSB)
        self.pat_det >>= 1;
        if dbit != 0 {
            self.pat_det |= 0x80;
        }

        // Shift into 32-bit flag-run detector
        self.flag4_det >>= 1;
        if dbit != 0 {
            self.flag4_det |= 0x8000_0000;
        }

        // Append raw bit to the raw bit buffer
        self.rrbb.append_bit(raw_int);

        if self.pat_det == 0x7e {
            // Flag pattern (01111110) detected
            self.rrbb.chop8();

            let result = if self.rrbb.len() >= MIN_FRAME_LEN * 8 {
                // End of frame — compute speed error and attempt decode
                let speed_error = if *pll_symbol_count > 0 {
                    (*pll_nudge_total as f64 * 100.0
                        / TICKS_PER_PLL_CYCLE
                        / *pll_symbol_count as f64
                        + 0.02) as f32
                } else {
                    0.0
                };
                self.rrbb.speed_error = speed_error;

                self.try_decode()
            } else {
                // Start of new frame — reset PLL measurement
                *pll_nudge_total = 0;
                *pll_symbol_count = -1;
                None
            };

            // Reset for next frame
            self.rrbb.clear(self.is_scrambled, self.lfsr, self.prev_descram);
            self.olen = 0;
            self.frame_len = 0;

            // Seed the raw bit buffer with the last bit of the flag.
            // This is needed to get the first data bit via NRZI.
            self.rrbb.append_bit(self.prev_raw);

            return result;
        } else if self.pat_det == 0xfe {
            // Abort: seven consecutive 1-bits (11111110)
            self.olen = -1;
            self.frame_len = 0;
            self.rrbb
                .clear(self.is_scrambled, self.lfsr, self.prev_descram);
        } else if (self.pat_det & 0xfc) == 0x7c {
            // Bit stuffing: zero after five consecutive ones — discard this bit
        } else {
            // Normal data bit — accumulate into octets
            if self.olen >= 0 {
                self.oacc >>= 1;
                if dbit != 0 {
                    self.oacc |= 0x80;
                }
                self.olen += 1;

                if self.olen == 8 {
                    self.olen = 0;
                    if self.frame_len < MAX_FRAME_LEN {
                        self.frame_buf[self.frame_len] = self.oacc;
                        self.frame_len += 1;
                    }
                }
            }
        }

        None
    }

    /// Attempt to decode a frame from the raw bit buffer.
    ///
    /// First tries direct FCS check. If that fails and `fix_bits` allows it,
    /// attempts retry strategies on the stored raw bits.
    fn try_decode(&self) -> Option<DecodedFrame> {
        // Direct decode from accumulated octets (equivalent to OLD_WAY)
        if self.olen == 7 && self.frame_len >= MIN_FRAME_LEN {
            let actual_fcs = self.frame_buf[self.frame_len - 2] as u16
                | ((self.frame_buf[self.frame_len - 1] as u16) << 8);
            let expected_fcs = fcs_calc(&self.frame_buf[..self.frame_len - 2]);

            if actual_fcs == expected_fcs {
                return Some(DecodedFrame {
                    chan: self.chan,
                    subchan: self.subchan,
                    slice: self.slice,
                    data: self.frame_buf[..self.frame_len - 2].to_vec(),
                    retry: RetryType::None,
                    quality: 0,
                    audio_level_mark: self.rrbb.audio_level_mark,
                    audio_level_space: self.rrbb.audio_level_space,
                    speed_error: self.rrbb.speed_error,
                    sample_offset: 0,
                });
            }
        }

        // If direct decode failed, try from raw bits (handles slightly
        // different boundary conditions vs. the direct accumulator path)
        if let Some(frame) = self.decode_from_raw_bits(&self.rrbb, RetryType::None) {
            return Some(frame);
        }

        // Retry strategies when fix_bits > None
        match self.fix_bits {
            RetryType::None => {}
            RetryType::InvertSingle => {
                if let Some(frame) = self.try_fix_single_bit() {
                    return Some(frame);
                }
            }
            RetryType::InvertDouble => {
                if let Some(frame) = self.try_fix_single_bit() {
                    return Some(frame);
                }
                if let Some(frame) = self.try_fix_double_bit() {
                    return Some(frame);
                }
            }
            _ => {
                if let Some(frame) = self.try_fix_single_bit() {
                    return Some(frame);
                }
                if let Some(frame) = self.try_fix_double_bit() {
                    return Some(frame);
                }
            }
        }

        None
    }

    /// Decode a frame from stored raw bits using NRZI/descramble, flag detection,
    /// bit unstuffing, and FCS checking.
    ///
    /// This is the Rust equivalent of the core decode loop in `hdlc_rec2_try_to_fix_later()`.
    fn decode_from_raw_bits(
        &self,
        rrbb: &RawBitBuffer,
        retry: RetryType,
    ) -> Option<DecodedFrame> {
        let num_bits = rrbb.len();
        if num_bits < MIN_FRAME_LEN * 8 {
            return None;
        }

        let mut prev_raw_state = if num_bits > 0 { rrbb.get_bit(0) } else { 0 };
        let mut lfsr_state = rrbb.descram_state;
        let mut prev_descram_state = rrbb.prev_descram;

        let mut pat: u8 = 0;
        let mut oacc: u8 = 0;
        let mut olen: i32 = -1;
        let mut fbuf = [0u8; MAX_FRAME_LEN];
        let mut flen: usize = 0;

        // Skip the first bit which is the seeded prev_raw from the flag
        for i in 1..num_bits {
            let raw_b = rrbb.get_bit(i);

            let dbit: u8 = if rrbb.is_scrambled {
                let descram = descramble(raw_b as i32, &mut lfsr_state);
                let d = if descram == prev_descram_state { 1u8 } else { 0u8 };
                prev_descram_state = descram;
                prev_raw_state = raw_b;
                d
            } else {
                let d = if raw_b == prev_raw_state { 1u8 } else { 0u8 };
                prev_raw_state = raw_b;
                d
            };

            pat >>= 1;
            if dbit != 0 {
                pat |= 0x80;
            }

            if pat == 0x7e {
                // End flag — check what we have
                if olen == 7 && flen >= MIN_FRAME_LEN {
                    let actual_fcs =
                        fbuf[flen - 2] as u16 | ((fbuf[flen - 1] as u16) << 8);
                    let expected_fcs = fcs_calc(&fbuf[..flen - 2]);

                    if actual_fcs == expected_fcs {
                        return Some(DecodedFrame {
                            chan: rrbb.chan,
                            subchan: rrbb.subchan,
                            slice: rrbb.slice,
                            data: fbuf[..flen - 2].to_vec(),
                            retry,
                            quality: 0,
                            audio_level_mark: rrbb.audio_level_mark,
                            audio_level_space: rrbb.audio_level_space,
                            speed_error: rrbb.speed_error,
                            sample_offset: 0,
                        });
                    }
                }
                olen = 0;
                flen = 0;
            } else if pat == 0xfe {
                olen = -1;
                flen = 0;
            } else if (pat & 0xfc) == 0x7c {
                // bit stuffing — discard
            } else if olen >= 0 {
                oacc >>= 1;
                if dbit != 0 {
                    oacc |= 0x80;
                }
                olen += 1;
                if olen == 8 {
                    olen = 0;
                    if flen < MAX_FRAME_LEN {
                        fbuf[flen] = oacc;
                        flen += 1;
                    }
                }
            }
        }

        // Check the residual frame (between last start flag and end of buffer)
        if olen == 7 && flen >= MIN_FRAME_LEN {
            let actual_fcs = fbuf[flen - 2] as u16 | ((fbuf[flen - 1] as u16) << 8);
            let expected_fcs = fcs_calc(&fbuf[..flen - 2]);

            if actual_fcs == expected_fcs {
                return Some(DecodedFrame {
                    chan: rrbb.chan,
                    subchan: rrbb.subchan,
                    slice: rrbb.slice,
                    data: fbuf[..flen - 2].to_vec(),
                    retry,
                    quality: 0,
                    audio_level_mark: rrbb.audio_level_mark,
                    audio_level_space: rrbb.audio_level_space,
                    speed_error: rrbb.speed_error,
                    sample_offset: 0,
                });
            }
        }

        None
    }

    /// Try flipping each single bit in the raw buffer and re-decode.
    fn try_fix_single_bit(&self) -> Option<DecodedFrame> {
        let mut trial = self.rrbb.clone();
        let num_bits = trial.len();

        for i in 0..num_bits {
            trial.flip_bit(i);
            if let Some(frame) = self.decode_from_raw_bits(&trial, RetryType::InvertSingle) {
                return Some(frame);
            }
            trial.flip_bit(i);
        }

        None
    }

    /// Try flipping each pair of bits in the raw buffer and re-decode.
    fn try_fix_double_bit(&self) -> Option<DecodedFrame> {
        let mut trial = self.rrbb.clone();
        let num_bits = trial.len();

        for i in 0..num_bits {
            trial.flip_bit(i);
            for j in (i + 1)..num_bits {
                trial.flip_bit(j);
                if let Some(frame) =
                    self.decode_from_raw_bits(&trial, RetryType::InvertDouble)
                {
                    return Some(frame);
                }
                trial.flip_bit(j);
            }
            trial.flip_bit(i);
        }

        None
    }

    /// Whether the decoder is currently gathering bits into a frame
    /// (i.e., has seen a start flag but not yet an end flag).
    pub fn is_gathering(&self) -> bool {
        self.olen >= 0
    }

    /// Get a reference to the current raw bit buffer (for diagnostics).
    pub fn raw_bits(&self) -> &RawBitBuffer {
        &self.rrbb
    }

    /// Returns true if the last 32 decoded bits form 4 consecutive flag patterns.
    pub fn has_four_flags(&self) -> bool {
        self.flag4_det == 0x7e7e7e7e
    }
}

// --- Composite DCD tracking ---

/// Composite Data Carrier Detect tracking across subchannels and slicers.
///
/// This is the Rust equivalent of the `composite_dcd` and `dcd_change()` /
/// `hdlc_rec_data_detect_any()` functions in the C code.
pub struct CompositeDcd {
    dcd: [[i32; MAX_SUBCHANS + 1]; MAX_RADIO_CHANS],
    num_subchan: [usize; MAX_RADIO_CHANS],
}

impl Default for CompositeDcd {
    fn default() -> Self {
        Self::new()
    }
}

impl CompositeDcd {
    pub fn new() -> Self {
        Self {
            dcd: [[0i32; MAX_SUBCHANS + 1]; MAX_RADIO_CHANS],
            num_subchan: [1; MAX_RADIO_CHANS],
        }
    }

    pub fn set_num_subchans(&mut self, chan: usize, n: usize) {
        assert!(n >= 1 && n <= MAX_SUBCHANS);
        self.num_subchan[chan] = n;
    }

    /// Update DCD state for a specific subchannel/slice.
    /// Returns `Some((chan, new_state))` if the composite DCD state changed.
    pub fn dcd_change(
        &mut self,
        chan: usize,
        subchan: usize,
        slice: usize,
        state: bool,
    ) -> Option<(usize, bool)> {
        let old = self.data_detect_any(chan);

        if state {
            self.dcd[chan][subchan] |= 1 << slice;
        } else {
            self.dcd[chan][subchan] &= !(1 << slice);
        }

        let new = self.data_detect_any(chan);

        if new != old {
            Some((chan, new))
        } else {
            None
        }
    }

    /// Returns true if ANY subchannel/slice for this channel has data detected.
    pub fn data_detect_any(&self, chan: usize) -> bool {
        for sc in 0..self.num_subchan[chan] {
            if self.dcd[chan][sc] != 0 {
                return true;
            }
        }
        false
    }
}
