//! Unix domain socket server for the modem IPC.
//!
//! Lifecycle:
//!  1. [`IpcServer::bind`] creates and listens on the socket, then writes `\n`
//!     to stdout — the readiness signal the Go parent waits on.
//!  2. [`IpcServer::accept`] blocks until the Go client connects, then sends
//!     a `ModemReady` message.
//!  3. It returns an [`IpcHandle`] (for thread-safe sends) and a
//!     `Receiver<IpcInbound>` that a reader thread fills with incoming
//!     messages until the peer closes or a read error occurs.
//!
//! Only a single client is supported — if one disconnects the server should
//! be torn down and the modem process exits (the Go side will relaunch us).

use std::fs;
use std::io::{self, Write};
use std::os::unix::net::{UnixListener, UnixStream};
use std::path::{Path, PathBuf};
use std::sync::mpsc::{self, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::thread;

use super::framing::{read_frame, write_frame};
use super::proto::{IpcMessage, ModemReady};

/// An inbound IPC message from the Go application, or a termination signal.
pub enum IpcInbound {
    Message(IpcMessage),
    /// Peer closed the socket cleanly.
    Disconnected,
    /// I/O error while reading; the connection is dead.
    ReadError(io::Error),
}

/// Thread-safe sender for outbound IPC messages. Clone to share across
/// threads — writes are serialized by an internal mutex.
#[derive(Clone)]
pub struct IpcHandle {
    stream: Arc<Mutex<UnixStream>>,
}

impl IpcHandle {
    pub fn send(&self, msg: &IpcMessage) -> io::Result<()> {
        let mut guard = self.stream.lock().unwrap();
        write_frame(&mut *guard, msg)
    }

    /// Shutdown the writer half of the socket so that the reader side on the
    /// peer observes EOF. Called during graceful shutdown after the final
    /// `StatusUpdate` is sent.
    pub fn shutdown_write(&self) -> io::Result<()> {
        let guard = self.stream.lock().unwrap();
        guard.shutdown(std::net::Shutdown::Write)
    }
}

pub struct IpcServer {
    socket_path: PathBuf,
    listener: UnixListener,
}

impl IpcServer {
    /// Bind a Unix socket at `path`, removing any stale file first. Emits the
    /// readiness byte to stdout as soon as the listener is ready.
    pub fn bind<P: AsRef<Path>>(path: P) -> io::Result<Self> {
        let socket_path = path.as_ref().to_path_buf();
        if socket_path.exists() {
            fs::remove_file(&socket_path)?;
        }
        let listener = UnixListener::bind(&socket_path)?;

        // Readiness handshake: Go parent reads exactly one byte from our
        // stdout pipe to know the socket is accepting connections.
        {
            let stdout = io::stdout();
            let mut lock = stdout.lock();
            lock.write_all(b"\n")?;
            lock.flush()?;
        }

        Ok(Self { socket_path, listener })
    }

    /// Block until the Go client connects, send `ModemReady`, and spawn a
    /// reader thread. Returns the send-handle and an inbound receiver.
    /// The `IpcServer` is kept alive by the caller (or dropped) so that the
    /// socket file is cleaned up on exit.
    pub fn accept(
        &self,
    ) -> io::Result<(IpcHandle, Receiver<IpcInbound>, thread::JoinHandle<()>)> {
        let (stream, _addr) = self.listener.accept()?;

        let reader_stream = stream.try_clone()?;
        let handle = IpcHandle { stream: Arc::new(Mutex::new(stream)) };

        // Send ModemReady immediately so the Go side knows IPC is live.
        let ready = IpcMessage::modem_ready(ModemReady {
            version: env!("CARGO_PKG_VERSION").to_string(),
            pid: std::process::id() as u64,
        });
        handle.send(&ready)?;

        let (tx, rx): (Sender<IpcInbound>, Receiver<IpcInbound>) = mpsc::channel();
        let join = thread::Builder::new()
            .name("ipc-reader".into())
            .spawn(move || reader_loop(reader_stream, tx))
            .expect("failed to spawn ipc reader thread");

        Ok((handle, rx, join))
    }

    pub fn socket_path(&self) -> &Path {
        &self.socket_path
    }
}

fn reader_loop(mut stream: UnixStream, tx: Sender<IpcInbound>) {
    loop {
        match read_frame(&mut stream) {
            Ok(Some(msg)) => {
                if tx.send(IpcInbound::Message(msg)).is_err() {
                    return; // main thread dropped the receiver
                }
            }
            Ok(None) => {
                let _ = tx.send(IpcInbound::Disconnected);
                return;
            }
            Err(e) => {
                let _ = tx.send(IpcInbound::ReadError(e));
                return;
            }
        }
    }
}

impl Drop for IpcServer {
    fn drop(&mut self) {
        let _ = fs::remove_file(&self.socket_path);
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::ipc::proto::{ipc_message::Payload, ConfigureAudio, StartAudio};
    use std::time::Duration;

    #[test]
    fn end_to_end_local_socket() {
        let tmp = std::env::temp_dir().join(format!("graywolf-test-{}.sock", std::process::id()));
        let _ = fs::remove_file(&tmp);

        let tmp_clone = tmp.clone();
        let server_thread = thread::spawn(move || {
            let server = IpcServer::bind(&tmp_clone).unwrap();
            let (handle, rx, _join) = server.accept().unwrap();
            // Echo: first message in → echo a StatusUpdate back.
            if let Ok(IpcInbound::Message(m)) = rx.recv_timeout(Duration::from_secs(2)) {
                match m.payload {
                    Some(Payload::ConfigureAudio(_)) => {
                        handle
                            .send(&IpcMessage::status_update(Default::default()))
                            .unwrap();
                    }
                    _ => panic!("unexpected message"),
                }
            } else {
                panic!("no message received");
            }
        });

        // Wait for socket file to exist.
        for _ in 0..50 {
            if tmp.exists() {
                break;
            }
            thread::sleep(Duration::from_millis(20));
        }
        let mut client = UnixStream::connect(&tmp).unwrap();

        // Server should have sent ModemReady first.
        let ready = read_frame(&mut client).unwrap().unwrap();
        assert!(matches!(ready.payload, Some(Payload::ModemReady(_))));

        // Send ConfigureAudio.
        let cfg = IpcMessage {
            payload: Some(Payload::ConfigureAudio(ConfigureAudio {
                device_id: 0,
                device_name: "stdin".into(),
                sample_rate: 44100,
                channels: 1,
                source_type: "stdin".into(),
                format: "s16le".into(),
            })),
        };
        write_frame(&mut client, &cfg).unwrap();

        // Expect status_update back.
        let resp = read_frame(&mut client).unwrap().unwrap();
        assert!(matches!(resp.payload, Some(Payload::StatusUpdate(_))));

        server_thread.join().unwrap();
        let _ = fs::remove_file(&tmp);
        // Suppress unused import warning from StartAudio in some configs.
        let _ = StartAudio {};
    }
}
