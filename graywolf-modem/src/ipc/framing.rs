//! Length-prefixed protobuf framing over an arbitrary byte stream.
//!
//! Frame: `[4 bytes big-endian length N][N bytes IpcMessage payload]`.
//! Max frame size is bounded to guard against resource exhaustion.

use std::io::{self, Read, Write};

use prost::Message;

use super::proto::IpcMessage;

/// Maximum allowable frame payload size (bytes). AX.25 frames top out around
/// 2 KB; 64 KB gives generous headroom for future message growth.
pub const MAX_FRAME_SIZE: usize = 64 * 1024;

/// Write a single `IpcMessage` to `w` using length-prefixed framing.
pub fn write_frame<W: Write>(w: &mut W, msg: &IpcMessage) -> io::Result<()> {
    let mut buf = Vec::with_capacity(msg.encoded_len());
    msg.encode(&mut buf)
        .map_err(|e| io::Error::new(io::ErrorKind::Other, e))?;
    if buf.len() > MAX_FRAME_SIZE {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            format!("frame too large: {} > {}", buf.len(), MAX_FRAME_SIZE),
        ));
    }
    let len = (buf.len() as u32).to_be_bytes();
    w.write_all(&len)?;
    w.write_all(&buf)?;
    Ok(())
}

/// Read a single `IpcMessage` from `r`. Returns `Ok(None)` on clean EOF before
/// any bytes are read.
pub fn read_frame<R: Read>(r: &mut R) -> io::Result<Option<IpcMessage>> {
    let mut len_buf = [0u8; 4];
    match r.read_exact(&mut len_buf) {
        Ok(()) => {}
        Err(e) if e.kind() == io::ErrorKind::UnexpectedEof => return Ok(None),
        Err(e) => return Err(e),
    }
    let len = u32::from_be_bytes(len_buf) as usize;
    if len > MAX_FRAME_SIZE {
        return Err(io::Error::new(
            io::ErrorKind::InvalidData,
            format!("frame too large: {} > {}", len, MAX_FRAME_SIZE),
        ));
    }
    let mut buf = vec![0u8; len];
    r.read_exact(&mut buf)?;
    let msg = IpcMessage::decode(&buf[..])
        .map_err(|e| io::Error::new(io::ErrorKind::InvalidData, e))?;
    Ok(Some(msg))
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ipc::proto::{ModemReady, ReceivedFrame};
    use std::io::Cursor;

    #[test]
    fn round_trip_single_frame() {
        let msg = IpcMessage::modem_ready(ModemReady {
            version: "test".into(),
            pid: 1234,
        });
        let mut buf = Vec::new();
        write_frame(&mut buf, &msg).unwrap();
        let mut cur = Cursor::new(buf);
        let decoded = read_frame(&mut cur).unwrap().unwrap();
        assert_eq!(decoded, msg);
    }

    #[test]
    fn round_trip_multiple_frames() {
        let msgs = vec![
            IpcMessage::modem_ready(ModemReady { version: "v1".into(), pid: 1 }),
            IpcMessage::received_frame(ReceivedFrame {
                channel: 0,
                data: vec![1, 2, 3],
                retry: "none".into(),
                ..Default::default()
            }),
        ];
        let mut buf = Vec::new();
        for m in &msgs {
            write_frame(&mut buf, m).unwrap();
        }
        let mut cur = Cursor::new(buf);
        for m in &msgs {
            let decoded = read_frame(&mut cur).unwrap().unwrap();
            assert_eq!(&decoded, m);
        }
        assert!(read_frame(&mut cur).unwrap().is_none());
    }

    #[test]
    fn rejects_oversized_frame() {
        let mut buf = Vec::new();
        buf.extend_from_slice(&(MAX_FRAME_SIZE as u32 + 1).to_be_bytes());
        let mut cur = Cursor::new(buf);
        assert!(read_frame(&mut cur).is_err());
    }
}
