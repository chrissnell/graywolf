//! IPC between the Rust modem and the Go application.
//!
//! Transport: bidirectional, length-prefixed protobuf frames over a
//! platform-specific stream. Each frame is `[4 bytes big-endian length][IpcMessage bytes]`.
//!
//! - Unix: Unix domain socket at a caller-chosen path.
//! - Windows: TCP on 127.0.0.1 (loopback only).
//!
//! The protobuf schema is `proto/graywolf.proto`. Rust message types are
//! defined with `prost` derive in [`proto`] so that we do not take a build
//! dependency on `protoc`. The struct layout, field numbers, wire types, and
//! `oneof` tags match `graywolf.proto` exactly — if one changes, the other
//! must change in lockstep. A schema conformance test lives in `tests.rs`.

pub mod proto;
pub mod server;
pub mod framing;

pub use proto::*;
pub use server::{IpcServer, IpcInbound, IpcHandle};
