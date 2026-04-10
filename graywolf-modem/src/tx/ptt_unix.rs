//! Unix-side hardware adapter for serial PTT lines.
//!
//! Opens a TTY with `O_RDWR | O_NOCTTY | O_NONBLOCK | O_CLOEXEC` and
//! toggles RTS/DTR via `ioctl(TIOCMSET)`. Never touches termios — line
//! discipline, baud rate, and parity are irrelevant when we only move
//! modem control lines, and calling `tcsetattr` on some USB-serial
//! adapters briefly bounces RTS/DTR (which would key PTT spuriously).
//! Matches direwolf's `ptt.c` Unix path exactly.

use std::os::fd::{AsRawFd, FromRawFd, OwnedFd};

use nix::fcntl::{open, OFlag};
use nix::libc;
use nix::sys::stat::Mode;

use super::ModemControlLines;

// TIOCMGET / TIOCMSET are not in nix's high-level API. The
// ioctl_read_bad! / ioctl_write_ptr_bad! macros generate typed unsafe
// wrappers around libc::ioctl with the correct request number — "bad"
// in nix taxonomy means the request numbers aren't size-encoded, but
// they're otherwise stable kernel ABI.
nix::ioctl_read_bad!(tiocmget, libc::TIOCMGET, libc::c_int);
nix::ioctl_write_ptr_bad!(tiocmset, libc::TIOCMSET, libc::c_int);

pub(super) struct UnixSerialLines {
    fd: OwnedFd,
}

impl UnixSerialLines {
    pub(super) fn open(path: &str) -> Result<Self, String> {
        // O_NONBLOCK during open() avoids the DCD-blocking bug on
        // certain USB-serial adapters (direwolf ptt.c:928-931). We
        // never read or write data, only toggle modem control lines
        // via ioctl, so blocking mode after open is irrelevant and we
        // deliberately don't clear O_NONBLOCK afterwards.
        let raw = open(
            path,
            OFlag::O_RDWR | OFlag::O_NOCTTY | OFlag::O_NONBLOCK | OFlag::O_CLOEXEC,
            Mode::empty(),
        )
        .map_err(|e| format!("open {}: {}", path, e))?;
        // SAFETY: raw is a fresh fd we just opened and own exclusively.
        let fd = unsafe { OwnedFd::from_raw_fd(raw) };
        Ok(Self { fd })
    }

    fn update(&mut self, set: libc::c_int, clear: libc::c_int) -> Result<(), String> {
        let mut bits: libc::c_int = 0;
        // SAFETY: self.fd is valid for the lifetime of &mut self; &mut bits
        // points at valid stack memory the kernel reads into.
        unsafe { tiocmget(self.fd.as_raw_fd(), &mut bits) }
            .map_err(|e| format!("TIOCMGET: {}", e))?;
        bits = (bits | set) & !clear;
        // SAFETY: same invariants; the kernel only reads *bits.
        unsafe { tiocmset(self.fd.as_raw_fd(), &bits) }.map_err(|e| format!("TIOCMSET: {}", e))?;
        Ok(())
    }
}

impl ModemControlLines for UnixSerialLines {
    fn write_rts(&mut self, level: bool) -> Result<(), String> {
        if level {
            self.update(libc::TIOCM_RTS, 0)
        } else {
            self.update(0, libc::TIOCM_RTS)
        }
    }

    fn write_dtr(&mut self, level: bool) -> Result<(), String> {
        if level {
            self.update(libc::TIOCM_DTR, 0)
        } else {
            self.update(0, libc::TIOCM_DTR)
        }
    }
}

// Drop is implicit: OwnedFd closes the file descriptor on drop.
