//! Mode S / ADS-B message construction and field decoding.
//!
//! A focused decoder for the extended-squitter formats that make ADS-B useful:
//! downlink format, ICAO address, type code, aircraft identification
//! (callsign), and airborne position (CPR + barometric altitude). The layout
//! follows the same ME-field parsing as the `adsb_deku` reference
//! (<https://github.com/rsadsb/adsb_deku>). Frame construction is the inverse,
//! so a callsign round-trips through [`super::modulator`] and
//! [`super::demodulator`].

use super::crc;

/// Extended squitter downlink format (ADS-B).
pub const DF_ADSB: u8 = 17;
/// Default transponder capability for a DF17 broadcast.
pub const CA_LEVEL2: u8 = 5;

/// 6-bit ident charset (index → ASCII). Index 32 is space; `#` marks the
/// unused/reserved slots that decoding skips.
const IDENT_CHARSET: &[u8; 64] =
    b"#ABCDEFGHIJKLMNOPQRSTUVWXYZ##### ###############0123456789######";

/// Read `len` bits (`<= 32`) from `msg`, MSB-first, starting at bit `start`.
fn get_bits(msg: &[u8], start: usize, len: usize) -> u32 {
    let mut v = 0u32;
    for k in 0..len {
        let bit = start + k;
        let set = (msg[bit / 8] >> (7 - (bit % 8))) & 1;
        v = (v << 1) | set as u32;
    }
    v
}

/// A parsed Mode S frame — a thin view over the raw bytes.
#[derive(Clone, Debug)]
pub struct Frame<'a> {
    bytes: &'a [u8],
}

impl<'a> Frame<'a> {
    pub fn new(bytes: &'a [u8]) -> Self {
        Self { bytes }
    }

    /// Downlink format (5 bits).
    pub fn df(&self) -> u8 {
        self.bytes[0] >> 3
    }

    /// Transponder capability (3 bits) for DF17/18.
    pub fn ca(&self) -> u8 {
        self.bytes[0] & 0x07
    }

    /// 24-bit ICAO aircraft address (DF17/18).
    pub fn icao(&self) -> u32 {
        get_bits(self.bytes, 8, 24)
    }

    /// ME type code (top 5 bits of the message field), or `None` if too short.
    pub fn type_code(&self) -> Option<u8> {
        if self.bytes.len() < 11 {
            return None;
        }
        Some((get_bits(self.bytes, 32, 5)) as u8)
    }

    /// Decode the 8-character callsign for identification messages (TC 1-4).
    pub fn callsign(&self) -> Option<String> {
        match self.type_code() {
            Some(tc) if (1..=4).contains(&tc) => {}
            _ => return None,
        }
        let mut s = String::with_capacity(8);
        for c in 0..8 {
            // Callsign chars start at ME bit 9 (message bit 40), 6 bits each.
            let idx = get_bits(self.bytes, 40 + c * 6, 6) as usize;
            let ch = IDENT_CHARSET[idx];
            if ch != b'#' {
                s.push(ch as char);
            }
        }
        Some(s.trim_end().to_string())
    }

    /// Decode an airborne-position ME field (TC 9-18, 20-22): CPR fraction,
    /// odd/even flag, and barometric altitude (feet).
    pub fn airborne_position(&self) -> Option<AirbornePosition> {
        let tc = self.type_code()?;
        if !((9..=18).contains(&tc) || (20..=22).contains(&tc)) {
            return None;
        }
        Some(AirbornePosition {
            odd: get_bits(self.bytes, 53, 1) == 1,
            lat_cpr: get_bits(self.bytes, 54, 17),
            lon_cpr: get_bits(self.bytes, 71, 17),
            altitude: decode_ac12(get_bits(self.bytes, 40, 12) as u16),
        })
    }
}

/// One half of an airborne CPR position report.
#[derive(Clone, Copy, Debug, PartialEq)]
pub struct AirbornePosition {
    /// CPR format: `false` = even frame, `true` = odd frame.
    pub odd: bool,
    /// 17-bit encoded latitude fraction.
    pub lat_cpr: u32,
    /// 17-bit encoded longitude fraction.
    pub lon_cpr: u32,
    /// Barometric altitude in feet, if the AC field was valid.
    pub altitude: Option<i32>,
}

/// Decode a 12-bit AC altitude field to feet (25 ft Q-bit encoding).
fn decode_ac12(ac: u16) -> Option<i32> {
    if ac == 0 {
        return None;
    }
    // Q bit (bit 5 of 12, i.e. mask 0x10) selects 25 ft resolution.
    if ac & 0x10 != 0 {
        let n = (((ac & 0x0FE0) >> 1) | (ac & 0x000F)) as i32;
        Some(n * 25 - 1000)
    } else {
        // 100 ft Gillham-coded altitudes are not decoded here.
        None
    }
}

/// CPR longitude-zone count for a given latitude (ICAO Annex 10).
fn cpr_nl(lat: f64) -> f64 {
    let lat = lat.abs();
    if lat < 1e-9 {
        return 59.0;
    }
    if (lat - 87.0).abs() < 1e-9 {
        return 2.0;
    }
    if lat > 87.0 {
        return 1.0;
    }
    let nz = 15.0;
    let a = 1.0 - (std::f64::consts::PI / (2.0 * nz)).cos();
    let b = (std::f64::consts::PI / 180.0 * lat).cos().powi(2);
    let x = (1.0 - a / b).acos();
    (2.0 * std::f64::consts::PI / x).floor()
}

/// Globally decode an airborne position from an even and an odd frame.
///
/// `newest_is_odd` marks which of the pair was received most recently; its
/// longitude zone anchors the result. Returns `(latitude, longitude)` in
/// degrees, or `None` when the two frames straddle a latitude zone boundary.
pub fn cpr_decode_airborne(
    even: &AirbornePosition,
    odd: &AirbornePosition,
    newest_is_odd: bool,
) -> Option<(f64, f64)> {
    const SCALE: f64 = 131072.0; // 2^17
    let lat_even = even.lat_cpr as f64 / SCALE;
    let lat_odd = odd.lat_cpr as f64 / SCALE;

    let dlat_even = 360.0 / 60.0;
    let dlat_odd = 360.0 / 59.0;

    let j = (59.0 * lat_even - 60.0 * lat_odd + 0.5).floor();

    let mut rlat_even = dlat_even * (j.rem_euclid(60.0) + lat_even);
    let mut rlat_odd = dlat_odd * (j.rem_euclid(59.0) + lat_odd);
    if rlat_even >= 270.0 {
        rlat_even -= 360.0;
    }
    if rlat_odd >= 270.0 {
        rlat_odd -= 360.0;
    }

    if cpr_nl(rlat_even) != cpr_nl(rlat_odd) {
        return None;
    }

    let (rlat, nl, lon_cpr) = if newest_is_odd {
        (rlat_odd, cpr_nl(rlat_odd) - 1.0, odd.lon_cpr as f64 / SCALE)
    } else {
        (rlat_even, cpr_nl(rlat_even), even.lon_cpr as f64 / SCALE)
    };

    let ni = nl.max(1.0);
    let dlon = 360.0 / ni;
    let m = (even.lon_cpr as f64 / SCALE * (cpr_nl(rlat) - 1.0)
        - odd.lon_cpr as f64 / SCALE * cpr_nl(rlat)
        + 0.5)
        .floor();
    let mut lon = dlon * (m.rem_euclid(ni) + lon_cpr);
    if lon >= 180.0 {
        lon -= 360.0;
    }
    Some((rlat, lon))
}

/// Build a complete DF17 identification (callsign) frame with valid parity.
///
/// `callsign` is padded/truncated to 8 chars; unsupported characters become
/// spaces. Returns the 14-byte extended-squitter frame.
pub fn encode_identification(icao: u32, callsign: &str) -> [u8; 14] {
    let mut frame = [0u8; 14];
    frame[0] = (DF_ADSB << 3) | CA_LEVEL2;
    frame[1] = (icao >> 16) as u8;
    frame[2] = (icao >> 8) as u8;
    frame[3] = icao as u8;

    // ME: TC(5)=4, category(3)=0, then 8 x 6-bit characters.
    let tc: u8 = 4;
    frame[4] = tc << 3;

    let mut chars = [b' '; 8];
    for (i, c) in callsign.chars().take(8).enumerate() {
        chars[i] = char_to_ident(c);
    }
    // Pack 48 callsign bits starting at message bit 40 (ME bit 9).
    let mut bitpos = 40usize;
    for &c in &chars {
        let code = ident_to_index(c);
        for k in (0..6).rev() {
            let set = (code >> k) & 1;
            frame[bitpos / 8] |= set << (7 - (bitpos % 8));
            bitpos += 1;
        }
    }

    crc::append_parity(&mut frame);
    frame
}

fn char_to_ident(c: char) -> u8 {
    let up = c.to_ascii_uppercase();
    match up {
        'A'..='Z' | '0'..='9' | ' ' => up as u8,
        _ => b' ',
    }
}

fn ident_to_index(c: u8) -> u8 {
    match c {
        b'A'..=b'Z' => c - b'A' + 1,
        b'0'..=b'9' => c - b'0' + 48,
        _ => 32, // space
    }
}
