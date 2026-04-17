//! Linux gpiochip v2 adapter for GPIO PTT.
//!
//! Thin wrapper over the `gpiocdev` crate, which speaks the Linux
//! `/dev/gpiochipN` character-device v2 API (`GPIO_V2_LINE_REQUEST`,
//! `GPIO_V2_LINE_SET_VALUES`). Parity with [`ptt_cm108_unix`]: open a
//! hardware handle in `open()`, twiddle a single line in `set_line()`,
//! map ioctl errno back to a small [`GpioError`] enum so upper layers
//! can evict the cached fd on device removal.
//!
//! The legacy sysfs interface (`/sys/class/gpio`) is deprecated since
//! Linux 4.8 and is intentionally NOT supported — `gpiocdev` speaks
//! only chardev v2.
//!
//! On drop, `gpiocdev::Request`'s own `Drop` closes the line fd. The
//! kernel's post-release line state depends on the SoC reset behavior
//! and is not guaranteed to be low, so the `GpioPtt` bridge struct in
//! `ptt.rs` calls `unkey()` before drop — graywolf drives the line low
//! before closing the fd, rather than relying on any post-release
//! kernel default.

use gpiocdev::line::Value;
use gpiocdev::request::Request;
use gpiocdev::Chip;

use super::{GpiochipControl, GpioError};

// errno constants from the Linux ABI (stable since forever). Kept as
// i32 literals rather than pulling in `libc` on top of what `gpiocdev`
// already drags in; the numeric values are stable.
const EACCES: i32 = 13;
const EBUSY: i32 = 16;
const ENOENT: i32 = 2;
const EPIPE: i32 = 32;
const EIO: i32 = 5;
const ENODEV: i32 = 19;
const ENXIO: i32 = 6;

/// Consumer string reported to the kernel — shows up in `gpioinfo`
/// output so operators can tell which process is holding the line.
const CONSUMER: &str = "graywolf-ptt";

/// Real gpiochip handle backed by `gpiocdev::Request`. One instance
/// owns exactly one requested line.
pub(super) struct LinuxGpiochip {
    request: Request,
    chip_path: String,
    line: u32,
}

impl LinuxGpiochip {
    /// Open the given chip, validate the line offset against the
    /// chip's `num_lines`, and request the line as an output driven
    /// inactive. Maps errno-level failures to [`GpioError`].
    pub(super) fn open(chip_path: &str, line: u32) -> Result<Self, GpioError> {
        if chip_path.is_empty() {
            return Err(GpioError::Other("chip path is empty".into()));
        }

        // Validate the line offset up front so we emit a descriptive
        // "out of range" message instead of a generic EINVAL from the
        // kernel when we try to request it.
        let chip = Chip::from_path(chip_path).map_err(|e| map_open_error(chip_path, e))?;
        let info = chip
            .info()
            .map_err(|e| map_open_error(chip_path, e))?;
        if line >= info.num_lines {
            return Err(GpioError::Other(format!(
                "gpio line {} out of range for {} (chip has {} lines, 0..{})",
                line,
                chip_path,
                info.num_lines,
                info.num_lines.saturating_sub(1)
            )));
        }
        // Drop the temporary Chip fd before requesting — `gpiocdev`
        // doesn't let us build a request from an existing Chip, and
        // holding both costs a second fd for no benefit.
        drop(chip);

        let request = Request::builder()
            .on_chip(chip_path)
            .with_consumer(CONSUMER)
            .with_line(line)
            .as_output(Value::Inactive)
            .request()
            .map_err(|e| map_open_error_with_line(chip_path, line, e))?;

        Ok(Self {
            request,
            chip_path: chip_path.to_string(),
            line,
        })
    }
}

impl GpiochipControl for LinuxGpiochip {
    fn set_line(&mut self, level: bool) -> Result<(), GpioError> {
        let value = if level { Value::Active } else { Value::Inactive };
        self.request
            .set_value(self.line, value)
            .map_err(|e| map_set_error(&self.chip_path, self.line, e))
    }
}

// Drop is implicit: `gpiocdev::Request`'s Drop closes the line fd.
// The GpioPtt bridge struct in ptt.rs is responsible for calling
// unkey() before drop so the line is driven low before close —
// graywolf drives the line low before closing the fd, rather than
// relying on any particular kernel/SoC post-release behavior.

/// Pull the raw errno out of a `gpiocdev::Error`, if there is one.
/// Returns `None` for errors that aren't backed by a system-call
/// failure (argument validation, missing ABI, etc.).
fn extract_errno(err: &gpiocdev::Error) -> Option<i32> {
    use gpiocdev_uapi::{Errno, Error as UapiError};
    match err {
        gpiocdev::Error::Os(Errno(n)) => Some(*n),
        gpiocdev::Error::Uapi(_, UapiError::Os(Errno(n))) => Some(*n),
        _ => None,
    }
}

fn map_open_error(chip_path: &str, err: gpiocdev::Error) -> GpioError {
    map_open_error_with_line(chip_path, 0, err)
}

fn map_open_error_with_line(chip_path: &str, line: u32, err: gpiocdev::Error) -> GpioError {
    match extract_errno(&err) {
        Some(EACCES) => GpioError::PermissionDenied {
            chip: chip_path.to_string(),
        },
        Some(EBUSY) => GpioError::Busy {
            chip: chip_path.to_string(),
            line,
        },
        // ENOENT / ENODEV / ENXIO during open → chip device doesn't
        // exist or has been unplugged. Surface as `Other` with a clear
        // message; there's nothing cached to evict yet.
        Some(ENOENT) | Some(ENODEV) | Some(ENXIO) => {
            GpioError::Other(format!("open {}: no such device: {}", chip_path, err))
        }
        _ => GpioError::Other(format!("open {}: {}", chip_path, err)),
    }
}

fn map_set_error(chip_path: &str, line: u32, err: gpiocdev::Error) -> GpioError {
    match extract_errno(&err) {
        // Post-open EPIPE / EIO / ENODEV / ENXIO mean the device went
        // away. Signal eviction upstream.
        Some(EPIPE) | Some(EIO) | Some(ENODEV) | Some(ENXIO) => GpioError::LineGone {
            chip: chip_path.to_string(),
            line,
        },
        Some(EACCES) => GpioError::PermissionDenied {
            chip: chip_path.to_string(),
        },
        Some(EBUSY) => GpioError::Busy {
            chip: chip_path.to_string(),
            line,
        },
        _ => GpioError::Other(format!("set_value {}:{}: {}", chip_path, line, err)),
    }
}
