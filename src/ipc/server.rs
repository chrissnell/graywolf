//! IPC server for the modem process.
//!
//! On Unix: Unix domain socket at a caller-chosen path.
//! On Windows: TCP on 127.0.0.1 (loopback only), port chosen by the OS.
//!
//! Lifecycle:
//!  1. [`IpcServer::bind`] creates the listener and writes a readiness signal
//!     to stdout — the Go parent waits on this before connecting.
//!     - Unix: writes `\n` (the parent already knows the socket path).
//!     - Windows: writes `<port>\n` so the parent knows where to dial.
//!  2. [`IpcServer::accept`] blocks until the Go client connects, then sends
//!     a `ModemReady` message.
//!  3. It returns an [`IpcHandle`] (for thread-safe sends) and a
//!     `Receiver<IpcInbound>` that a reader thread fills with incoming
//!     messages until the peer closes or a read error occurs.
//!
//! Only a single client is supported — if one disconnects the server should
//! be torn down and the modem process exits (the Go side will relaunch us).

use std::io::{self, Write};
use std::sync::mpsc::{self, Receiver, Sender};
use std::sync::{Arc, Mutex};
use std::thread;

#[cfg(unix)]
use std::fs;
#[cfg(unix)]
use std::os::unix::net::{UnixListener, UnixStream};
#[cfg(unix)]
use std::path::{Path, PathBuf};

#[cfg(windows)]
use std::net::{TcpListener, TcpStream};

use super::framing::{read_frame, write_frame};
use super::proto::{IpcMessage, ModemReady};

// Platform type aliases — both implement Read + Write + try_clone + shutdown.
#[cfg(unix)]
type IpcListener = UnixListener;
#[cfg(unix)]
type IpcStream = UnixStream;

#[cfg(windows)]
type IpcListener = TcpListener;
#[cfg(windows)]
type IpcStream = TcpStream;

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
    stream: Arc<Mutex<IpcStream>>,
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

    /// Construct a handle backed by one end of a local stream pair, for
    /// unit tests that need a `Modem` without a live peer.
    ///
    /// WARNING: the caller receives the other end and must keep it alive
    /// for the duration of the test. Dropping it closes the far side, and
    /// any code path under test that calls [`IpcHandle::send`] (directly
    /// or indirectly) will then fail with `EPIPE`. Current tests only
    /// exercise early-return paths that never send; new tests must either
    /// preserve the peer or deliberately tolerate send failures.
    #[cfg(test)]
    pub(crate) fn test_pair() -> (Self, IpcStream) {
        #[cfg(unix)]
        {
            let (a, b) = UnixStream::pair().expect("UnixStream::pair");
            (
                Self {
                    stream: Arc::new(Mutex::new(a)),
                },
                b,
            )
        }
        #[cfg(windows)]
        {
            let listener = TcpListener::bind("127.0.0.1:0").expect("bind");
            let addr = listener.local_addr().expect("local_addr");
            let client = TcpStream::connect(addr).expect("connect");
            let (server_stream, _) = listener.accept().expect("accept");
            (
                Self {
                    stream: Arc::new(Mutex::new(server_stream)),
                },
                client,
            )
        }
    }
}

pub struct IpcServer {
    #[cfg(unix)]
    socket_path: PathBuf,
    listener: IpcListener,
}

impl IpcServer {
    /// Bind a Unix socket at `path`, removing any stale file first. Emits the
    /// readiness byte to stdout as soon as the listener is ready.
    #[cfg(unix)]
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

    /// Bind a TCP listener on 127.0.0.1 with an OS-assigned port. Writes
    /// `<port>\n` to stdout so the Go parent knows where to connect.
    #[cfg(windows)]
    pub fn bind() -> io::Result<Self> {
        let listener = TcpListener::bind("127.0.0.1:0")?;
        let port = listener.local_addr()?.port();

        {
            let stdout = io::stdout();
            let mut lock = stdout.lock();
            write!(lock, "{}\n", port)?;
            lock.flush()?;
        }

        Ok(Self { listener })
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

    #[cfg(unix)]
    pub fn socket_path(&self) -> &Path {
        &self.socket_path
    }
}

fn reader_loop(mut stream: IpcStream, tx: Sender<IpcInbound>) {
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

#[cfg(unix)]
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

    /// Connect to a test server. On Unix uses UDS, on Windows uses TCP.
    #[cfg(unix)]
    fn test_connect(server: &IpcServer) -> IpcStream {
        // Poll for the socket file to appear.
        for _ in 0..50 {
            if server.socket_path().exists() {
                break;
            }
            thread::sleep(Duration::from_millis(20));
        }
        UnixStream::connect(server.socket_path()).unwrap()
    }

    #[cfg(windows)]
    fn test_connect(server: &IpcServer) -> IpcStream {
        TcpStream::connect(server.listener.local_addr().unwrap()).unwrap()
    }

    #[test]
    fn end_to_end_local_socket() {
        #[cfg(unix)]
        let tmp = std::env::temp_dir()
            .join(format!("graywolf-test-{}.sock", std::process::id()));
        #[cfg(unix)]
        let _ = fs::remove_file(&tmp);

        // Build the server on the main thread so test_connect can reference it.
        // We pass it to the server thread via Arc since IpcServer isn't Clone.
        #[cfg(unix)]
        let server = Arc::new(IpcServer::bind(&tmp).unwrap());
        #[cfg(windows)]
        let server = Arc::new(IpcServer::bind().unwrap());

        let server2 = Arc::clone(&server);
        let server_thread = thread::spawn(move || {
            let (handle, rx, _join) = server2.accept().unwrap();
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

        let mut client = test_connect(&server);

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
                gain_db: 0.0,
            })),
        };
        write_frame(&mut client, &cfg).unwrap();

        // Expect status_update back.
        let resp = read_frame(&mut client).unwrap().unwrap();
        assert!(matches!(resp.payload, Some(Payload::StatusUpdate(_))));

        server_thread.join().unwrap();

        #[cfg(unix)]
        let _ = fs::remove_file(&tmp);

        // Suppress unused import warning from StartAudio in some configs.
        let _ = StartAudio {};
    }
}
