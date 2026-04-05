//! Prost message definitions generated from `proto/graywolf.proto`.
//!
//! The actual types are produced by `prost-build` at compile time (see
//! `build.rs`) and included here so the rest of the crate can refer to them
//! via `crate::ipc::proto::*`.

include!(concat!(env!("OUT_DIR"), "/graywolf.rs"));

// Ergonomic constructors for the most common outbound (Rust → Go) envelopes.
impl IpcMessage {
    pub fn received_frame(f: ReceivedFrame) -> Self {
        Self { payload: Some(ipc_message::Payload::ReceivedFrame(f)) }
    }
    pub fn dcd_change(c: DcdChange) -> Self {
        Self { payload: Some(ipc_message::Payload::DcdChange(c)) }
    }
    pub fn status_update(s: StatusUpdate) -> Self {
        Self { payload: Some(ipc_message::Payload::StatusUpdate(s)) }
    }
    pub fn modem_ready(r: ModemReady) -> Self {
        Self { payload: Some(ipc_message::Payload::ModemReady(r)) }
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use prost::Message;

    #[test]
    fn round_trip_received_frame() {
        let original = IpcMessage::received_frame(ReceivedFrame {
            channel: 1,
            subchan: 0,
            slice: 2,
            data: vec![0xAA, 0xBB, 0xCC],
            quality: 85,
            audio_level_mark: 0.5,
            audio_level_space: 0.6,
            speed_error: 0.1,
            retry: "none".into(),
            timestamp_ns: 1_700_000_000_000_000_000,
        });
        let mut buf = Vec::new();
        original.encode(&mut buf).unwrap();
        let decoded = IpcMessage::decode(&buf[..]).unwrap();
        assert_eq!(original, decoded);
    }

    #[test]
    fn round_trip_configure_channel() {
        let original = IpcMessage {
            payload: Some(ipc_message::Payload::ConfigureChannel(ConfigureChannel {
                channel: 0,
                device_id: 0,
                audio_channel: 0,
                baud: 1200,
                mark_freq: 1200,
                space_freq: 2200,
                modem_type: "afsk".into(),
                profile: "A".into(),
                num_slicers: 1,
                fix_bits: "none".into(),
            })),
        };
        let mut buf = Vec::new();
        original.encode(&mut buf).unwrap();
        let decoded = IpcMessage::decode(&buf[..]).unwrap();
        assert_eq!(original, decoded);
    }

    #[test]
    fn oneof_tags_are_distinct() {
        // Catches accidental tag collisions after refactoring.
        let msgs = vec![
            IpcMessage::received_frame(Default::default()),
            IpcMessage::dcd_change(Default::default()),
            IpcMessage::status_update(Default::default()),
            IpcMessage::modem_ready(Default::default()),
        ];
        for m in msgs {
            let mut buf = Vec::new();
            m.encode(&mut buf).unwrap();
            assert_eq!(IpcMessage::decode(&buf[..]).unwrap(), m);
        }
    }
}
