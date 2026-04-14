//! Windows CM108 HID GPIO adapter for PTT.
//!
//! Writes CM108 HID output reports via `WriteFile` to the HID device
//! handle. Same 5-byte output report as Linux (`write()` on
//! `/dev/hidrawN`) and macOS (`hidapi`). On Windows the device path is
//! a HID device interface path (e.g. `\\?\hid#vid_0d8c&pid_000c#...`),
//! returned by `hidapi`'s enumeration and passed through from the Go
//! parent's `--list-cm108` subcommand output.

use windows::core::HSTRING;
use windows::Win32::Foundation::{CloseHandle, GENERIC_READ, GENERIC_WRITE, HANDLE};
use windows::Win32::Storage::FileSystem::{
    CreateFileW, WriteFile, FILE_FLAGS_AND_ATTRIBUTES, FILE_SHARE_READ, FILE_SHARE_WRITE,
    OPEN_EXISTING,
};

pub(super) struct WinCm108Gpio {
    handle: HANDLE,
}

// SAFETY: Windows HANDLE is not marked Send by the `windows` crate
// because raw HANDLEs are process-wide. PortRegistry serialises all
// access through its Mutex, so concurrent use is impossible by
// construction. Same justification as WinSerialLines.
unsafe impl Send for WinCm108Gpio {}

impl WinCm108Gpio {
    pub(super) fn open(path: &str) -> Result<Self, String> {
        let wide: HSTRING = path.into();
        // SAFETY: `wide` is a valid NUL-terminated UTF-16 buffer that
        // outlives the call; all other pointer arguments are default.
        let handle = unsafe {
            CreateFileW(
                &wide,
                (GENERIC_READ | GENERIC_WRITE).0,
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

impl super::Cm108GpioControl for WinCm108Gpio {
    fn write_gpio(&mut self, pin: u8, level: bool) -> Result<(), String> {
        if pin < 1 || pin > 8 {
            return Err(format!("cm108 gpio pin {} out of range (1-8)", pin));
        }
        let mask: u8 = 1 << (pin - 1);
        let value: u8 = if level { mask } else { 0 };
        let report: [u8; 5] = [
            0x00,  // HID report ID (always 0)
            0x00,  // HID_OR0: GPIO write mode (bits 7:6 = 00)
            value, // HID_OR1: GPIO output values
            mask,  // HID_OR2: data direction register (1=output)
            0x00,  // HID_OR3: SPDIF control (unused)
        ];
        let mut written: u32 = 0;
        // SAFETY: handle is valid for the lifetime of &mut self;
        // report buffer outlives the call.
        unsafe {
            WriteFile(self.handle, Some(&report), Some(&mut written), None)
        }
        .map_err(|e| format!("cm108 WriteFile: {}", e))?;
        if written as usize != report.len() {
            return Err(format!(
                "cm108 short write: {} of {}",
                written,
                report.len()
            ));
        }
        Ok(())
    }
}

impl Drop for WinCm108Gpio {
    fn drop(&mut self) {
        // SAFETY: handle was obtained from CreateFileW and hasn't been
        // closed. Ignore the return — can't recover during Drop.
        let _ = unsafe { CloseHandle(self.handle) };
    }
}
