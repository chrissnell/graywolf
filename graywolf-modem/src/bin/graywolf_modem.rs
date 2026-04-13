//! `graywolf-modem` — the Rust DSP child process for graywolf.
//!
//! Usage (Unix):
//!
//!     graywolf-modem <socket-path>
//!
//! Usage (Windows):
//!
//!     graywolf-modem
//!
//! Subcommands:
//!
//!     graywolf-modem --version       Print version and exit.
//!     graywolf-modem --list-cm108    Enumerate CM108-compatible USB HID
//!                                    devices as a JSON array and exit.
//!                                    Used by the Go parent on macOS and
//!                                    Windows where sysfs is unavailable.
//!
//! On Unix the IPC listener is a Unix domain socket at the given path. On
//! Windows it is a TCP socket on 127.0.0.1 with an OS-assigned port; the
//! port is printed to stdout as the readiness signal.
//!
//! Lifecycle:
//!  1. Bind the IPC listener.
//!  2. Write readiness signal to stdout (the Go parent waits on this).
//!  3. Accept one IPC client, send `ModemReady`.
//!  4. Serve control + audio messages until `Shutdown` or disconnect.

use std::process::ExitCode;

use graywolf_demod::ipc::server::IpcServer;
use graywolf_demod::modem::Modem;

fn main() -> ExitCode {
    let args: Vec<String> = std::env::args().collect();
    if args.len() == 2 && args[1] == "--version" {
        // Go parent parses this exact string to compare against its own
        // build version; keep the format stable.
        println!("{}", graywolf_demod::full_version());
        return ExitCode::SUCCESS;
    }

    if args.len() == 2 && args[1] == "--list-cm108" {
        match graywolf_demod::cm108::enumerate_cm108() {
            Ok(json) => {
                println!("{}", json);
                return ExitCode::SUCCESS;
            }
            Err(e) => {
                eprintln!("graywolf-modem: cm108 enumerate: {}", e);
                return ExitCode::from(1);
            }
        }
    }

    let server = bind_server(&args);
    let server = match server {
        Ok(s) => s,
        Err(e) => {
            eprintln!("graywolf-modem: bind: {}", e);
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

    let modem = match Modem::new(handle, inbound) {
        Ok(m) => m,
        Err(e) => {
            eprintln!("graywolf-modem: init failed: {}", e);
            return ExitCode::from(1);
        }
    };
    modem.run();
    ExitCode::SUCCESS
}

#[cfg(unix)]
fn bind_server(args: &[String]) -> Result<IpcServer, Box<dyn std::error::Error>> {
    if args.len() != 2 {
        eprintln!("usage: graywolf-modem <socket-path>");
        std::process::exit(2);
    }
    Ok(IpcServer::bind(&args[1])?)
}

#[cfg(windows)]
fn bind_server(_args: &[String]) -> Result<IpcServer, Box<dyn std::error::Error>> {
    Ok(IpcServer::bind()?)
}
