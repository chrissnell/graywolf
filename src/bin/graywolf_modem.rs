//! `graywolf-modem` — the Rust DSP child process for graywolf.
//!
//! Usage:
//!
//!     graywolf-modem <socket-path>
//!
//! Lifecycle:
//!  1. Bind the Unix socket at `<socket-path>`.
//!  2. Write `\n` to stdout (readiness signal for the Go parent).
//!  3. Accept one IPC client, send `ModemReady`.
//!  4. Serve control + audio messages until `Shutdown` or disconnect.

use std::process::ExitCode;

use direwolf_demod::ipc::server::IpcServer;
use direwolf_demod::modem::Modem;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().collect();
    if args.len() != 2 {
        eprintln!("usage: graywolf-modem <socket-path>");
        return ExitCode::from(2);
    }
    let socket_path = &args[1];

    let server = match IpcServer::bind(socket_path) {
        Ok(s) => s,
        Err(e) => {
            eprintln!("graywolf-modem: bind {}: {}", socket_path, e);
            return ExitCode::from(1);
        }
    };

    let (handle, inbound, _reader_join) = match server.accept() {
        Ok(v) => v,
        Err(e) => {
            eprintln!("graywolf-modem: accept failed: {}", e);
            return ExitCode::from(1);
        }
    };

    let modem = Modem::new(handle, inbound);
    modem.run();
    ExitCode::SUCCESS
}
