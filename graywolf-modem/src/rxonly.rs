//! Reusable RX-only glue: feed audio chunks into a `MultiAfskDemodulator`
//! and turn drained `DecodedFrame`s into human-readable AX.25 strings.
//!
//! This module is called by the POC-A binary (`bin/poc_a_rxonly.rs`) and is
//! kept independent of the production `Modem` IPC state machine on purpose:
//! the POC needs the smallest possible RX path, and the production modem
//! has level metering, gain, and IPC concerns that don't belong here.

use crate::demod_afsk_multi::MultiAfskDemodulator;
use crate::hdlc::DecodedFrame;

/// Push one chunk of mono i16 audio samples through the demodulator and
/// return any frames that fell out as a side effect.
pub fn feed_chunk(demod: &mut MultiAfskDemodulator, chunk: &[i16]) -> Vec<DecodedFrame> {
    for &s in chunk {
        demod.process_sample(s as i32);
    }
    demod.take_frames()
}

/// Render the bytes of a successfully-decoded AX.25 v2.0 UI frame as
/// `<src>><dest>[,via1[*],via2[*],...]:<info>`. Returns `None` if the
/// frame is too short or fails the address-byte sanity checks (low bit of
/// every non-final address byte must be 0; bit 7 of the last byte must be 1).
///
/// This is the smallest correct decoder: callsign characters live in the
/// upper 7 bits of bytes 0..6, SSID in bits 1..4 of byte 6, the
/// "has been repeated" H bit in bit 7 of byte 6 (digipeater entries only),
/// and the address-extension bit in bit 0 of byte 6.
pub fn format_ax25_ui_frame(data: &[u8]) -> Option<String> {
    // AX.25 v2.0 UI frame layout (FCS already stripped by the HDLC decoder):
    //   addresses: 2..N x 7 bytes (dest, src, optional digi path)
    //   control:   1 byte  (0x03 for UI)
    //   PID:       1 byte  (0xf0 for no L3)
    //   info:      remainder
    if data.len() < 7 * 2 + 2 {
        return None;
    }

    // Walk address fields until we hit the one with bit 0 set ("last addr").
    let mut addrs: Vec<(String, u8, bool)> = Vec::new(); // (call, ssid, h_bit)
    let mut i = 0;
    loop {
        if i + 7 > data.len() {
            return None;
        }
        let chunk = &data[i..i + 7];
        let mut call = String::with_capacity(6);
        for &b in &chunk[0..6] {
            // Low bit is the address-extension flag; chars are in upper 7 bits.
            if b & 0x01 != 0 {
                return None; // malformed: extension bit set on non-final byte
            }
            let c = (b >> 1) as char;
            if c != ' ' {
                if !c.is_ascii_alphanumeric() {
                    return None;
                }
                call.push(c);
            }
        }
        let ssid_byte = chunk[6];
        let ssid = (ssid_byte >> 1) & 0x0f;
        let h_bit = ssid_byte & 0b1000_0000 != 0;
        let last = ssid_byte & 0x01 != 0;
        addrs.push((call, ssid, h_bit));
        i += 7;
        if last { break; }
        if addrs.len() > 10 {
            // AX.25 v2.0 caps the digi list at 8; refuse anything pathological.
            return None;
        }
    }

    if addrs.len() < 2 || i + 2 > data.len() {
        return None;
    }
    let info = &data[i + 2..]; // skip control + pid
    // Render. dest is addrs[0], src is addrs[1], remainder are digis.
    let fmt_addr = |c: &str, ssid: u8| -> String {
        if ssid == 0 { c.to_string() } else { format!("{}-{}", c, ssid) }
    };
    let dest = fmt_addr(&addrs[0].0, addrs[0].1);
    let src = fmt_addr(&addrs[1].0, addrs[1].1);
    let mut s = format!("{}>{}", src, dest);
    for d in &addrs[2..] {
        s.push(',');
        s.push_str(&fmt_addr(&d.0, d.1));
        if d.2 { s.push('*'); } // "has been repeated" marker
    }
    s.push(':');
    // Info field may contain non-printable bytes (Mic-E, compressed pos).
    // Render as lossy UTF-8 so the line stays one printable line; the goal
    // is operator-readable triage, not byte-perfect roundtrip.
    s.push_str(&String::from_utf8_lossy(info));
    Some(s)
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::demod_afsk_multi::{MultiAfskDemodulator, RECOMMENDED_3DEMOD};

    /// Hand-built minimal UI frame: src=N0CALL-1, dest=APRS-0, no path,
    /// control=0x03 (UI), pid=0xF0 (no L3), info="hi". Each callsign byte
    /// is left-shifted by one (low bit reserved for the address-extension
    /// flag); the SSID byte carries the SSID in bits 1..4 plus the
    /// extension bit in bit 0 of the final address byte.
    fn make_ui_frame(src: &str, src_ssid: u8, dest: &str, dest_ssid: u8, info: &[u8]) -> Vec<u8> {
        fn pack_addr(call: &str, ssid: u8, last: bool, h_bit: bool) -> [u8; 7] {
            let mut out = [b' ' << 1; 7];
            for (i, c) in call.bytes().take(6).enumerate() {
                out[i] = c.to_ascii_uppercase() << 1;
            }
            // SSID byte layout: H | R | R | SSID(4) | ext
            // R bits stay 1 per AX.25 spec (reserved, not negotiated).
            let mut s = 0b0110_0000;
            s |= (ssid & 0x0f) << 1;
            if h_bit { s |= 0b1000_0000; }
            if last { s |= 0b0000_0001; }
            out[6] = s;
            out
        }
        let mut frame = Vec::new();
        frame.extend_from_slice(&pack_addr(dest, dest_ssid, false, false));
        frame.extend_from_slice(&pack_addr(src,  src_ssid,  true,  false));
        frame.push(0x03); // UI control
        frame.push(0xf0); // PID: no layer 3
        frame.extend_from_slice(info);
        frame
    }

    #[test]
    fn format_handles_no_path_ui_frame() {
        let f = make_ui_frame("N0CALL", 1, "APRS", 0, b"hi");
        let s = format_ax25_ui_frame(&f).expect("format must succeed");
        assert_eq!(s, "N0CALL-1>APRS:hi");
    }

    #[test]
    fn format_handles_digipeater_path_with_h_bit() {
        // dest=APRS-0, src=W1AW-0, via WIDE1-1* (already repeated), WIDE2-2.
        fn pack_with_h(call: &str, ssid: u8, last: bool, h: bool) -> [u8; 7] {
            let mut out = [b' ' << 1; 7];
            for (i, c) in call.bytes().take(6).enumerate() {
                out[i] = c.to_ascii_uppercase() << 1;
            }
            let mut s = 0b0110_0000;
            s |= (ssid & 0x0f) << 1;
            if h { s |= 0b1000_0000; }
            if last { s |= 0b0000_0001; }
            out[6] = s;
            out
        }
        let mut frame = Vec::new();
        frame.extend_from_slice(&pack_with_h("APRS",  0, false, false));
        frame.extend_from_slice(&pack_with_h("W1AW",  0, false, false));
        frame.extend_from_slice(&pack_with_h("WIDE1", 1, false, true));
        frame.extend_from_slice(&pack_with_h("WIDE2", 2, true,  false));
        frame.push(0x03);
        frame.push(0xf0);
        frame.extend_from_slice(b"!4500.00N/07300.00W>");
        let s = format_ax25_ui_frame(&frame).unwrap();
        assert_eq!(s, "W1AW>APRS,WIDE1-1*,WIDE2-2:!4500.00N/07300.00W>");
    }

    #[test]
    fn format_returns_none_on_runt_frame() {
        assert!(format_ax25_ui_frame(&[]).is_none());
        assert!(format_ax25_ui_frame(&[0; 5]).is_none());
    }

    /// End-to-end: feed the first known-decodable FLAC track through a
    /// triple-demod ensemble and assert at least 1 frame falls out.
    /// Track is 2-channel 16-bit 44.1 kHz; we extract channel 0 to mono.
    #[test]
    fn feed_chunk_decodes_at_least_one_frame_from_reference_track() {
        use claxon::FlacReader;
        let path = "aprs-test-tracks/01_40-Mins-Traffic -on-144.39.flac";
        let mut reader = FlacReader::open(path).expect("open reference flac");
        let info = reader.streaminfo();
        let sr = info.sample_rate;
        let channels = info.channels as usize;
        let bits = info.bits_per_sample;
        let mut samples: Vec<i16> = Vec::new();
        for (idx, s) in reader.samples().enumerate() {
            let raw = s.unwrap();
            let scaled = if bits > 16 {
                (raw >> (bits - 16)) as i16
            } else if bits < 16 {
                (raw << (16 - bits)) as i16
            } else {
                raw as i16
            };
            if channels == 1 || idx % channels == 0 {
                samples.push(scaled);
            }
            if samples.len() >= sr as usize * 60 { break; } // first 60 s is plenty.
        }
        let mut demod = MultiAfskDemodulator::new(sr, 1200, 1200, 2200, 0, &RECOMMENDED_3DEMOD);
        let mut total = 0usize;
        for chunk in samples.chunks(960) {
            total += feed_chunk(&mut demod, chunk).len();
        }
        assert!(total >= 1, "expected >=1 frame from reference track, got {}", total);
    }
}
