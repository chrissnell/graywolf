//! Windows-side hardware adapter for serial PTT lines.
//!
//! Opens a COM port with `CreateFileW` in shared read+write mode
//! (so `rigctld` et al. can still access the device) and toggles
//! RTS/DTR via `EscapeCommFunction`. No termios analog exists on
//! Windows, so there is no equivalent of the Unix-side concern
//! about `tcsetattr` bouncing modem control lines. Matches
//! direwolf's `ptt.c` Windows path.

use windows::core::HSTRING;
use windows::Win32::Devices::Communication::{EscapeCommFunction, CLRDTR, CLRRTS, SETDTR, SETRTS};
use windows::Win32::Foundation::{CloseHandle, GENERIC_READ, GENERIC_WRITE, HANDLE};
use windows::Win32::Storage::FileSystem::{
    CreateFileW, FILE_FLAGS_AND_ATTRIBUTES, FILE_SHARE_READ, FILE_SHARE_WRITE, OPEN_EXISTING,
};

use super::ModemControlLines;

pub(super) struct WinSerialLines {
    handle: HANDLE,
}

// SAFETY: Windows HANDLE is not marked Send by the `windows` crate
// because raw HANDLEs are process-wide and the crate can't know
// whether a given one is being shared. PortRegistry serialises all
// access to a single WinSerialLines instance through its Mutex, so
// concurrent use is impossible by construction.
unsafe impl Send for WinSerialLines {}

impl WinSerialLines {
    pub(super) fn open(path: &str) -> Result<Self, String> {
        let wide: HSTRING = path.into();
        // SAFETY: `wide` is a valid NUL-terminated UTF-16 buffer that
        // outlives the call; all other pointer arguments are default.
        let handle = unsafe {
            CreateFileW(
                &wide,
                (GENERIC_READ | GENERIC_WRITE).0,
                // Shared read+write so rigctld / fldigi / other tools
                // can also open the device. Matches direwolf's shared-
                // mode behaviour (not exclusive like the serialport
                // crate's default).
                FILE_SHARE_READ | FILE_SHARE_WRITE,
                None,
                OPEN_EXISTING,
                FILE_FLAGS_AND_ATTRIBUTES(0),
                HANDLE::default(),
            )
        }
        .map_err(|e| format!("CreateFileW {}: {}", path, e))?;
        Ok(Self { handle })
    }
}

impl ModemControlLines for WinSerialLines {
    fn write_rts(&mut self, level: bool) -> Result<(), String> {
        let code = if level { SETRTS } else { CLRRTS };
        // SAFETY: handle is valid for the lifetime of &mut self.
        unsafe { EscapeCommFunction(self.handle, code) }
            .map_err(|e| format!("EscapeCommFunction RTS={}: {}", level, e))
    }

    fn write_dtr(&mut self, level: bool) -> Result<(), String> {
        let code = if level { SETDTR } else { CLRDTR };
        // SAFETY: same as write_rts.
        unsafe { EscapeCommFunction(self.handle, code) }
            .map_err(|e| format!("EscapeCommFunction DTR={}: {}", level, e))
    }
}

impl Drop for WinSerialLines {
    fn drop(&mut self) {
        // SAFETY: handle was obtained from CreateFileW and hasn't
        // been closed. Ignore the return — we can't recover from a
        // close failure during Drop.
        let _ = unsafe { CloseHandle(self.handle) };
    }
}
