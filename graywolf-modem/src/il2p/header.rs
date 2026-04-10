//! IL2P header encoding/decoding.
//!
//! The IL2P header is 13 bytes that encode AX.25 address fields, control,
//! PID, and payload length in a compact binary format. It is RS-encoded
//! to 15 bytes (13 data + 2 parity) for transmission.
//!
//! Byte layout (direwolf-compatible):
//!   Bits 0-5 of bytes 0-11: callsign chars in DEC SIXBIT
//!     - bytes 0-5: destination, bytes 6-11: source
//!   Bit 6 of bytes 0-11: UI(1) + PID(4) + Control(7)
//!   Bit 7 of bytes 0-11: FEC(1) + HdrType(1) + PayloadLen(10)
//!   Byte 12: dest_ssid(hi nibble) | src_ssid(lo nibble)

use super::rs_il2p;

// DEC SIXBIT conversions
fn ascii_to_sixbit(a: u8) -> u8 {
    if a >= b' ' && a <= b'_' { a - b' ' } else { 31 }
}

fn sixbit_to_ascii(s: u8) -> u8 {
    s + b' '
}

// Encode an AX.25 PID into IL2P 4-bit PID field. Returns None if not representable.
fn encode_pid(pid: u8) -> Option<u8> {
    match pid {
        p if (p & 0x30) == 0x20 => Some(0x2), // AX.25 Layer 3
        p if (p & 0x30) == 0x10 => Some(0x2),
        0x01 => Some(0x3), // ISO 8208
        0x06 => Some(0x4), // Compressed TCP/IP
        0x07 => Some(0x5), // Uncompressed TCP/IP
        0x08 => Some(0x6), // Segmentation fragment
        0xcc => Some(0xb), // ARPA Internet Protocol
        0xcd => Some(0xc), // ARPA Address Resolution
        0xce => Some(0xd), // FlexNet
        0xcf => Some(0xe), // TheNET
        0xf0 => Some(0xf), // No L3
        _ => None,
    }
}

// Decode IL2P 4-bit PID to AX.25 8-bit PID.
fn decode_pid(pid: u8) -> u8 {
    const AXPID: [u8; 16] = [
        0xf0, 0xf0, 0x20, 0x01, 0x06, 0x07, 0x08, 0xf0,
        0xf0, 0xf0, 0xf0, 0xcc, 0xcd, 0xce, 0xcf, 0xf0,
    ];
    AXPID[(pid & 0x0f) as usize]
}

// Set a multi-bit field in the header, one bit per byte at the given bit position.
// value LSB goes into hdr[lsb_index], next bit into hdr[lsb_index-1], etc.
fn set_field(hdr: &mut [u8; 13], bit_num: u8, lsb_index: usize, width: usize, value: u32) {
    let mut val = value;
    let mut idx = lsb_index;
    for _ in 0..width {
        if val & 1 != 0 {
            hdr[idx] |= 1 << bit_num;
        }
        val >>= 1;
        if idx == 0 { break; }
        idx -= 1;
    }
}

// Extract a multi-bit field from the header.
fn get_field(hdr: &[u8; 13], bit_num: u8, lsb_index: usize, width: usize) -> u32 {
    let mut result = 0u32;
    let msb_index = lsb_index - (width - 1);
    for i in 0..width {
        result <<= 1;
        if hdr[msb_index + i] & (1 << bit_num) != 0 {
            result |= 1;
        }
    }
    result
}

/// IL2P header (decoded to AX.25-level fields).
#[derive(Clone, Debug)]
pub struct Il2pHeader {
    /// Destination callsign (up to 6 ASCII chars, space-padded).
    pub dest_call: [u8; 6],
    /// Destination SSID (0-15).
    pub dest_ssid: u8,
    /// Source callsign (up to 6 ASCII chars, space-padded).
    pub src_call: [u8; 6],
    /// Source SSID (0-15).
    pub src_ssid: u8,
    /// AX.25 control field.
    pub control: u8,
    /// AX.25 PID field.
    pub pid: u8,
    /// Payload length (0-1023).
    pub payload_len: usize,
    /// Offset into the original AX.25 frame where info field begins.
    pub ax25_info_offset: usize,
    /// UI frame flag.
    pub is_ui: bool,
    /// Command/Response bits.
    pub cr_dest: bool,
    pub cr_src: bool,
    /// Header type (0 = transparent, 1 = compact).
    pub hdr_type: u8,
    /// Max FEC flag.
    pub max_fec: bool,
    /// IL2P-encoded control field (7 bits).
    il2p_control: u8,
    /// IL2P-encoded PID field (4 bits).
    il2p_pid: u8,
}

impl Il2pHeader {
    /// Build from an AX.25 frame (without FCS).
    /// Returns None if the frame can't be encoded as type 1 header.
    pub fn from_ax25(ax25: &[u8]) -> Option<Self> {
        if ax25.len() < 15 {
            return None; // minimum: dest(7) + src(7) + ctrl(1)
        }

        // Parse destination address
        let mut dest_call = [b' '; 6];
        for i in 0..6 {
            dest_call[i] = ax25[i] >> 1;
        }
        let dest_ssid = (ax25[6] >> 1) & 0x0F;
        let cr_dest = (ax25[6] & 0x80) != 0;

        // Parse source address
        let mut src_call = [b' '; 6];
        for i in 0..6 {
            src_call[i] = ax25[7 + i] >> 1;
        }
        let src_ssid = (ax25[13] >> 1) & 0x0F;
        let cr_src = (ax25[13] & 0x80) != 0;
        let last_addr = (ax25[13] & 0x01) != 0;

        // Type 1 headers only support exactly 2 addresses
        if !last_addr {
            return None;
        }

        // Validate callsign chars are DEC SIXBIT representable
        for &c in dest_call.iter().chain(src_call.iter()) {
            if c < b' ' || c > b'_' {
                return None;
            }
        }

        let addr_end = 14;
        let control = if addr_end < ax25.len() { ax25[addr_end] } else { return None; };

        // Determine frame type from AX.25 control byte
        let (_il2p_ui, il2p_pid, il2p_control, is_ui, ax25_pid) =
            classify_ax25_control(control, ax25, addr_end, cr_dest)?;

        let info_offset = if is_ui || (control & 0x01) == 0 {
            // UI frames and I frames have a PID byte
            addr_end + 2
        } else {
            // S frames and non-UI U frames have no PID byte
            addr_end + 1
        };
        let payload_len = if info_offset <= ax25.len() {
            ax25.len() - info_offset
        } else {
            0
        };

        Some(Il2pHeader {
            dest_call,
            dest_ssid,
            src_call,
            src_ssid,
            control,
            pid: ax25_pid,
            payload_len,
            ax25_info_offset: info_offset,
            is_ui,
            cr_dest,
            cr_src,
            hdr_type: 1,
            max_fec: false,
            il2p_control,
            il2p_pid,
        })
    }

    /// Reconstruct AX.25 frame header (addresses + control + PID).
    pub fn to_ax25(&self) -> Option<Vec<u8>> {
        let mut frame = Vec::with_capacity(16 + self.payload_len);

        // Destination address
        for i in 0..6 {
            frame.push(self.dest_call[i] << 1);
        }
        let mut dest_ssid_byte = (self.dest_ssid << 1) | 0x60;
        if self.cr_dest {
            dest_ssid_byte |= 0x80;
        }
        frame.push(dest_ssid_byte);

        // Source address (with last-address bit set)
        for i in 0..6 {
            frame.push(self.src_call[i] << 1);
        }
        let mut src_ssid_byte = (self.src_ssid << 1) | 0x61; // last address bit set
        if self.cr_src {
            src_ssid_byte |= 0x80;
        }
        frame.push(src_ssid_byte);

        // Control
        frame.push(self.control);

        // PID for I and UI frames
        if self.is_ui || (self.control & 0x01) == 0 {
            frame.push(self.pid);
        }

        Some(frame)
    }

    /// Serialize header to 13 bytes matching direwolf's il2p_type_1_header().
    pub fn to_bytes(&self) -> [u8; 13] {
        let mut hdr = [0u8; 13];

        // Bytes 0-5: destination callsign in DEC SIXBIT (low 6 bits)
        for i in 0..6 {
            hdr[i] = ascii_to_sixbit(self.dest_call[i]);
        }

        // Bytes 6-11: source callsign in DEC SIXBIT (low 6 bits)
        for i in 0..6 {
            hdr[i + 6] = ascii_to_sixbit(self.src_call[i]);
        }

        // Byte 12: dest SSID (high nibble) | src SSID (low nibble)
        hdr[12] = (self.dest_ssid << 4) | (self.src_ssid & 0x0f);

        // Bit 6 fields: UI(1) + PID(4) + Control(7)
        set_field(&mut hdr, 6, 0, 1, self.il2p_ui() as u32);
        set_field(&mut hdr, 6, 4, 4, self.il2p_pid as u32);
        set_field(&mut hdr, 6, 11, 7, self.il2p_control as u32);

        // Bit 7 fields: FEC(1) + HdrType(1) + PayloadLen(10)
        set_field(&mut hdr, 7, 0, 1, self.max_fec as u32);
        set_field(&mut hdr, 7, 1, 1, self.hdr_type as u32);
        set_field(&mut hdr, 7, 11, 10, self.payload_len as u32);

        hdr
    }

    fn il2p_ui(&self) -> bool {
        self.is_ui
    }

    /// Deserialize from 13-byte IL2P header, matching direwolf's
    /// il2p_decode_header_type_1().
    pub fn from_bytes(bytes: &[u8; 13]) -> Self {
        // Extract callsigns from DEC SIXBIT
        let mut dest_call = [b' '; 6];
        for i in 0..6 {
            dest_call[i] = sixbit_to_ascii(bytes[i] & 0x3f);
        }
        let mut src_call = [b' '; 6];
        for i in 0..6 {
            src_call[i] = sixbit_to_ascii(bytes[i + 6] & 0x3f);
        }

        // Byte 12: SSIDs
        let dest_ssid = (bytes[12] >> 4) & 0x0f;
        let src_ssid = bytes[12] & 0x0f;

        // Extract bit-6 fields
        let ui = get_field(bytes, 6, 0, 1);
        let il2p_pid_val = get_field(bytes, 6, 4, 4) as u8;
        let il2p_ctrl = get_field(bytes, 6, 11, 7) as u8;

        // Extract bit-7 fields
        let max_fec = get_field(bytes, 7, 0, 1) != 0;
        let hdr_type = get_field(bytes, 7, 1, 1) as u8;
        let payload_len = get_field(bytes, 7, 11, 10) as usize;

        // Reconstruct AX.25 control and PID from IL2P compact encoding.
        // C bit (command/response) is at bit 2 of il2p_ctrl.
        let cr_dest = (il2p_ctrl & 0x04) != 0;
        let cr_src = !cr_dest; // IL2P assumes opposite

        let (control, pid, is_ui) = decode_il2p_control(ui != 0, il2p_pid_val, il2p_ctrl);

        // info offset: 14 (addresses) + 1 (control) + optional 1 (PID)
        let ax25_info_offset = if is_ui || (control & 0x01) == 0 { 16 } else { 15 };

        Il2pHeader {
            dest_call,
            dest_ssid,
            src_call,
            src_ssid,
            control,
            pid,
            payload_len,
            ax25_info_offset,
            is_ui,
            cr_dest,
            cr_src,
            hdr_type,
            max_fec,
            il2p_control: il2p_ctrl,
            il2p_pid: il2p_pid_val,
        }
    }
}

/// Classify AX.25 control byte into IL2P compact fields.
/// Returns (ui, il2p_pid, il2p_control, is_ui, ax25_pid).
fn classify_ax25_control(
    control: u8, ax25: &[u8], addr_end: usize, cr_dest: bool,
) -> Option<(bool, u8, u8, bool, u8)> {
    let c_bit = if cr_dest { 1u8 } else { 0 };

    // Check frame type from AX.25 control byte
    if control & 0x01 == 0 {
        // I frame: N(S) P/F N(R) 0 (mod 8)
        let ns = (control >> 1) & 0x07;
        let pf = (control >> 4) & 0x01;
        let nr = (control >> 5) & 0x07;
        let ax25_pid = if addr_end + 1 < ax25.len() { ax25[addr_end + 1] } else { 0xf0 };
        let il2p_pid = encode_pid(ax25_pid)?;
        let il2p_ctrl = (pf << 6) | (nr << 3) | ns;
        Some((false, il2p_pid, il2p_ctrl, false, ax25_pid))
    } else if control & 0x02 == 0 {
        // S frame: x x x P/F N(R) S S 0 1
        let ss = (control >> 2) & 0x03;
        let pf = (control >> 4) & 0x01;
        let nr = (control >> 5) & 0x07;
        let il2p_ctrl = (pf << 6) | (nr << 3) | (c_bit << 2) | ss;
        Some((false, 0, il2p_ctrl, false, 0))
    } else {
        // U frame: various
        let pf = (control >> 4) & 0x01;
        let (opcode, is_ui) = match control & 0xEF {
            0x2F => (0, false), // SABM
            0x43 => (1, false), // DISC
            0x0F => (2, false), // DM
            0x63 => (3, false), // UA
            0x87 => (4, false), // FRMR
            0x03 => (5, true),  // UI
            0xAF => (6, false), // XID
            0xE3 => (7, false), // TEST
            0x6F => return None, // SABME — can't represent in type 1
            _ => return None,
        };

        if is_ui {
            let ax25_pid = if addr_end + 1 < ax25.len() { ax25[addr_end + 1] } else { 0xf0 };
            let il2p_pid = encode_pid(ax25_pid)?;
            let il2p_ctrl = (pf << 6) | (opcode << 3) | (c_bit << 2);
            Some((true, il2p_pid, il2p_ctrl, true, ax25_pid))
        } else {
            let il2p_ctrl = (pf << 6) | (opcode << 3) | (c_bit << 2);
            Some((false, 1, il2p_ctrl, false, 0))
        }
    }
}

/// Decode IL2P compact control/PID back to AX.25 control byte and PID.
fn decode_il2p_control(ui: bool, il2p_pid: u8, il2p_ctrl: u8) -> (u8, u8, bool) {
    let pf = (il2p_ctrl >> 6) & 0x01;

    if il2p_pid == 0 {
        // S frame: P/F N(R) C S S
        let ss = il2p_ctrl & 0x03;
        let nr = (il2p_ctrl >> 3) & 0x07;
        // AX.25 S frame control: N(R) P/F S S 0 1
        let control = (nr << 5) | (pf << 4) | (ss << 2) | 0x01;
        (control, 0, false)
    } else if il2p_pid == 1 {
        // U frame (not UI): P/F OPCODE[3] C x x
        let opcode = (il2p_ctrl >> 3) & 0x07;
        let control = match opcode {
            0 => 0x2F | (pf << 4), // SABM
            1 => 0x43 | (pf << 4), // DISC
            2 => 0x0F | (pf << 4), // DM
            3 => 0x63 | (pf << 4), // UA
            4 => 0x87 | (pf << 4), // FRMR
            5 => 0x03 | (pf << 4), // UI (shouldn't happen with pid==1)
            6 => 0xAF | (pf << 4), // XID
            _ => 0xE3 | (pf << 4), // TEST
        };
        (control, 0, false)
    } else if ui {
        // UI frame
        let ax25_pid = decode_pid(il2p_pid);
        let control = 0x03 | (pf << 4); // UI control byte
        (control, ax25_pid, true)
    } else {
        // I frame: P/F N(R) N(S)
        let nr = (il2p_ctrl >> 3) & 0x07;
        let ns = il2p_ctrl & 0x07;
        let ax25_pid = decode_pid(il2p_pid);
        // AX.25 I frame control: N(R) P/F N(S) 0
        let control = (nr << 5) | (pf << 4) | (ns << 1);
        (control, ax25_pid, false)
    }
}

/// Encode an IL2P header with RS parity.
/// Returns 15 bytes (13 data + 2 parity).
pub fn encode_header(hdr: &Il2pHeader) -> Option<Vec<u8>> {
    let data = hdr.to_bytes();
    let parity = rs_il2p::rs_encode_header(&data);
    let mut encoded = Vec::with_capacity(15);
    encoded.extend_from_slice(&data);
    encoded.extend_from_slice(&parity);
    Some(encoded)
}

/// Decode an RS-encoded IL2P header.
/// Input: 15 bytes. Returns decoded header on success.
pub fn decode_header(encoded: &[u8]) -> Option<Il2pHeader> {
    if encoded.len() < 15 {
        return None;
    }

    let mut block = [0u8; 15];
    block.copy_from_slice(&encoded[..15]);

    // RS decode (correct up to 1 error with 2 parity symbols)
    if !rs_il2p::rs_decode_header(&mut block) {
        return None;
    }

    let data: [u8; 13] = block[..13].try_into().unwrap();
    Some(Il2pHeader::from_bytes(&data))
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn header_roundtrip() {
        let hdr = Il2pHeader {
            dest_call: *b"APRS  ",
            dest_ssid: 0,
            src_call: *b"N3LLO ",
            src_ssid: 7,
            control: 0x03,
            pid: 0xF0,
            payload_len: 42,
            ax25_info_offset: 16,
            is_ui: true,
            cr_dest: true,
            cr_src: false,
            hdr_type: 1,
            max_fec: false,
            il2p_control: (5 << 3) | (1 << 2), // UI opcode + C bit
            il2p_pid: 0x0f, // No L3
        };

        let bytes = hdr.to_bytes();
        let recovered = Il2pHeader::from_bytes(&bytes);
        assert_eq!(recovered.dest_call, hdr.dest_call);
        assert_eq!(recovered.dest_ssid, hdr.dest_ssid);
        assert_eq!(recovered.src_call, hdr.src_call);
        assert_eq!(recovered.src_ssid, hdr.src_ssid);
        assert_eq!(recovered.payload_len, hdr.payload_len);
        assert_eq!(recovered.is_ui, hdr.is_ui);
        assert_eq!(recovered.cr_dest, hdr.cr_dest);
        // Control should round-trip through IL2P encoding
        assert_eq!(recovered.pid, 0xF0);
    }

    #[test]
    fn header_encode_decode() {
        let hdr = Il2pHeader {
            dest_call: *b"TEST  ",
            dest_ssid: 3,
            src_call: *b"CALL  ",
            src_ssid: 1,
            control: 0x03,
            pid: 0xF0,
            payload_len: 100,
            ax25_info_offset: 16,
            is_ui: true,
            cr_dest: false,
            cr_src: false,
            hdr_type: 1,
            max_fec: false,
            il2p_control: 5 << 3, // UI opcode, no C bit
            il2p_pid: 0x0f, // No L3
        };

        let encoded = encode_header(&hdr).unwrap();
        assert_eq!(encoded.len(), 15);

        let decoded = decode_header(&encoded).unwrap();
        assert_eq!(decoded.dest_call, hdr.dest_call);
        assert_eq!(decoded.payload_len, hdr.payload_len);
    }

    #[test]
    fn sixbit_encoding() {
        // Space -> 0, 'A' -> 0x21-0x20 = 1, etc.
        assert_eq!(ascii_to_sixbit(b' '), 0);
        assert_eq!(ascii_to_sixbit(b'A'), 0x21);
        assert_eq!(sixbit_to_ascii(0), b' ');
        assert_eq!(sixbit_to_ascii(0x21), b'A');
    }

    #[test]
    fn from_ax25_ui_frame() {
        // Build AX.25 UI frame: CQ-0 > WB2OSZ-0
        let mut ax25 = Vec::new();
        for &c in b"CQ    " { ax25.push(c << 1); }
        ax25.push(0xe0); // SSID 0, C=1, not last — wait, for 2 addresses with last bit:
        // Actually: dest SSID byte with C bit set for command
        ax25[6] = 0xe0; // C=1, SSID=0, RR bits set
        for &c in b"WB2OSZ" { ax25.push(c << 1); }
        ax25.push(0x61); // last address, SSID 0, C=0
        ax25.push(0x03); // UI control
        ax25.push(0xF0); // No L3 PID
        ax25.extend_from_slice(b"test");

        let hdr = Il2pHeader::from_ax25(&ax25).unwrap();
        assert_eq!(&hdr.dest_call, b"CQ    ");
        assert_eq!(&hdr.src_call, b"WB2OSZ");
        assert!(hdr.is_ui);
        assert_eq!(hdr.pid, 0xF0);
        assert_eq!(hdr.payload_len, 4);

        // Roundtrip through to_bytes/from_bytes
        let bytes = hdr.to_bytes();
        let recovered = Il2pHeader::from_bytes(&bytes);
        assert_eq!(recovered.dest_call, hdr.dest_call);
        assert_eq!(recovered.src_call, hdr.src_call);
        assert_eq!(recovered.payload_len, hdr.payload_len);
        assert!(recovered.is_ui);
        assert_eq!(recovered.pid, 0xF0);
    }

    #[test]
    fn pid_encode_decode_roundtrip() {
        // Test all encodable PIDs
        let test_pids: &[u8] = &[0x20, 0xf0, 0x01, 0x06, 0x07, 0x08, 0xcc, 0xcd, 0xce, 0xcf];
        for &pid in test_pids {
            let encoded = encode_pid(pid);
            assert!(encoded.is_some(), "PID 0x{:02x} should be encodable", pid);
            let decoded = decode_pid(encoded.unwrap());
            // Note: some PIDs map to the same IL2P code (e.g., 0x20 and 0x10 both → 2 → 0x20)
            if pid == 0x20 {
                assert_eq!(decoded, 0x20);
            } else {
                assert_eq!(decoded, pid, "PID 0x{:02x} didn't roundtrip", pid);
            }
        }
    }

    #[test]
    fn direwolf_callsign_packing() {
        // Verify 6th character of source callsign is preserved
        let mut ax25 = Vec::new();
        for &c in b"APRS  " { ax25.push(c << 1); }
        ax25.push(0x60); // dest SSID
        for &c in b"WB2OSZ" { ax25.push(c << 1); }
        ax25.push(0x61); // src SSID, last
        ax25.push(0x03); // UI
        ax25.push(0xF0); // PID

        let hdr = Il2pHeader::from_ax25(&ax25).unwrap();
        let bytes = hdr.to_bytes();
        let recovered = Il2pHeader::from_bytes(&bytes);
        assert_eq!(recovered.src_call, *b"WB2OSZ");
    }
}
