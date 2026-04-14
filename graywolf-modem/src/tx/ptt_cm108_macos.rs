//! macOS CM108 HID GPIO adapter for PTT.
//!
//! Uses `hidapi` to write CM108 HID output reports via IOKit's
//! `IOHIDDeviceSetReport(kIOHIDReportTypeOutput)`. macOS has no
//! `/dev/hidraw*` equivalent, so direct fd access as on Linux is not
//! possible. The `hidapi` crate wraps the IOKit HID Manager and
//! compiles from bundled C source — no Homebrew dependency at build
//! or runtime, only system frameworks (IOKit, CoreFoundation).
//!
//! The device path is an IOKit registry path (e.g.
//! `IOService:/AppleACPIPlatformExpert/...`), returned by `hidapi`'s
//! enumeration via `IORegistryEntryGetPath()`. The Go parent receives
//! this from the `--list-cm108` subcommand and passes it through
//! `ConfigurePtt.device`. Path format compatibility is guaranteed
//! because the same `hidapi` library handles both enumeration and
//! `open_path()`.
//!
//! Reference: Direwolf PR wb2osz/direwolf#500 added macOS CM108
//! support using the same `hidapi` library.

use std::ffi::CString;

use hidapi::HidApi;

pub(super) struct MacCm108Gpio {
    // Field order matters for Drop: device (hid_close) must drop before
    // _api (hid_exit). Rust drops fields in declaration order.
    device: hidapi::HidDevice,
    _api: HidApi,
}

// SAFETY: HidApi contains raw pointers from the C library; HidDevice
// wraps a raw IOKit handle. Neither is Send by default. PortRegistry
// serialises all access through its Mutex, so concurrent use is
// impossible by construction.
unsafe impl Send for MacCm108Gpio {}

impl MacCm108Gpio {
    pub(super) fn open(path: &str) -> Result<Self, String> {
        let api = HidApi::new().map_err(|e| format!("hidapi init: {}", e))?;
        let cpath =
            CString::new(path).map_err(|_| format!("invalid device path: {}", path))?;
        let device = api
            .open_path(&cpath)
            .map_err(|e| format!("hidapi open {}: {}", path, e))?;
        Ok(Self { device, _api: api })
    }
}

impl super::Cm108GpioControl for MacCm108Gpio {
    fn write_gpio(&mut self, pin: u8, level: bool) -> Result<(), String> {
        if pin < 1 || pin > 8 {
            return Err(format!("cm108 gpio pin {} out of range (1-8)", pin));
        }
        let mask = 1u8 << (pin - 1);
        let value = if level { mask } else { 0 };
        // Same 5-byte output report as Linux/Windows. hidapi sends it
        // via IOHIDDeviceSetReport(kIOHIDReportTypeOutput) on macOS.
        let report: [u8; 5] = [
            0x00,  // HID report ID (always 0)
            0x00,  // HID_OR0: GPIO write mode (bits 7:6 = 00)
            value, // HID_OR1: GPIO output values
            mask,  // HID_OR2: data direction register (1=output)
            0x00,  // HID_OR3: SPDIF control (unused)
        ];
        self.device
            .write(&report)
            .map_err(|e| format!("cm108 write: {}", e))?;
        Ok(())
    }
}
