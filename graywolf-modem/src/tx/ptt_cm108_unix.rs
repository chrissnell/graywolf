//! Unix CM108 HID GPIO adapter for PTT.
//!
//! Writes CM108 HID output reports to `/dev/hidrawN` via
//! `nix::unistd::write`. Same approach as direwolf `cm108.c` and hamlib
//! `cm108.c` — a 5-byte output report (report ID + 4 payload registers)
//! sent via `write()`, NOT `ioctl(HIDIOCSFEATURE)`.
//!
//! HIDIOCSFEATURE sends a HID *feature report*, which is a different USB
//! transfer type. On AIOC devices, feature reports are interpreted as
//! configuration register writes and would corrupt device settings.

use std::os::fd::{FromRawFd, OwnedFd};

use nix::fcntl::{open, OFlag};
use nix::sys::stat::Mode;

pub(super) struct UnixCm108Gpio {
    fd: OwnedFd,
}

impl UnixCm108Gpio {
    pub(super) fn open(path: &str) -> Result<Self, String> {
        let raw = open(
            path,
            OFlag::O_RDWR | OFlag::O_NONBLOCK | OFlag::O_CLOEXEC,
            Mode::empty(),
        )
        .map_err(|e| format!("open {}: {}", path, e))?;
        // SAFETY: raw is a fresh fd we just opened and own exclusively.
        let fd = unsafe { OwnedFd::from_raw_fd(raw) };
        Ok(Self { fd })
    }
}

impl super::Cm108GpioControl for UnixCm108Gpio {
    fn write_gpio(&mut self, pin: u8, level: bool) -> Result<(), String> {
        if pin < 1 || pin > 4 {
            return Err(format!("cm108 gpio pin {} out of range (1-4)", pin));
        }
        let mask: u8 = 1 << (pin - 1); // pin 1-indexed: GPIO3 → bit 2 → 0x04
        let value: u8 = if level { mask } else { 0 };
        let report: [u8; 5] = [
            0x00,  // HID report ID (always 0)
            0x00,  // HID_OR0: GPIO write mode (bits 7:6 = 00)
            value, // HID_OR1: GPIO output values
            mask,  // HID_OR2: data direction register (1=output)
            0x00,  // HID_OR3: SPDIF control (unused)
        ];
        let n = nix::unistd::write(&self.fd, &report)
            .map_err(|e| format!("cm108 write: {}", e))?;
        if n != report.len() {
            return Err(format!("cm108 short write: {} of {}", n, report.len()));
        }
        Ok(())
    }
}

// Drop is implicit: OwnedFd closes the file descriptor on drop.
// Unlike serial ports, closing a hidraw fd does NOT reset CM108 GPIO
// state. The Cm108Ptt driver (Step 8) handles this by calling unkey()
// in its Drop impl.
