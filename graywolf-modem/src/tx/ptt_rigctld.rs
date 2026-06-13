//! Hamlib `rigctld` TCP PTT driver.
//!
//! Commands a radio's PTT over CAT via a running `rigctld` (the hamlib
//! daemon). The protocol is line-based ASCII over TCP:
//! - `T 1\n` / `T 0\n` → set PTT on/off. Response: `RPRT <n>\n` where
//!   `n == 0` means success, non-zero is a hamlib error code.
//! - `t\n`             → get PTT. Per the rigctld protocol a successful
//!   GET returns ONLY the value — a single line `0` or `1`, with NO
//!   trailing `RPRT 0`. An error returns `RPRT <n>\n` instead. (Reading
//!   `t` as two lines was the GRA-73 bug: against a healthy daemon the
//!   second read blocked until timeout, the connect handshake failed,
//!   and PTT silently never keyed.)
//!
//! ## Why a dedicated driver
//!
//! Unlike serial RTS/DTR or CM108 GPIO, rigctld is a networked daemon
//! that can die, drop connections, or fail to reach the rig. This driver
//! owns a persistent `TcpStream`, auto-reconnects on I/O failure, and
//! treats unkey as safety-critical — a stuck-keyed radio is materially
//! worse than a failed key, so unkey gets a bounded retry loop before
//! falling back to closing the socket (the close signals rigctld to
//! release its control session).
//!
//! ## Threading
//!
//! Each driver owns two TCP connections:
//! 1. The primary connection used by the TX worker for `key`/`unkey`.
//! 2. A separate keepalive connection owned by a dedicated idle-probe
//!    thread that fires `t\n` every 30 s (5 s when down) to detect silent daemon death
//!    between TX cycles. Using a second connection avoids mutex
//!    contention on the TX hot path — one TCP per driver is cheap, and
//!    a keepalive failure never poisons the TX path.
//!
//! ## No `PortRegistry` caching
//!
//! Each channel gets its own TCP connection. There's no serial-fd
//! contention to work around, and if two channels point at the same
//! rigctld daemon that daemon can handle multiple concurrent sessions
//! on its own (it's a server).

use std::io::{BufRead, BufReader, Write};
use std::net::{TcpStream, ToSocketAddrs};
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::mpsc::{self, RecvTimeoutError, Sender};
use std::sync::Arc;
use std::thread::{self, JoinHandle};
use std::time::{Duration, Instant};

use super::PttDriver;

/// 3 seconds: TCP connect budget (initial + reconnect). DNS resolution
/// isn't bounded by this on every platform — documented in the plan.
const CONNECT_TIMEOUT: Duration = Duration::from_secs(3);

/// 2 seconds: initial handshake probe window. Longer than the hot path
/// so a sluggish rig has a chance to answer `t` before we give up.
const PROBE_TIMEOUT: Duration = Duration::from_secs(2);

/// 500 ms: per-I/O timeout for the hot-path `T 1`/`T 0`. Bounds
/// worst-case stuck-TX under ~1 s (initial failure + single reconnect).
const IO_TIMEOUT: Duration = Duration::from_millis(500);

/// Spacing between the bounded safety retries on unkey failure.
const UNKEY_RETRY_SPACING: Duration = Duration::from_millis(150);

/// How many extra retries to make after the single reconnect-retry
/// during unkey. Only applies to unkey, not key — stuck-keyed radio
/// is materially worse than a failed TX.
const UNKEY_SAFETY_RETRIES: u32 = 3;

/// Idle interval between keepalive probes when the daemon is up.
const KEEPALIVE_INTERVAL: Duration = Duration::from_secs(30);

/// Faster retry interval when the daemon is known to be down.
const KEEPALIVE_RETRY_INTERVAL: Duration = Duration::from_secs(5);

/// Handle to the keepalive thread: a shutdown sender plus the join
/// handle. `Drop` on [`RigctldPtt`] fires a non-blocking `try_send` on
/// the shutdown channel and then joins with a short timeout.
struct KeepaliveHandle {
    shutdown: Sender<()>,
    join: Option<JoinHandle<()>>,
}

/// Rigctld TCP PTT driver. Lives per-channel inside the TX worker's
/// driver table; `Send` because the underlying `TcpStream` and
/// `JoinHandle` are both `Send` and nothing else is borrowed from the
/// outside.
pub(crate) struct RigctldPtt {
    addr: String,
    stream: Option<TcpStream>,
    reader: Option<BufReader<TcpStream>>,
    connect_timeout: Duration,
    probe_timeout: Duration,
    io_timeout: Duration,
    keepalive: Option<KeepaliveHandle>,
    /// Set by the keepalive thread when it detects the daemon went down.
    /// Checked by `ensure_connected` to preemptively drop a stale primary
    /// stream so the next TX gets a clean reconnect.
    daemon_down: Arc<AtomicBool>,
}

impl RigctldPtt {
    /// Open a TCP connection to the rigctld daemon at `addr`
    /// (`"host:port"`), validate the full TCP → rigctld → CAT → radio
    /// path with a `t` probe, force the radio to RX state, then spawn
    /// the keepalive thread. Returns an error if any step fails.
    pub(crate) fn connect(addr: &str) -> Result<Self, String> {
        if addr.is_empty() {
            return Err("rigctld ptt: address is empty".into());
        }
        let mut driver = Self {
            addr: addr.to_string(),
            stream: None,
            reader: None,
            connect_timeout: CONNECT_TIMEOUT,
            probe_timeout: PROBE_TIMEOUT,
            io_timeout: IO_TIMEOUT,
            keepalive: None,
            daemon_down: Arc::new(AtomicBool::new(false)),
        };

        // Initial connect at probe-timeout so the validation handshake
        // has a chance against a sluggish radio. Timeouts drop to the
        // hot-path value after the probe succeeds.
        driver.open_stream(driver.probe_timeout)?;

        // Validate the full path: `t` is non-disruptive (no keying).
        // A healthy daemon answers with a single line `0`/`1`. A non-zero
        // `RPRT` means rigctld is up but the CAT link or rig is broken —
        // surface that to the caller rather than silently papering over.
        driver.probe_get_ptt()?;

        // Lower timeouts to the hot-path value for steady-state use.
        if let Some(s) = driver.stream.as_ref() {
            s.set_read_timeout(Some(driver.io_timeout))
                .map_err(|e| format!("rigctld ptt: set read timeout: {}", e))?;
            s.set_write_timeout(Some(driver.io_timeout))
                .map_err(|e| format!("rigctld ptt: set write timeout: {}", e))?;
        }

        // Force RX before returning — parity with Cm108Ptt / SerialLinePtt
        // construction. Any prior PTT state on the rig is cleared.
        driver.set_ptt(false)?;

        // Keepalive thread runs last so a failure in anything above
        // doesn't leave a stray thread behind.
        driver.spawn_keepalive();

        Ok(driver)
    }

    /// Open (or re-open) the primary TCP connection. Replaces `stream`
    /// and `reader` on success; on failure they are cleared. `timeout`
    /// is applied to both read and write for steady-state I/O.
    fn open_stream(&mut self, timeout: Duration) -> Result<(), String> {
        // `TcpStream::connect_timeout` requires a pre-resolved SocketAddr,
        // so we run DNS first via ToSocketAddrs, then try each address
        // until one connects. DNS resolution itself is NOT bounded by
        // `connect_timeout` on every platform (documented risk in plan).
        let addrs = self
            .addr
            .to_socket_addrs()
            .map_err(|e| format!("rigctld ptt: resolve '{}': {}", self.addr, e))?;

        let mut last_err = None;
        let mut stream: Option<TcpStream> = None;
        for sa in addrs {
            match TcpStream::connect_timeout(&sa, self.connect_timeout) {
                Ok(s) => {
                    stream = Some(s);
                    break;
                }
                Err(e) => last_err = Some(e),
            }
        }
        let stream = stream.ok_or_else(|| {
            let msg = last_err
                .map(|e| e.to_string())
                .unwrap_or_else(|| "no addresses resolved".into());
            format!("rigctld ptt: connect '{}': {}", self.addr, msg)
        })?;

        stream
            .set_nodelay(true)
            .map_err(|e| format!("rigctld ptt: set nodelay: {}", e))?;
        stream
            .set_read_timeout(Some(timeout))
            .map_err(|e| format!("rigctld ptt: set read timeout: {}", e))?;
        stream
            .set_write_timeout(Some(timeout))
            .map_err(|e| format!("rigctld ptt: set write timeout: {}", e))?;

        // `BufReader` gets its own clone of the stream so reads and
        // writes don't fight each other's buffers.
        let reader_stream = stream
            .try_clone()
            .map_err(|e| format!("rigctld ptt: clone stream: {}", e))?;

        self.stream = Some(stream);
        self.reader = Some(BufReader::new(reader_stream));
        Ok(())
    }

    /// Drop the primary stream and its reader. The next hot-path call
    /// will re-open. Also used by the unkey panic path, where closing
    /// the socket signals rigctld to release the control session.
    fn drop_stream(&mut self) {
        self.stream = None;
        self.reader = None;
    }

    /// Ensure the primary stream is open, opening it at `io_timeout`
    /// if not. Called at the top of every hot-path command. If the
    /// keepalive thread has signalled that the daemon went down, drop
    /// the stale stream first so we get a clean reconnect.
    fn ensure_connected(&mut self) -> Result<(), String> {
        if self.daemon_down.swap(false, Ordering::Relaxed) {
            self.drop_stream();
        }
        if self.stream.is_some() && self.reader.is_some() {
            return Ok(());
        }
        self.open_stream(self.io_timeout)
    }

    /// Send `t\n` and read the single-line reply. Used only during the
    /// initial handshake in `connect()`. A successful get_ptt returns
    /// just the state value (`0`/`1`) with no trailing `RPRT` — so one
    /// line of `0`/`1` is success. An error reply is `RPRT <code>`; a
    /// non-zero code surfaces the hamlib error so the caller can tell
    /// rigctld-up-radio-down apart from rigctld-unreachable.
    fn probe_get_ptt(&mut self) -> Result<(), String> {
        self.write_line("t\n")?;
        let line = self.read_line()?;
        let trimmed = line.trim();
        if trimmed == "0" || trimmed == "1" {
            return Ok(());
        }
        // Not a state line: the only meaningful alternative is an error
        // `RPRT <code>`, whose non-zero code we surface. parse_rprt maps
        // malformed input to the -9999 sentinel; a bare `RPRT 0` (which a
        // get should never emit) parses as Ok. Both fall through to the
        // mismatch arm — a get that yields no value is not a valid probe.
        match parse_rprt(trimmed) {
            Err(code) if code != -9999 => Err(format!(
                "rigctld ptt: probe: hamlib error code {} (see `rigctl --list`)",
                code
            )),
            _ => Err(format!(
                "rigctld ptt: probe: expected '0' or '1', got '{}'",
                trimmed
            )),
        }
    }

    /// Set PTT state. On I/O error: drop stream, reconnect once, retry.
    /// On non-zero RPRT: return `Err` (don't retry — it's a semantic
    /// failure, not a transport failure). This is the raw single-pass
    /// attempt; the safety-retry loop is layered on top for unkey.
    fn set_ptt(&mut self, on: bool) -> Result<(), String> {
        let cmd = if on { "T 1\n" } else { "T 0\n" };
        match self.try_set_ptt_once(cmd) {
            Ok(()) => Ok(()),
            Err(TrySetErr::Io(e)) => {
                // Transport failure — reconnect once and retry.
                self.drop_stream();
                self.ensure_connected()
                    .map_err(|re| format!("{}; reconnect: {}", e, re))?;
                match self.try_set_ptt_once(cmd) {
                    Ok(()) => Ok(()),
                    Err(TrySetErr::Io(e2)) => Err(format!("rigctld ptt: {}", e2)),
                    Err(TrySetErr::Rprt(code)) => {
                        Err(format!("rigctld ptt: hamlib error code {}", code))
                    }
                }
            }
            Err(TrySetErr::Rprt(code)) => {
                Err(format!("rigctld ptt: hamlib error code {}", code))
            }
        }
    }

    /// Single attempt: send `cmd` (ending in `\n`), read one line,
    /// parse as `RPRT <n>`. Callers classify the error and decide
    /// whether to retry.
    fn try_set_ptt_once(&mut self, cmd: &str) -> Result<(), TrySetErr> {
        self.ensure_connected().map_err(TrySetErr::Io)?;
        self.write_line(cmd).map_err(TrySetErr::Io)?;
        let line = self.read_line().map_err(TrySetErr::Io)?;
        match parse_rprt(&line) {
            Ok(()) => Ok(()),
            Err(code) => Err(TrySetErr::Rprt(code)),
        }
    }

    /// Write a raw command string and flush. Returns an error message
    /// (not an `io::Error`) so callers can treat it as transport failure.
    fn write_line(&mut self, line: &str) -> Result<(), String> {
        let stream = self
            .stream
            .as_mut()
            .ok_or_else(|| "rigctld ptt: stream not connected".to_string())?;
        stream
            .write_all(line.as_bytes())
            .map_err(|e| format!("write: {}", e))?;
        stream.flush().map_err(|e| format!("flush: {}", e))
    }

    /// Read one `\n`-terminated line from the buffered reader. Returns
    /// the line WITH the trailing newline stripped via `trim_end`
    /// semantics left to the caller.
    fn read_line(&mut self) -> Result<String, String> {
        let reader = self
            .reader
            .as_mut()
            .ok_or_else(|| "rigctld ptt: reader not connected".to_string())?;
        let mut buf = String::new();
        let n = reader
            .read_line(&mut buf)
            .map_err(|e| format!("read: {}", e))?;
        if n == 0 {
            return Err("read: peer closed connection".into());
        }
        Ok(buf)
    }

    /// Safety-critical unkey: on failure after the single reconnect in
    /// `set_ptt`, try up to [`UNKEY_SAFETY_RETRIES`] additional times
    /// spaced [`UNKEY_RETRY_SPACING`] apart. If all fail, drop the
    /// socket (which tells rigctld to release its control session) and
    /// log at error level. Stuck-key on-air is materially worse than a
    /// surfaced error.
    fn unkey_with_safety_retries(&mut self) -> Result<(), String> {
        match self.set_ptt(false) {
            Ok(()) => return Ok(()),
            Err(e) => {
                eprintln!(
                    "graywolf-modem: rigctld: unkey failed (initial+reconnect): {}; \
                     starting {}-attempt safety retry at {}ms spacing",
                    e,
                    UNKEY_SAFETY_RETRIES,
                    UNKEY_RETRY_SPACING.as_millis()
                );
            }
        }

        let mut last_err = String::new();
        for attempt in 1..=UNKEY_SAFETY_RETRIES {
            thread::sleep(UNKEY_RETRY_SPACING);
            // Force a fresh connection each retry — the prior error
            // leaves the stream in an unknown state.
            self.drop_stream();
            match self.set_ptt(false) {
                Ok(()) => {
                    eprintln!(
                        "graywolf-modem: rigctld: unkey recovered on safety retry {}",
                        attempt
                    );
                    return Ok(());
                }
                Err(e) => {
                    last_err = e;
                    eprintln!(
                        "graywolf-modem: rigctld: unkey safety retry {}/{} failed: {}",
                        attempt, UNKEY_SAFETY_RETRIES, last_err
                    );
                }
            }
        }

        // Panic-unkey: drop the socket. rigctld treats a closed
        // control session as a reason to release the rig, which on
        // most backends clears PTT. This is our last line of defense.
        self.drop_stream();
        eprintln!(
            "graywolf-modem: rigctld: PANIC-UNKEY for '{}' — closed socket after {} \
             safety retries; radio may still be keyed, last error: {}",
            self.addr, UNKEY_SAFETY_RETRIES, last_err
        );
        Err(format!(
            "rigctld ptt: panic-unkey, socket closed after {} retries: {}",
            UNKEY_SAFETY_RETRIES, last_err
        ))
    }

    /// Spawn the idle-probe thread. The thread owns its own TCP
    /// connection — no mutex contention with the TX hot path. On probe
    /// failure it logs a state transition and tries to reconnect on
    /// the next tick.
    fn spawn_keepalive(&mut self) {
        let (tx, rx) = mpsc::channel::<()>();
        let addr = self.addr.clone();
        let connect_timeout = self.connect_timeout;
        let probe_timeout = self.probe_timeout;
        let daemon_down = self.daemon_down.clone();

        let join = thread::Builder::new()
            .name("graywolf-rigctld-keepalive".into())
            .spawn(move || keepalive_loop(addr, connect_timeout, probe_timeout, rx, daemon_down))
            .ok();

        self.keepalive = Some(KeepaliveHandle {
            shutdown: tx,
            join,
        });
    }
}

/// Classifier for [`RigctldPtt::try_set_ptt_once`].
enum TrySetErr {
    /// Transport failure — caller should reconnect and retry.
    Io(String),
    /// `RPRT n` with non-zero `n` — semantic failure, don't retry.
    Rprt(i32),
}

/// Parse a rigctld `RPRT <n>\n` line. Returns `Ok(())` on `RPRT 0`,
/// `Err(code)` with the hamlib error code on non-zero, and a synthetic
/// -9999 code for malformed lines so callers can surface the problem
/// without adding another error variant.
fn parse_rprt(line: &str) -> Result<(), i32> {
    let trimmed = line.trim();
    let rest = match trimmed.strip_prefix("RPRT ") {
        Some(r) => r,
        None => return Err(-9999),
    };
    let code: i32 = match rest.parse() {
        Ok(n) => n,
        Err(_) => return Err(-9999),
    };
    if code == 0 {
        Ok(())
    } else {
        Err(code)
    }
}

/// Idle-probe thread body. Owns its own TCP connection (opened lazily
/// on the first tick). Every [`KEEPALIVE_INTERVAL`] sends `t\n` and
/// logs `up → down` / `down → up` transitions. Shuts down as soon as
/// the `shutdown` channel fires or disconnects.
fn keepalive_loop(
    addr: String,
    connect_timeout: Duration,
    probe_timeout: Duration,
    shutdown: mpsc::Receiver<()>,
    daemon_down: Arc<AtomicBool>,
) {
    let mut conn: Option<(TcpStream, BufReader<TcpStream>)> = None;
    let mut up = true; // Primary connect succeeded, so we start "up".

    loop {
        // Poll faster when the daemon is known to be down so recovery
        // is detected in ~5s instead of ~30s.
        let interval = if up {
            KEEPALIVE_INTERVAL
        } else {
            KEEPALIVE_RETRY_INTERVAL
        };

        match shutdown.recv_timeout(interval) {
            Ok(()) => break,
            Err(RecvTimeoutError::Disconnected) => break,
            Err(RecvTimeoutError::Timeout) => {}
        }

        let result = keepalive_probe_once(&mut conn, &addr, connect_timeout, probe_timeout);
        match (up, result) {
            (true, Ok(())) => {
                // Still up, quietly continue.
            }
            (true, Err(e)) => {
                up = false;
                daemon_down.store(true, Ordering::Relaxed);
                eprintln!(
                    "graywolf-modem: rigctld keepalive: up → down for '{}': {}",
                    addr, e
                );
                conn = None;
            }
            (false, Ok(())) => {
                up = true;
                daemon_down.store(false, Ordering::Relaxed);
                eprintln!(
                    "graywolf-modem: rigctld keepalive: down → up for '{}'",
                    addr
                );
            }
            (false, Err(_)) => {
                // Still down, stay quiet to avoid log spam.
                conn = None;
            }
        }
    }
}

/// One keepalive probe: open if needed, send `t\n`, read the single
/// state line, return Ok on `0`/`1`. A successful get_ptt has no
/// trailing `RPRT` (see `probe_get_ptt`); reading a second line here
/// would block until timeout against a healthy daemon and falsely mark
/// it down. Any I/O failure or error reply propagates as Err.
fn keepalive_probe_once(
    conn: &mut Option<(TcpStream, BufReader<TcpStream>)>,
    addr: &str,
    connect_timeout: Duration,
    probe_timeout: Duration,
) -> Result<(), String> {
    let (stream, reader) = match conn.as_mut() {
        Some(c) => c,
        None => {
            let stream = open_tcp(addr, connect_timeout, probe_timeout)?;
            let reader_stream = stream
                .try_clone()
                .map_err(|e| format!("clone: {}", e))?;
            conn.insert((stream, BufReader::new(reader_stream)))
        }
    };
    stream
        .write_all(b"t\n")
        .map_err(|e| format!("write: {}", e))?;
    stream.flush().map_err(|e| format!("flush: {}", e))?;

    let mut line = String::new();
    let n = reader
        .read_line(&mut line)
        .map_err(|e| format!("read state: {}", e))?;
    if n == 0 {
        return Err("peer closed connection".into());
    }
    let trimmed = line.trim();
    if trimmed == "0" || trimmed == "1" {
        return Ok(());
    }
    match parse_rprt(trimmed) {
        Err(code) if code != -9999 => Err(format!("hamlib error code {}", code)),
        _ => Err(format!("unexpected state line '{}'", trimmed)),
    }
}

/// Shared helper: resolve + connect with timeout + apply read/write
/// timeouts + set_nodelay. Used only by the keepalive thread; the
/// primary driver has its own equivalent on `RigctldPtt` that also
/// wires the result into the driver's fields.
fn open_tcp(
    addr: &str,
    connect_timeout: Duration,
    io_timeout: Duration,
) -> Result<TcpStream, String> {
    let addrs = addr
        .to_socket_addrs()
        .map_err(|e| format!("resolve '{}': {}", addr, e))?;
    let mut last_err = None;
    for sa in addrs {
        match TcpStream::connect_timeout(&sa, connect_timeout) {
            Ok(s) => {
                s.set_nodelay(true).map_err(|e| format!("nodelay: {}", e))?;
                s.set_read_timeout(Some(io_timeout))
                    .map_err(|e| format!("set read timeout: {}", e))?;
                s.set_write_timeout(Some(io_timeout))
                    .map_err(|e| format!("set write timeout: {}", e))?;
                return Ok(s);
            }
            Err(e) => last_err = Some(e),
        }
    }
    Err(format!(
        "connect '{}': {}",
        addr,
        last_err
            .map(|e| e.to_string())
            .unwrap_or_else(|| "no addresses resolved".into())
    ))
}

impl PttDriver for RigctldPtt {
    /// Key the radio. No safety retry — a failed key is a surfaced
    /// error, not a risk of stuck TX.
    fn key(&mut self) -> Result<(), String> {
        self.set_ptt(true)
    }

    /// Unkey the radio. Safety-critical: bounded retry loop on top of
    /// `set_ptt` before falling back to panic-unkey (socket close).
    fn unkey(&mut self) -> Result<(), String> {
        self.unkey_with_safety_retries()
    }
}

impl Drop for RigctldPtt {
    fn drop(&mut self) {
        // Shut the keepalive thread down first — it's the only other
        // thread that might race on the daemon. `try_send` is
        // non-blocking; a full channel or closed receiver is fine
        // (the thread is exiting or already gone).
        if let Some(mut handle) = self.keepalive.take() {
            // Best-effort: if the thread has already exited the channel
            // will be disconnected, which is exactly what we want.
            let _ = handle.shutdown.send(());
            if let Some(j) = handle.join.take() {
                // No JoinHandle::join_timeout in std; use a small
                // polling wait so Drop doesn't stall the driver-swap
                // path indefinitely if the keepalive thread is wedged.
                let deadline = Instant::now() + Duration::from_millis(500);
                loop {
                    if j.is_finished() {
                        let _ = j.join();
                        break;
                    }
                    if Instant::now() >= deadline {
                        // Leak the handle rather than block forever.
                        // The thread will notice the closed channel
                        // eventually and exit.
                        break;
                    }
                    thread::sleep(Duration::from_millis(20));
                }
            }
        }

        // Best-effort unkey. Parity with Cm108Ptt::drop at ptt.rs:261-270.
        // If the daemon is already gone this will fail — and that's
        // fine; losing the connection effectively unkeys too.
        // Worst-case ~450ms on a dead daemon (3 × 150ms safety retries
        // before panic-unkey closes the socket).
        let _ = self.unkey_with_safety_retries();
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn parse_rprt_accepts_zero_as_success() {
        assert!(parse_rprt("RPRT 0\n").is_ok());
        assert!(parse_rprt("RPRT 0").is_ok());
        assert!(parse_rprt("  RPRT 0  \n").is_ok());
    }

    #[test]
    fn parse_rprt_returns_nonzero_code_as_error() {
        assert_eq!(parse_rprt("RPRT -1\n"), Err(-1));
        assert_eq!(parse_rprt("RPRT 2\n"), Err(2));
        assert_eq!(parse_rprt("RPRT -11\n"), Err(-11));
    }

    #[test]
    fn parse_rprt_treats_malformed_lines_as_synthetic_error() {
        assert_eq!(parse_rprt(""), Err(-9999));
        assert_eq!(parse_rprt("not-rprt\n"), Err(-9999));
        assert_eq!(parse_rprt("RPRT abc\n"), Err(-9999));
        assert_eq!(parse_rprt("0\n"), Err(-9999));
    }

    /// Spawn a fake rigctld on an ephemeral loopback port that speaks
    /// the REAL protocol: a set (`T 1`/`T 0`) replies with `RPRT 0`, and
    /// a get (`t`) replies with `get_reply` — a single line, defaulting
    /// to a healthy `0\n` value with no trailing `RPRT`. Accepts
    /// repeatedly so both the primary and keepalive connections are
    /// served. Returns the bound address.
    fn spawn_fake_rigctld_with(get_reply: &'static [u8]) -> String {
        use std::net::TcpListener;
        let listener = TcpListener::bind("127.0.0.1:0").expect("bind fake rigctld");
        let addr = listener.local_addr().expect("local_addr").to_string();
        thread::spawn(move || {
            for stream in listener.incoming() {
                let stream = match stream {
                    Ok(s) => s,
                    Err(_) => continue,
                };
                thread::spawn(move || {
                    let mut writer = stream.try_clone().expect("clone");
                    let mut reader = BufReader::new(stream);
                    let mut line = String::new();
                    loop {
                        line.clear();
                        match reader.read_line(&mut line) {
                            Ok(0) | Err(_) => break,
                            Ok(_) => {}
                        }
                        let cmd = line.trim();
                        let reply: &[u8] = if cmd == "t" {
                            get_reply
                        } else if cmd.starts_with("T ") {
                            b"RPRT 0\n"
                        } else {
                            b"RPRT -1\n"
                        };
                        if writer.write_all(reply).is_err() {
                            break;
                        }
                        let _ = writer.flush();
                    }
                });
            }
        });
        addr
    }

    /// Healthy daemon: `t` returns a bare `0` value line.
    fn spawn_fake_rigctld() -> String {
        spawn_fake_rigctld_with(b"0\n")
    }

    /// GRA-73 regression: a healthy rigctld answers `t` with only the
    /// state value (no trailing `RPRT 0`). The connect handshake must
    /// succeed against that, and key/unkey must round-trip — previously
    /// the probe blocked waiting for a second line, the connect failed,
    /// and PTT silently never keyed.
    #[test]
    fn connect_and_key_against_single_line_get_ptt() {
        let addr = spawn_fake_rigctld();
        let mut driver = match RigctldPtt::connect(&addr) {
            Ok(d) => d,
            Err(e) => panic!("connect should succeed against real protocol: {}", e),
        };
        driver.key().expect("key should send 'T 1' and accept RPRT 0");
        driver.unkey().expect("unkey should send 'T 0' and accept RPRT 0");
    }

    #[test]
    fn connect_surfaces_hamlib_error_from_get_ptt() {
        // rigctld-up-radio-down: the get returns a non-zero RPRT instead
        // of a value. The probe must surface the hamlib code, not key.
        let addr = spawn_fake_rigctld_with(b"RPRT -6\n");
        let err = match RigctldPtt::connect(&addr) {
            Err(e) => e,
            Ok(_) => panic!("expected connect to fail on RPRT -6"),
        };
        assert!(err.contains("hamlib error code -6"), "unexpected error: {}", err);
    }

    #[test]
    fn connect_rejects_unexpected_get_ptt_line() {
        // A line that is neither a state value nor a parseable RPRT is a
        // protocol mismatch (parse_rprt maps it to the -9999 sentinel,
        // which must NOT be reported as a hamlib code).
        let addr = spawn_fake_rigctld_with(b"BOGUS\n");
        let err = match RigctldPtt::connect(&addr) {
            Err(e) => e,
            Ok(_) => panic!("expected connect to fail on garbage reply"),
        };
        assert!(
            err.contains("expected '0' or '1'") && err.contains("BOGUS"),
            "unexpected error: {}",
            err
        );
    }

    #[test]
    fn connect_rejects_empty_address() {
        // `unwrap_err` would need RigctldPtt: Debug, which we deliberately
        // don't impl. Pattern match keeps this test zero-dep.
        let err = match RigctldPtt::connect("") {
            Err(e) => e,
            Ok(_) => panic!("expected connect to fail on empty address"),
        };
        assert!(err.contains("address is empty"), "unexpected error: {}", err);
    }
}
