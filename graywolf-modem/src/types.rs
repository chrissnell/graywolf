//! Constants, enums, and shared type definitions.
//!
//! All values are ported from the Dire Wolf C headers (`direwolf.h`, `audio.h`,
//! `ax25_pad.h`, `fsk_demod_state.h`). They are organized by origin so it is
//! straightforward to cross-reference with the original source.

// --- Dimensional constants (from direwolf.h) ---

pub const MAX_ADEVS: usize = 3;
pub const MAX_RADIO_CHANS: usize = MAX_ADEVS * 2;
pub const MAX_TOTAL_CHANS: usize = 16;
pub const MAX_SUBCHANS: usize = 9;
pub const MAX_SLICERS: usize = 9;
pub const MAX_FILTER_SIZE: usize = 480;

// --- Audio defaults (from audio.h) ---

pub const DEFAULT_SAMPLES_PER_SEC: u32 = 44100;
pub const MIN_SAMPLES_PER_SEC: u32 = 8000;
pub const MAX_SAMPLES_PER_SEC: u32 = 192000;
pub const DEFAULT_BITS_PER_SAMPLE: u32 = 16;
pub const DEFAULT_NUM_CHANNELS: u32 = 1;
pub const DEFAULT_BAUD: u32 = 1200;
pub const MIN_BAUD: u32 = 100;
pub const MAX_BAUD: u32 = 40000;
pub const DEFAULT_MARK_FREQ: u32 = 1200;
pub const DEFAULT_SPACE_FREQ: u32 = 2200;

// --- DCD thresholds (from fsk_demod_state.h) ---

pub const DCD_THRESH_ON: u32 = 30;
pub const DCD_THRESH_OFF: u32 = 6;
pub const DCD_GOOD_WIDTH: i32 = 512;

// --- PLL (from fsk_demod_state.h) ---

pub const TICKS_PER_PLL_CYCLE: f64 = 256.0 * 256.0 * 256.0 * 256.0;

// --- CIC filter (from fsk_demod_state.h) ---

pub const CIC_LEN_MAX: usize = 4000;

// --- AX.25 frame sizes (from ax25_pad.h) ---

pub const AX25_MAX_ADDRS: usize = 10;
pub const AX25_MAX_ADDR_LEN: usize = 12;
pub const AX25_MAX_INFO_LEN: usize = 2048;
pub const AX25_MIN_PACKET_LEN: usize = 2 * 7 + 1; // 15: dest(7) + src(7) + ctrl(1)
pub const AX25_MAX_PACKET_LEN: usize = AX25_MAX_ADDRS * 7 + 2 + 3 + AX25_MAX_INFO_LEN; // 2123
pub const MIN_FRAME_LEN: usize = AX25_MIN_PACKET_LEN + 2; // 17: packet + FCS
pub const MAX_FRAME_LEN: usize = AX25_MAX_PACKET_LEN + 2; // 2125: packet + FCS

// --- Transmit timing defaults (from audio.h) ---

pub const DEFAULT_DWAIT: u32 = 0;
pub const DEFAULT_SLOTTIME: u32 = 10;
pub const DEFAULT_PERSIST: u32 = 63;
pub const DEFAULT_TXDELAY: u32 = 30;
pub const DEFAULT_TXTAIL: u32 = 10;

// --- Enums ---

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum WindowType {
    Truncated,
    Cosine,
    Hamming,
    Blackman,
    FlatTop,
}

#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub enum AfskProfile {
    A,
    B,
}

/// Modem types (from audio.h: enum modem_t)
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum ModemType {
    #[default]
    Afsk,
    Baseband,
    Scramble,
    Qpsk,
    Psk8,
    Off,
    Qam16,
    Qam64,
    Ais,
    Eas,
}

/// V.26 alternatives (from audio.h: enum v26_e)
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum V26Alternative {
    #[default]
    Unspecified,
    A,
    B,
}

/// Layer 2 protocol (from audio.h: enum layer2_t)
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Layer2 {
    #[default]
    Ax25,
    Fx25,
    Il2p,
}

/// Retry / fix-bits strategy for CRC error recovery (from audio.h: enum retry_e)
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum RetryType {
    #[default]
    None,
    InvertSingle,
    InvertDouble,
    InvertTriple,
    InvertTwoSep,
}

/// Sanity check level for recovered frames (from audio.h: enum sanity_e)
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum SanityCheck {
    Aprs,
    Ax25,
    #[default]
    None,
}

/// Channel medium type (from audio.h: enum medium_e)
#[derive(Clone, Copy, Debug, Default, PartialEq, Eq)]
pub enum Medium {
    #[default]
    None,
    Radio,
    Igate,
    NetTnc,
}
