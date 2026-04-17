//! PTT (push-to-talk) drivers for the TX path.
//!
//! Drives the radio's PTT line via serial modem-control handshake lines
//! (RTS/DTR), or leaves it as a no-op for VOX-keyed rigs. CM108 (USB HID)
//! and Linux GPIO slots exist in [`PttMethod`] for later phases; building
//! one today returns a typed error rather than silently falling through
//! to `None`, so a misconfigured channel fails loudly instead of keying
//! nothing on the air.
//!
//! ## Shared fds
//!
//! A single serial port is opened at most once per device path. Two
//! channels that share a device (e.g. one driving RTS, the other DTR)
//! both receive handles to the *same* underlying port through
//! [`PortRegistry`]. Opening the same TTY twice from one process either
//! fights on modem-control ioctls or fails outright on `flock`, so
//! direwolf caches serial fds per device and we follow suit. See
//! `direwolf/src/ptt.c:894-906, 937`.
//!
//! ## Platform adapters
//!
//! The hardware-facing code lives in two sibling modules that each
//! expose a single struct implementing [`ModemControlLines`]:
//!
//! - [`ptt_unix`] — `nix::fcntl::open` with
//!   `O_RDWR | O_NOCTTY | O_NONBLOCK | O_CLOEXEC`, zero termios calls,
//!   `ioctl(TIOCMSET)` for modem control. Mirrors direwolf's Unix path
//!   (`direwolf/src/ptt.c:928-960`). Never calling `tcsetattr` is
//!   deliberate: on some USB-serial adapters a `tcsetattr` briefly
//!   bounces the RTS/DTR lines, which would key PTT the moment we open
//!   the port.
//!
//! - [`ptt_win`] — `CreateFileW` in shared mode (`FILE_SHARE_READ |
//!   FILE_SHARE_WRITE`) plus `EscapeCommFunction(SET/CLR RTS/DTR)`.
//!   Mirrors direwolf's Windows path (`direwolf/src/ptt.c:920-925`).
//!   Windows has no termios analog, so the "don't touch termios"
//!   concern from the Unix side does not apply. Shared-mode is what
//!   lets `rigctld` / `fldigi` open the same COM port alongside us.
//!
//! Neither adapter reads or writes the device — they only move modem
//! control lines — so baud rate, parity, and line discipline are all
//! irrelevant.
//!
//! ## Startup unkey
//!
//! [`PortRegistry::serial_driver`] calls `driver.unkey()` before
//! returning. This is a direwolf-parity safety step: on Linux the
//! kernel's TTY layer asserts DTR during `open()` regardless of what
//! userspace asks for, and the explicit `ioctl(TIOCMSET)` we issue
//! immediately after `open()` narrows the window to microseconds — too
//! short for a mechanical relay or optoisolator to respond. Without
//! this, a DTR-keyed rig would transmit continuously from
//! ConfigurePtt until the first beacon.
//!
//! ## macOS device-name gotcha
//!
//! On macOS the DigiRig (and every other USB-serial adapter) shows up
//! twice: `/dev/cu.usbserial-*` and `/dev/tty.usbserial-*`. The `tty.*`
//! variant blocks `open()` forever waiting for DCD assert even with
//! `O_NONBLOCK`. This is a macOS TTY-subsystem behaviour, not something
//! any userspace crate addresses. The PTT UI hint and the loopback
//! README document this; configure graywolf with the `cu.*` path.
//!
//! ## Internal abstraction
//!
//! [`SerialLinePtt`] is written against [`ModemControlLines`] — a tiny
//! two-method trait — so tests can verify fd-sharing semantics with an
//! in-memory fake instead of touching real hardware, and so the Unix
//! and Windows adapters can slot in behind `#[cfg]` without the
//! higher-level code caring.

use std::collections::HashMap;
use std::sync::{Arc, Mutex};

#[cfg(unix)]
#[path = "ptt_unix.rs"]
mod ptt_unix;
#[cfg(windows)]
#[path = "ptt_win.rs"]
mod ptt_win;

#[cfg(target_os = "linux")]
#[path = "ptt_cm108_unix.rs"]
mod ptt_cm108_unix;
#[cfg(target_os = "macos")]
#[path = "ptt_cm108_macos.rs"]
mod ptt_cm108_macos;
#[cfg(windows)]
#[path = "ptt_cm108_win.rs"]
mod ptt_cm108_win;

#[cfg(target_os = "linux")]
#[path = "ptt_gpio_linux.rs"]
mod ptt_gpio_linux;

#[path = "ptt_rigctld.rs"]
mod ptt_rigctld;

#[cfg(unix)]
use ptt_unix::UnixSerialLines as PlatformSerialLines;
#[cfg(windows)]
use ptt_win::WinSerialLines as PlatformSerialLines;

#[cfg(target_os = "linux")]
use ptt_cm108_unix::UnixCm108Gpio as PlatformCm108Gpio;
#[cfg(target_os = "macos")]
use ptt_cm108_macos::MacCm108Gpio as PlatformCm108Gpio;
#[cfg(windows)]
use ptt_cm108_win::WinCm108Gpio as PlatformCm108Gpio;

use crate::ipc::proto::ConfigurePtt;

/// PTT hardware method, parsed from `ConfigurePtt.method`.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum PttMethod {
    /// VOX-keyed radio: audio alone triggers TX, no separate PTT line.
    None,
    SerialRts,
    SerialDtr,
    Cm108,
    Gpio,
    /// Hamlib rigctld over TCP. `device` is `"host:port"`.
    Rigctld,
}

impl PttMethod {
    /// Parse the `method` string from [`ConfigurePtt`]. Returns `None`
    /// for unrecognised values so [`PortRegistry::build_driver`] can
    /// surface an error to the operator — silently falling back to a
    /// no-op would hide typos like `"serial-rts"` as "radio never keys"
    /// with no log output.
    pub(crate) fn parse(s: &str) -> Option<Self> {
        match s {
            "" | "none" => Some(Self::None),
            "serial_rts" => Some(Self::SerialRts),
            "serial_dtr" => Some(Self::SerialDtr),
            "cm108" => Some(Self::Cm108),
            "gpio" => Some(Self::Gpio),
            "rigctld" => Some(Self::Rigctld),
            _ => None,
        }
    }
}

/// Per-channel PTT driver. Implementations are instantiated once per
/// channel by [`PortRegistry::build_driver`] and held inside an
/// `Arc<Mutex<..>>` by the modem so the TX worker can serialise key/unkey.
pub(crate) trait PttDriver: Send {
    /// Assert the PTT line (put the radio into transmit).
    fn key(&mut self) -> Result<(), String>;

    /// Release the PTT line (return the radio to receive).
    fn unkey(&mut self) -> Result<(), String>;
}

/// No-op driver for VOX-keyed rigs. The audio carrier itself triggers
/// the radio; we don't touch any GPIO / serial lines.
pub(crate) struct NonePtt;

impl PttDriver for NonePtt {
    fn key(&mut self) -> Result<(), String> {
        Ok(())
    }

    fn unkey(&mut self) -> Result<(), String> {
        Ok(())
    }
}

/// Which modem-control line on a serial port to toggle for PTT.
#[derive(Clone, Copy, Debug, PartialEq, Eq)]
pub(crate) enum SerialLine {
    Rts,
    Dtr,
}

/// Minimal hardware-facing interface the serial PTT driver actually
/// needs: just the two modem-control-line setters. Narrower than
/// `serialport::SerialPort`, which lets tests substitute an in-memory
/// fake without re-implementing the entire crate trait.
pub(crate) trait ModemControlLines: Send {
    fn write_rts(&mut self, level: bool) -> Result<(), String>;
    fn write_dtr(&mut self, level: bool) -> Result<(), String>;
}

/// Shared handle type used throughout the registry and the driver.
/// Two channels that point at the same device receive clones of the
/// same `Arc`, which is the whole point of the registry.
type SharedLines = Arc<Mutex<Box<dyn ModemControlLines>>>;

/// Shared handle type for CM108 HID devices. Same pattern as
/// [`SharedLines`] — two channels can share one CM108 adapter
/// (e.g. different GPIO pins on a multi-output board).
type SharedCm108 = Arc<Mutex<Box<dyn Cm108GpioControl>>>;

/// Shared handle type for Linux gpiochip line requests. One entry per
/// `chip:line` pair; the `gpiocdev::Request` inside owns both the line
/// fd and a chip fd. A second channel pointing at the same chip:line
/// would be unusual (only one PTT per radio) but is supported through
/// the standard Arc-clone pattern.
#[cfg(target_os = "linux")]
pub(crate) type SharedGpiochip = Arc<Mutex<Box<dyn GpiochipControl>>>;

/// Minimal hardware-facing interface for CM108 HID GPIO output reports.
/// Analogous to [`ModemControlLines`] for serial ports. Platform adapters
/// implement this behind `#[cfg]` — `UnixCm108Gpio` on Linux (via
/// `nix::unistd::write` to `/dev/hidrawN`); macOS and Windows adapters
/// follow in later steps.
pub(crate) trait Cm108GpioControl: Send {
    /// Write a CM108 HID output report to set or clear a GPIO pin.
    /// `pin` is 1-indexed (GPIO3 = pin 3 → mask 0x04).
    fn write_gpio(&mut self, pin: u8, level: bool) -> Result<(), String>;
}

/// Structured error type returned by [`GpiochipControl::set_line`]. Upper
/// layers pattern-match on these variants to decide whether to evict the
/// cached line fd (`LineGone`) vs. surface a user-visible error
/// (`PermissionDenied` / `Busy` / `Other`).
///
/// Linux-only: the gpiochip chardev API is a Linux kernel surface.
#[cfg(target_os = "linux")]
#[derive(Debug)]
pub(crate) enum GpioError {
    /// Post-open I/O failure (EPIPE / EIO / ENODEV / ENXIO). Hotplug
    /// class — the caller should drop the cached fd and let the next
    /// key() attempt reopen, mirroring the rigctld lazy-retry pattern.
    LineGone { chip: String, line: u32 },
    /// EACCES on open. User needs to be in the 'gpio' group (or
    /// equivalent plugdev on non-Raspbian distros) to access
    /// `/dev/gpiochipN`.
    PermissionDenied { chip: String },
    /// EBUSY — another driver (SPI, I2C, UART, or another user-space
    /// consumer) has claimed this line.
    Busy { chip: String, line: u32 },
    /// Any other failure, including invalid paths, out-of-range line
    /// offsets, or unrecognised ioctl errnos.
    Other(String),
}

#[cfg(target_os = "linux")]
impl std::fmt::Display for GpioError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        match self {
            GpioError::LineGone { chip, line } => write!(
                f,
                "gpio line {} on {}: device went away mid-operation",
                line, chip
            ),
            GpioError::PermissionDenied { chip } => write!(
                f,
                "gpio {}: permission denied — add user to 'gpio' group",
                chip
            ),
            GpioError::Busy { chip, line } => write!(
                f,
                "gpio line {} on {}: already claimed — check for kernel drivers (SPI, I2C, UART) using this pin",
                line, chip
            ),
            GpioError::Other(msg) => write!(f, "gpio: {}", msg),
        }
    }
}

#[cfg(target_os = "linux")]
impl std::error::Error for GpioError {}

/// Minimal hardware-facing interface for Linux gpiochip v2 line output.
/// Analogous to [`Cm108GpioControl`] for CM108 HID and [`ModemControlLines`]
/// for serial ports. The real impl lives in [`ptt_gpio_linux::LinuxGpiochip`];
/// tests substitute an in-memory fake.
#[cfg(target_os = "linux")]
pub(crate) trait GpiochipControl: Send {
    /// Drive the requested line active (`true`) or inactive (`false`).
    fn set_line(&mut self, level: bool) -> Result<(), GpioError>;
}

/// Serial-port PTT driver. Holds a shared reference to an already-open
/// serial port and toggles either RTS or DTR. `invert` is honoured for
/// rigs wired with reversed polarity — direwolf's `ptt_invert` at
/// `ptt.c:1380-1385`.
pub(crate) struct SerialLinePtt {
    port: SharedLines,
    line: SerialLine,
    invert: bool,
}

impl SerialLinePtt {
    fn set(&mut self, assert: bool) -> Result<(), String> {
        let level = assert ^ self.invert;
        let mut port = self
            .port
            .lock()
            .map_err(|e| format!("ptt port mutex poisoned: {}", e))?;
        match self.line {
            SerialLine::Rts => port.write_rts(level),
            SerialLine::Dtr => port.write_dtr(level),
        }
    }
}

impl PttDriver for SerialLinePtt {
    fn key(&mut self) -> Result<(), String> {
        self.set(true)
    }

    fn unkey(&mut self) -> Result<(), String> {
        self.set(false)
    }
}

/// CM108 HID GPIO PTT driver. Holds a shared reference to an
/// already-open HID device and toggles a single GPIO pin via
/// output reports. `invert` is honoured the same way as serial.
pub(crate) struct Cm108Ptt {
    port: SharedCm108,
    gpio_pin: u8,
    invert: bool,
}

impl Cm108Ptt {
    fn set(&mut self, assert: bool) -> Result<(), String> {
        let level = assert ^ self.invert;
        let mut port = self
            .port
            .lock()
            .map_err(|e| format!("cm108 mutex poisoned: {}", e))?;
        port.write_gpio(self.gpio_pin, level)
    }
}

impl PttDriver for Cm108Ptt {
    fn key(&mut self) -> Result<(), String> {
        self.set(true)
    }

    fn unkey(&mut self) -> Result<(), String> {
        self.set(false)
    }
}

impl Drop for Cm108Ptt {
    fn drop(&mut self) {
        // Unlike serial ports, closing a hidraw fd does NOT reset GPIO
        // state. A stuck PTT means continuous transmission — interference,
        // repeater lockout, potential FCC violation. Best-effort unkey on
        // drop; if the USB device is already disconnected, the write fails
        // silently and that's fine.
        let _ = self.unkey();
    }
}

/// Linux gpiochip v2 PTT driver. Holds a shared reference to a
/// requested gpio line (owned by the registry) and toggles it via
/// `set_line`. `invert` is honoured the same way as serial / CM108.
///
/// `chip_path` and `line` are retained so that on [`GpioError::LineGone`]
/// the driver can tell the registry which cache entry to evict. The
/// registry's `gpio_lines` cache is keyed by `"chip:line"` (see
/// [`gpio_line_key`]); evicting on `LineGone` mirrors the rigctld
/// driver's lazy-retry-on-disconnect behaviour.
#[cfg(target_os = "linux")]
pub(crate) struct GpioPtt {
    port: SharedGpiochip,
    invert: bool,
    chip_path: String,
    line: u32,
    /// Shared handle back to the registry's line cache. On `LineGone`
    /// we evict our entry so the next `build_driver` call reopens.
    /// Held as a weak reference to avoid a cycle (the registry owns
    /// the map this driver's handle lives in).
    eviction_cache: std::sync::Weak<Mutex<HashMap<String, SharedGpiochip>>>,
}

#[cfg(target_os = "linux")]
impl GpioPtt {
    fn set(&mut self, assert: bool) -> Result<(), String> {
        let level = assert ^ self.invert;
        let result = {
            let mut port = self
                .port
                .lock()
                .map_err(|e| format!("gpio port mutex poisoned: {}", e))?;
            port.set_line(level)
        };
        match result {
            Ok(()) => Ok(()),
            Err(e @ GpioError::LineGone { .. }) => {
                // Device removed mid-operation. Evict the cached line
                // fd so the next build_driver call reopens cleanly,
                // then surface the error so the current TX attempt
                // fails loudly rather than silently keying nothing.
                // Mirrors rigctld's ensure_connected() drop-on-down.
                self.evict_from_cache();
                Err(format!("{}", e))
            }
            Err(e) => Err(format!("{}", e)),
        }
    }

    fn evict_from_cache(&self) {
        if let Some(cache) = self.eviction_cache.upgrade() {
            if let Ok(mut map) = cache.lock() {
                map.remove(&gpio_line_key(&self.chip_path, self.line));
            }
        }
    }
}

#[cfg(target_os = "linux")]
impl PttDriver for GpioPtt {
    fn key(&mut self) -> Result<(), String> {
        self.set(true)
    }

    fn unkey(&mut self) -> Result<(), String> {
        self.set(false)
    }
}

#[cfg(target_os = "linux")]
impl Drop for GpioPtt {
    fn drop(&mut self) {
        // graywolf drives the line low before closing the fd — the
        // kernel's post-release line state depends on the SoC reset
        // behavior and is not guaranteed to be low. Best-effort: any
        // Err from unkey() (device already gone, etc.) is swallowed
        // here because there's no reasonable recovery in Drop and the
        // kernel closes the fd anyway.
        let _ = self.unkey();
    }
}

/// Registry cache key for a (chip_path, line) pair. Stable across the
/// lifetime of the process; used by both the registry map and
/// [`GpioPtt::evict_from_cache`].
#[cfg(target_os = "linux")]
fn gpio_line_key(chip_path: &str, line: u32) -> String {
    format!("{}:{}", chip_path, line)
}

/// Cache of open serial ports keyed by device path. A single port
/// handle is reused by every channel that points at the same device,
/// regardless of which modem-control line that channel drives. This
/// prevents the "open twice → second open fights the first on ioctls"
/// class of bugs direwolf documents and is the reason the registry
/// exists as a separate type rather than as free functions.
///
/// Known limitation: the registry holds open ports indefinitely. If a
/// channel is reconfigured to point at a new device, the old port stays
/// cached until the process exits. Given a PTT deployment of one or
/// two fds, this is cheaper than adding ref counting.
pub(crate) struct PortRegistry {
    ports: HashMap<String, SharedLines>,
    cm108_ports: HashMap<String, SharedCm108>,
    /// Cache of open gpiochip line requests, keyed by `"chip_path:line"`.
    /// Wrapped in `Arc<Mutex<..>>` (unlike the other registry maps) so
    /// that `GpioPtt` instances can hold a `Weak` clone and evict their
    /// own entry when `set_line()` returns `GpioError::LineGone` —
    /// parity with rigctld's lazy-reconnect-on-disconnect behaviour.
    ///
    /// Note on chip-fd sharing: `gpiocdev::Request` owns both its line
    /// fd *and* the chip fd it opened internally; there is no public
    /// API for sharing a chip fd across multiple `Request`s. A separate
    /// `gpio_chips` refcount map would therefore cache handles the
    /// library doesn't let us reuse, buying nothing. Per-line caching
    /// on its own still de-duplicates the (rare) case of two channels
    /// sharing one gpiochip line.
    #[cfg(target_os = "linux")]
    gpio_lines: Arc<Mutex<HashMap<String, SharedGpiochip>>>,
}

impl PortRegistry {
    /// Build an empty registry. The modem owns one for the lifetime of
    /// the process; dropping it closes every cached port.
    pub(crate) fn new() -> Self {
        Self {
            ports: HashMap::new(),
            cm108_ports: HashMap::new(),
            #[cfg(target_os = "linux")]
            gpio_lines: Arc::new(Mutex::new(HashMap::new())),
        }
    }

    /// Build a [`PttDriver`] for the given channel configuration. May
    /// open a new serial port as a side effect (and cache it for reuse
    /// on subsequent calls with the same `device`). Returns an error
    /// for unknown method strings and for `gpio` until that driver is
    /// implemented — the caller logs the error and leaves
    /// the channel driverless so a later TX attempt fails loudly rather
    /// than silently keying nothing.
    pub(crate) fn build_driver(
        &mut self,
        cfg: &ConfigurePtt,
    ) -> Result<Box<dyn PttDriver>, String> {
        let method = PttMethod::parse(&cfg.method)
            .ok_or_else(|| format!("unknown ptt method '{}'", cfg.method))?;
        match method {
            PttMethod::None => Ok(Box::new(NonePtt)),
            PttMethod::SerialRts => Ok(Box::new(self.serial_driver(
                &cfg.device,
                SerialLine::Rts,
                cfg.invert,
            )?)),
            PttMethod::SerialDtr => Ok(Box::new(self.serial_driver(
                &cfg.device,
                SerialLine::Dtr,
                cfg.invert,
            )?)),
            PttMethod::Cm108 => {
                let raw = cfg.gpio_pin;
                let gpio_pin = if raw == 0 {
                    3u8
                } else if raw > 8 {
                    return Err(format!("cm108 gpio_pin {} out of range (1-8)", raw));
                } else {
                    raw as u8
                };
                Ok(Box::new(self.cm108_driver(&cfg.device, gpio_pin, cfg.invert)?))
            }
            PttMethod::Gpio => {
                #[cfg(target_os = "linux")]
                {
                    Ok(Box::new(self.gpio_driver(
                        &cfg.device,
                        cfg.gpio_line,
                        cfg.invert,
                    )?))
                }
                #[cfg(not(target_os = "linux"))]
                {
                    let _ = cfg;
                    Err("gpio ptt is only supported on Linux".into())
                }
            }
            PttMethod::Rigctld => {
                if cfg.device.is_empty() {
                    return Err("rigctld ptt: device (host:port) is empty".into());
                }
                Ok(Box::new(ptt_rigctld::RigctldPtt::connect(&cfg.device)?))
            }
        }
    }

    fn serial_driver(
        &mut self,
        device: &str,
        line: SerialLine,
        invert: bool,
    ) -> Result<SerialLinePtt, String> {
        if device.is_empty() {
            return Err("serial ptt: device path is empty".into());
        }
        let port = self.open_or_reuse(device)?;
        let mut driver = SerialLinePtt { port, line, invert };
        // Force the line into its unkeyed state before returning.
        // Otherwise on Linux the kernel's TTY open() leaves DTR asserted
        // (see direwolf ptt.c:940-960 for the equivalent TIOCMSET clear),
        // and on any platform the line's prior state is whatever the
        // previous process or hardware default left it at. Direwolf
        // parity: the radio is unkeyed by construction, not by luck.
        driver.unkey()?;
        Ok(driver)
    }

    /// Look up or open the serial port for `device`. Returns the shared
    /// handle; the registry retains a clone for reuse.
    fn open_or_reuse(&mut self, device: &str) -> Result<SharedLines, String> {
        if let Some(port) = self.ports.get(device) {
            return Ok(port.clone());
        }
        let lines: Box<dyn ModemControlLines> = Box::new(PlatformSerialLines::open(device)?);
        let shared: SharedLines = Arc::new(Mutex::new(lines));
        self.ports.insert(device.to_string(), shared.clone());
        Ok(shared)
    }

    fn cm108_driver(
        &mut self,
        device: &str,
        gpio_pin: u8,
        invert: bool,
    ) -> Result<Cm108Ptt, String> {
        if device.is_empty() {
            return Err("cm108 ptt: device path is empty".into());
        }
        let port = self.open_or_reuse_cm108(device)?;
        let mut driver = Cm108Ptt {
            port,
            gpio_pin,
            invert,
        };
        driver.unkey()?;
        Ok(driver)
    }

    fn open_or_reuse_cm108(&mut self, device: &str) -> Result<SharedCm108, String> {
        if let Some(port) = self.cm108_ports.get(device) {
            return Ok(port.clone());
        }
        let gpio: Box<dyn Cm108GpioControl> = Box::new(PlatformCm108Gpio::open(device)?);
        let shared: SharedCm108 = Arc::new(Mutex::new(gpio));
        self.cm108_ports.insert(device.to_string(), shared.clone());
        Ok(shared)
    }

    /// Build a [`GpioPtt`] for the given chip path / line / invert.
    /// Reuses an existing `chip:line` handle if one is cached; opens a
    /// new gpiochip request otherwise. Mirrors [`cm108_driver`].
    #[cfg(target_os = "linux")]
    fn gpio_driver(
        &mut self,
        device: &str,
        line: u32,
        invert: bool,
    ) -> Result<GpioPtt, String> {
        if device.is_empty() {
            return Err("gpio ptt: device path is empty".into());
        }
        let port = self.open_or_reuse_gpio(device, line)?;
        let mut driver = GpioPtt {
            port,
            invert,
            chip_path: device.to_string(),
            line,
            eviction_cache: Arc::downgrade(&self.gpio_lines),
        };
        // Known-low start state, parity with serial_driver / cm108_driver.
        driver.unkey()?;
        Ok(driver)
    }

    /// Look up or open the gpiochip line handle for `(device, line)`.
    /// Returns the shared handle; the registry retains a clone for
    /// reuse and for `GpioPtt::evict_from_cache`.
    #[cfg(target_os = "linux")]
    fn open_or_reuse_gpio(
        &mut self,
        device: &str,
        line: u32,
    ) -> Result<SharedGpiochip, String> {
        let key = gpio_line_key(device, line);
        {
            let map = self
                .gpio_lines
                .lock()
                .map_err(|e| format!("gpio registry mutex poisoned: {}", e))?;
            if let Some(port) = map.get(&key) {
                return Ok(port.clone());
            }
        }
        // Open outside the lock so a slow kernel call doesn't block
        // other threads. The slot is only populated after a successful
        // open, so a concurrent open of the same key is harmless
        // (last-writer-wins; the loser's handle drops cleanly).
        let gpio: Box<dyn GpiochipControl> = Box::new(
            ptt_gpio_linux::LinuxGpiochip::open(device, line).map_err(|e| format!("{}", e))?,
        );
        let shared: SharedGpiochip = Arc::new(Mutex::new(gpio));
        let mut map = self
            .gpio_lines
            .lock()
            .map_err(|e| format!("gpio registry mutex poisoned: {}", e))?;
        // If a racing open beat us to it, drop ours and return the
        // winner's so both channels share one handle.
        if let Some(existing) = map.get(&key) {
            return Ok(existing.clone());
        }
        map.insert(key, shared.clone());
        Ok(shared)
    }

    /// Test hook: pre-install a fake serial port so tests can verify
    /// fd-sharing semantics without touching real hardware.
    #[cfg(test)]
    fn install_for_test(&mut self, device: &str, port: SharedLines) {
        self.ports.insert(device.to_string(), port);
    }

    /// Test hook: pre-install a fake CM108 port.
    #[cfg(test)]
    fn install_cm108_for_test(&mut self, device: &str, port: SharedCm108) {
        self.cm108_ports.insert(device.to_string(), port);
    }

    /// Test hook: pre-install a fake gpiochip line handle under
    /// `"chip_path:line"` (see [`gpio_line_key`]). Phase 2 uses this
    /// to exercise key/unkey, invert, construction unkey, and
    /// LineGone-triggered eviction without touching /dev/gpiochip*.
    #[cfg(all(test, target_os = "linux"))]
    pub(crate) fn install_gpio_for_test(&mut self, key: &str, port: SharedGpiochip) {
        self.gpio_lines
            .lock()
            .expect("gpio test cache mutex poisoned")
            .insert(key.to_string(), port);
    }
}

#[cfg(test)]
pub(crate) mod tests {
    use super::*;

    /// Shared log of recorded modem-control operations. Cloneable so
    /// the test body can keep a tap after the fake that writes into it
    /// has been moved behind a `dyn ModemControlLines` object.
    type OpLog = Arc<Mutex<Vec<(SerialLine, bool)>>>;

    /// In-memory [`ModemControlLines`] for tests. Recorded operations
    /// live in a cloneable [`OpLog`] the test body owns separately, so
    /// assertions never need to downcast the trait object.
    struct FakeLines {
        ops: OpLog,
    }

    impl ModemControlLines for FakeLines {
        fn write_rts(&mut self, level: bool) -> Result<(), String> {
            self.ops.lock().unwrap().push((SerialLine::Rts, level));
            Ok(())
        }

        fn write_dtr(&mut self, level: bool) -> Result<(), String> {
            self.ops.lock().unwrap().push((SerialLine::Dtr, level));
            Ok(())
        }
    }

    /// Build a shared port backed by an in-memory [`FakeLines`]. The
    /// returned handle is the port to install in a [`PortRegistry`]; the
    /// [`OpLog`] lets the test read back the recorded calls.
    fn shared_fake() -> (SharedLines, OpLog) {
        let ops: OpLog = Arc::new(Mutex::new(Vec::new()));
        let fake = FakeLines { ops: ops.clone() };
        let shared: SharedLines = Arc::new(Mutex::new(Box::new(fake)));
        (shared, ops)
    }

    fn base_cfg() -> ConfigurePtt {
        ConfigurePtt {
            channel: 0,
            method: String::new(),
            device: String::new(),
            txdelay_ms: 0,
            txtail_ms: 0,
            slottime_ms: 0,
            persist: 0,
            dwait_ms: 0,
            invert: false,
            gpio_pin: 3,
            gpio_line: 0,
        }
    }

    #[test]
    fn none_driver_key_and_unkey_are_noops_and_never_fail() {
        let mut driver = NonePtt;
        assert!(driver.key().is_ok());
        assert!(driver.unkey().is_ok());
    }

    #[test]
    fn parse_recognizes_known_method_strings_and_returns_none_for_unknown() {
        assert_eq!(PttMethod::parse("none"), Some(PttMethod::None));
        assert_eq!(PttMethod::parse(""), Some(PttMethod::None));
        assert_eq!(PttMethod::parse("serial_rts"), Some(PttMethod::SerialRts));
        assert_eq!(PttMethod::parse("serial_dtr"), Some(PttMethod::SerialDtr));
        assert_eq!(PttMethod::parse("cm108"), Some(PttMethod::Cm108));
        assert_eq!(PttMethod::parse("gpio"), Some(PttMethod::Gpio));
        assert_eq!(PttMethod::parse("rigctld"), Some(PttMethod::Rigctld));
        // Typos must surface as errors at build_driver time rather
        // than silently folding into a no-op — "radio never keys"
        // with no log output is the worst possible debug experience.
        assert_eq!(PttMethod::parse("serial-rts"), None);
        assert_eq!(PttMethod::parse("serial rts"), None);
        assert_eq!(PttMethod::parse("magic_new_method"), None);
    }

    #[test]
    fn build_driver_with_none_method_yields_noop_driver() {
        let mut registry = PortRegistry::new();
        let cfg = ConfigurePtt {
            method: "none".into(),
            ..base_cfg()
        };
        let mut driver = registry.build_driver(&cfg).expect("none always builds");
        assert!(driver.key().is_ok());
        assert!(driver.unkey().is_ok());
    }

    /// Destructure a `Box<dyn PttDriver>` Result into the error variant.
    /// `unwrap_err` would require `dyn PttDriver: Debug`, which we
    /// deliberately don't require of the production trait.
    fn expect_err(result: Result<Box<dyn PttDriver>, String>) -> String {
        match result {
            Err(e) => e,
            Ok(_) => panic!("expected build_driver to fail"),
        }
    }

    #[test]
    fn build_driver_rejects_empty_device_path_for_serial_methods() {
        let mut registry = PortRegistry::new();
        let cfg = ConfigurePtt {
            method: "serial_rts".into(),
            ..base_cfg()
        };
        let err = expect_err(registry.build_driver(&cfg));
        assert!(
            err.contains("device path is empty"),
            "unexpected error: {}",
            err
        );
    }

    /// On non-Linux, selecting GPIO must report the platform restriction
    /// loudly rather than silently falling back to a no-op. Phase 2
    /// owns the Linux-side build_driver tests; this just guards the
    /// cross-platform behaviour contract.
    #[cfg(not(target_os = "linux"))]
    #[test]
    fn build_driver_rejects_gpio_on_non_linux() {
        let mut registry = PortRegistry::new();
        let mut gpio = base_cfg();
        gpio.method = "gpio".into();
        gpio.device = "/dev/null".into();
        let err = expect_err(registry.build_driver(&gpio));
        assert!(
            err.contains("only supported on Linux"),
            "unexpected error: {}",
            err
        );
    }

    #[test]
    fn serial_line_ptt_writes_rts_high_on_key_and_low_on_unkey() {
        let (port, ops) = shared_fake();
        let mut driver = SerialLinePtt {
            port,
            line: SerialLine::Rts,
            invert: false,
        };
        driver.key().unwrap();
        driver.unkey().unwrap();
        assert_eq!(
            *ops.lock().unwrap(),
            vec![(SerialLine::Rts, true), (SerialLine::Rts, false)]
        );
    }

    #[test]
    fn serial_line_ptt_writes_dtr_high_on_key_and_low_on_unkey() {
        let (port, ops) = shared_fake();
        let mut driver = SerialLinePtt {
            port,
            line: SerialLine::Dtr,
            invert: false,
        };
        driver.key().unwrap();
        driver.unkey().unwrap();
        assert_eq!(
            *ops.lock().unwrap(),
            vec![(SerialLine::Dtr, true), (SerialLine::Dtr, false)]
        );
    }

    #[test]
    fn invert_flag_reverses_polarity_of_key_and_unkey() {
        let (port, ops) = shared_fake();
        let mut driver = SerialLinePtt {
            port,
            line: SerialLine::Rts,
            invert: true,
        };
        driver.key().unwrap();
        driver.unkey().unwrap();
        assert_eq!(
            *ops.lock().unwrap(),
            vec![(SerialLine::Rts, false), (SerialLine::Rts, true)]
        );
    }

    #[test]
    fn registry_reuses_one_port_for_two_channels_sharing_a_device() {
        let mut registry = PortRegistry::new();
        let (shared, ops) = shared_fake();
        registry.install_for_test("/dev/fake", shared.clone());

        // strong_count should be 2 after install (registry + test handle).
        assert_eq!(Arc::strong_count(&shared), 2);

        let rts_cfg = ConfigurePtt {
            method: "serial_rts".into(),
            device: "/dev/fake".into(),
            channel: 0,
            ..base_cfg()
        };
        let dtr_cfg = ConfigurePtt {
            method: "serial_dtr".into(),
            device: "/dev/fake".into(),
            channel: 1,
            ..base_cfg()
        };

        let mut rts_driver = registry.build_driver(&rts_cfg).unwrap();
        let mut dtr_driver = registry.build_driver(&dtr_cfg).unwrap();

        // Both drivers hold an Arc clone of the same underlying port,
        // so strong_count climbs to registry + test handle + 2 drivers.
        assert_eq!(Arc::strong_count(&shared), 4);

        // Keying one channel does not disturb the other's line, and
        // both operations land on the same fake — proving they share
        // one handle.
        rts_driver.key().unwrap();
        dtr_driver.key().unwrap();
        rts_driver.unkey().unwrap();
        dtr_driver.unkey().unwrap();

        assert_eq!(
            *ops.lock().unwrap(),
            vec![
                // Initial unkey-on-construct: each driver clears its
                // own line before serial_driver() returns.
                (SerialLine::Rts, false),
                (SerialLine::Dtr, false),
                // Explicit key/unkey cycles.
                (SerialLine::Rts, true),
                (SerialLine::Dtr, true),
                (SerialLine::Rts, false),
                (SerialLine::Dtr, false),
            ]
        );
    }

    #[test]
    fn serial_driver_unkeys_the_line_immediately_after_construction() {
        // Regression: without the unkey() in serial_driver(), a Linux
        // box opening a DTR-keyed rig's port would leave DTR asserted
        // (the kernel sets it during tty_port_open) from ConfigurePtt
        // until the first beacon — continuously transmitting until then.
        // Direwolf ptt.c:940-960 clears the line for the same reason.
        let mut registry = PortRegistry::new();
        let (port, ops) = shared_fake();
        registry.install_for_test("/dev/fake", port);

        let cfg = ConfigurePtt {
            method: "serial_dtr".into(),
            device: "/dev/fake".into(),
            channel: 0,
            ..base_cfg()
        };
        let _driver = registry.build_driver(&cfg).unwrap();

        assert_eq!(
            *ops.lock().unwrap(),
            vec![(SerialLine::Dtr, false)],
            "serial_driver must unkey() before returning"
        );
    }

    #[test]
    fn serial_driver_respects_invert_during_construction_unkey() {
        // invert=true + unkey() → set(assert=false) → level = false ^ true = true.
        // The initial unkey must honor invert so an inverted rig isn't
        // keyed during the ConfigurePtt-to-first-beacon window.
        let mut registry = PortRegistry::new();
        let (port, ops) = shared_fake();
        registry.install_for_test("/dev/fake", port);

        let cfg = ConfigurePtt {
            method: "serial_rts".into(),
            device: "/dev/fake".into(),
            channel: 0,
            invert: true,
            ..base_cfg()
        };
        let _driver = registry.build_driver(&cfg).unwrap();

        assert_eq!(*ops.lock().unwrap(), vec![(SerialLine::Rts, true)],);
    }

    #[test]
    fn build_driver_rejects_unknown_method_with_descriptive_error() {
        let mut registry = PortRegistry::new();
        let cfg = ConfigurePtt {
            method: "serial-rts".into(), // dash instead of underscore
            device: "/dev/fake".into(),
            channel: 0,
            ..base_cfg()
        };
        let err = expect_err(registry.build_driver(&cfg));
        assert!(
            err.contains("unknown ptt method") && err.contains("serial-rts"),
            "unexpected error: {}",
            err
        );
    }

    // --- CM108 fakes and tests ---

    /// Recorded CM108 GPIO operations: (pin, level).
    type Cm108OpLog = Arc<Mutex<Vec<(u8, bool)>>>;

    struct FakeCm108 {
        ops: Cm108OpLog,
    }

    impl Cm108GpioControl for FakeCm108 {
        fn write_gpio(&mut self, pin: u8, level: bool) -> Result<(), String> {
            self.ops.lock().unwrap().push((pin, level));
            Ok(())
        }
    }

    fn shared_fake_cm108() -> (SharedCm108, Cm108OpLog) {
        let ops: Cm108OpLog = Arc::new(Mutex::new(Vec::new()));
        let fake = FakeCm108 { ops: ops.clone() };
        let shared: SharedCm108 = Arc::new(Mutex::new(Box::new(fake)));
        (shared, ops)
    }

    #[test]
    fn cm108_ptt_writes_gpio_high_on_key_and_low_on_unkey() {
        let (port, ops) = shared_fake_cm108();
        let mut driver = Cm108Ptt {
            port,
            gpio_pin: 3,
            invert: false,
        };
        driver.key().unwrap();
        driver.unkey().unwrap();
        assert_eq!(*ops.lock().unwrap(), vec![(3, true), (3, false)]);
    }

    #[test]
    fn cm108_ptt_invert_reverses_polarity() {
        let (port, ops) = shared_fake_cm108();
        let mut driver = Cm108Ptt {
            port,
            gpio_pin: 3,
            invert: true,
        };
        driver.key().unwrap();
        driver.unkey().unwrap();
        assert_eq!(*ops.lock().unwrap(), vec![(3, false), (3, true)]);
    }

    #[test]
    fn cm108_driver_unkeys_on_construction() {
        let mut registry = PortRegistry::new();
        let (port, ops) = shared_fake_cm108();
        registry.install_cm108_for_test("/dev/fake-cm108", port);

        let cfg = ConfigurePtt {
            method: "cm108".into(),
            device: "/dev/fake-cm108".into(),
            gpio_pin: 3,
            ..base_cfg()
        };
        let _driver = registry.build_driver(&cfg).unwrap();

        assert_eq!(
            *ops.lock().unwrap(),
            vec![(3, false)],
            "cm108_driver must unkey() before returning"
        );
    }

    #[test]
    fn cm108_driver_defaults_zero_gpio_pin_to_3() {
        let mut registry = PortRegistry::new();
        let (port, ops) = shared_fake_cm108();
        registry.install_cm108_for_test("/dev/fake-cm108", port);

        let cfg = ConfigurePtt {
            method: "cm108".into(),
            device: "/dev/fake-cm108".into(),
            gpio_pin: 0, // should default to 3
            ..base_cfg()
        };
        let _driver = registry.build_driver(&cfg).unwrap();

        // unkey-on-construction writes pin 3 (the default)
        assert_eq!(*ops.lock().unwrap(), vec![(3, false)]);
    }

    #[test]
    fn cm108_driver_rejects_pin_out_of_range() {
        let mut registry = PortRegistry::new();
        let cfg = ConfigurePtt {
            method: "cm108".into(),
            device: "/dev/fake-cm108".into(),
            gpio_pin: 9,
            ..base_cfg()
        };
        let err = expect_err(registry.build_driver(&cfg));
        assert!(
            err.contains("out of range"),
            "unexpected error: {}",
            err
        );
    }

    #[test]
    fn cm108_driver_rejects_empty_device_path() {
        let mut registry = PortRegistry::new();
        let cfg = ConfigurePtt {
            method: "cm108".into(),
            device: String::new(),
            gpio_pin: 3,
            ..base_cfg()
        };
        let err = expect_err(registry.build_driver(&cfg));
        assert!(
            err.contains("device path is empty"),
            "unexpected error: {}",
            err
        );
    }

    #[test]
    fn cm108_registry_reuses_port_for_two_channels() {
        let mut registry = PortRegistry::new();
        let (shared, ops) = shared_fake_cm108();
        registry.install_cm108_for_test("/dev/fake-cm108", shared.clone());

        assert_eq!(Arc::strong_count(&shared), 2);

        let cfg_a = ConfigurePtt {
            method: "cm108".into(),
            device: "/dev/fake-cm108".into(),
            channel: 0,
            gpio_pin: 3,
            ..base_cfg()
        };
        let cfg_b = ConfigurePtt {
            method: "cm108".into(),
            device: "/dev/fake-cm108".into(),
            channel: 1,
            gpio_pin: 2,
            ..base_cfg()
        };

        let mut drv_a = registry.build_driver(&cfg_a).unwrap();
        let mut drv_b = registry.build_driver(&cfg_b).unwrap();

        // registry + test handle + 2 drivers
        assert_eq!(Arc::strong_count(&shared), 4);

        drv_a.key().unwrap();
        drv_b.key().unwrap();
        drv_a.unkey().unwrap();
        drv_b.unkey().unwrap();

        assert_eq!(
            *ops.lock().unwrap(),
            vec![
                // Construction unkeys
                (3, false),
                (2, false),
                // Explicit key/unkey
                (3, true),
                (2, true),
                (3, false),
                (2, false),
            ]
        );
    }

    #[test]
    fn cm108_ptt_drop_calls_unkey() {
        let (port, ops) = shared_fake_cm108();
        {
            let _driver = Cm108Ptt {
                port,
                gpio_pin: 3,
                invert: false,
            };
            // driver drops here
        }
        assert_eq!(
            *ops.lock().unwrap(),
            vec![(3, false)],
            "Drop must call unkey()"
        );
    }

    // --- GPIO (Linux gpiochip v2) fakes and tests ---
    //
    // Mirrors the CM108 block above. The fake records every `set_line`
    // call into a shared [`GpioOpLog`] the test owns, and optionally
    // returns a pre-armed [`GpioError`] on the next call so tests can
    // drive the `LineGone` eviction path without real kernel state.

    /// Recorded gpiochip operations: just the level written (true = high/Active,
    /// false = low/Inactive). There is only one line per fake, so no
    /// offset is recorded — the `chip:line` identity is fixed by how the
    /// test installs the fake into the registry.
    #[cfg(all(test, target_os = "linux"))]
    type GpioOpLog = Arc<Mutex<Vec<bool>>>;

    /// In-memory [`GpiochipControl`] for tests. `next_error` is a
    /// single-shot slot: whatever error is armed there is returned on
    /// the next `set_line` call, then cleared. Tests use this to trigger
    /// `GpioError::LineGone` exactly once and observe the eviction.
    #[cfg(all(test, target_os = "linux"))]
    struct FakeGpiochip {
        ops: GpioOpLog,
        next_error: Option<GpioError>,
    }

    #[cfg(all(test, target_os = "linux"))]
    impl GpiochipControl for FakeGpiochip {
        fn set_line(&mut self, level: bool) -> Result<(), GpioError> {
            if let Some(err) = self.next_error.take() {
                return Err(err);
            }
            self.ops.lock().expect("ops log mutex poisoned").push(level);
            Ok(())
        }
    }

    /// Build a shared gpiochip handle backed by an in-memory
    /// [`FakeGpiochip`] with no error armed. Mirrors [`shared_fake_cm108`].
    #[cfg(all(test, target_os = "linux"))]
    fn shared_fake_gpio() -> (SharedGpiochip, GpioOpLog) {
        let ops: GpioOpLog = Arc::new(Mutex::new(Vec::new()));
        let fake = FakeGpiochip {
            ops: ops.clone(),
            next_error: None,
        };
        let shared: SharedGpiochip = Arc::new(Mutex::new(Box::new(fake)));
        (shared, ops)
    }

    /// Build a shared gpiochip handle with a one-shot error armed on
    /// the next `set_line` call. Returns the handle and a cloneable log.
    #[cfg(all(test, target_os = "linux"))]
    fn shared_fake_gpio_with_error(err: GpioError) -> (SharedGpiochip, GpioOpLog) {
        let ops: GpioOpLog = Arc::new(Mutex::new(Vec::new()));
        let fake = FakeGpiochip {
            ops: ops.clone(),
            next_error: Some(err),
        };
        let shared: SharedGpiochip = Arc::new(Mutex::new(Box::new(fake)));
        (shared, ops)
    }

    #[cfg(all(test, target_os = "linux"))]
    #[test]
    fn gpio_ptt_writes_high_on_key_and_low_on_unkey() {
        // Direct-struct parity with `cm108_ptt_writes_gpio_high_on_key_and_low_on_unkey`.
        // A freshly constructed GpioPtt with invert=false writes
        // Active on key() and Inactive on unkey().
        let (port, ops) = shared_fake_gpio();
        let mut driver = GpioPtt {
            port,
            invert: false,
            chip_path: "/dev/gpiochip0".into(),
            line: 17,
            // Construction outside the registry: no cache to evict into.
            eviction_cache: std::sync::Weak::new(),
        };
        driver.key().unwrap();
        driver.unkey().unwrap();
        assert_eq!(*ops.lock().unwrap(), vec![true, false]);
    }

    #[cfg(all(test, target_os = "linux"))]
    #[test]
    fn gpio_ptt_invert_reverses_polarity() {
        // Mirror of `cm108_ptt_invert_reverses_polarity`: with invert=true,
        // key() should drive the line Inactive and unkey() Active.
        let (port, ops) = shared_fake_gpio();
        let mut driver = GpioPtt {
            port,
            invert: true,
            chip_path: "/dev/gpiochip0".into(),
            line: 17,
            eviction_cache: std::sync::Weak::new(),
        };
        driver.key().unwrap();
        driver.unkey().unwrap();
        assert_eq!(*ops.lock().unwrap(), vec![false, true]);
    }

    #[cfg(all(test, target_os = "linux"))]
    #[test]
    fn gpio_driver_unkeys_on_construction() {
        // Mirror of `cm108_driver_unkeys_on_construction`: build_driver
        // must call unkey() before returning so the line starts known-low
        // even if the previous process or hardware default left it high.
        let mut registry = PortRegistry::new();
        let (port, ops) = shared_fake_gpio();
        registry.install_gpio_for_test("/dev/fake-gpiochip:17", port);

        let cfg = ConfigurePtt {
            method: "gpio".into(),
            device: "/dev/fake-gpiochip".into(),
            gpio_line: 17,
            ..base_cfg()
        };
        let _driver = registry.build_driver(&cfg).unwrap();

        assert_eq!(
            *ops.lock().unwrap(),
            vec![false],
            "gpio_driver must unkey() before returning"
        );
    }

    #[cfg(all(test, target_os = "linux"))]
    #[test]
    fn gpio_driver_rejects_empty_device_path() {
        // Mirror of `cm108_driver_rejects_empty_device_path`. Validation
        // must trip before any kernel interaction.
        let mut registry = PortRegistry::new();
        let cfg = ConfigurePtt {
            method: "gpio".into(),
            device: String::new(),
            gpio_line: 0,
            ..base_cfg()
        };
        let err = expect_err(registry.build_driver(&cfg));
        assert!(
            err.contains("device path is empty"),
            "unexpected error: {}",
            err
        );
    }

    #[cfg(all(test, target_os = "linux"))]
    #[test]
    fn gpio_ptt_drop_calls_unkey() {
        // Mirror of `cm108_ptt_drop_calls_unkey`: a stuck PTT means
        // continuous transmission (repeater lockout, FCC problem). Drop
        // must best-effort drive the line low. The gpiochip kernel
        // post-release state is not guaranteed, so graywolf drives the
        // line low explicitly before close — see GpioPtt::drop.
        let (port, ops) = shared_fake_gpio();
        {
            let _driver = GpioPtt {
                port,
                invert: false,
                chip_path: "/dev/gpiochip0".into(),
                line: 17,
                eviction_cache: std::sync::Weak::new(),
            };
            // driver drops here
        }
        assert_eq!(
            *ops.lock().unwrap(),
            vec![false],
            "Drop must call unkey()"
        );
    }

    #[cfg(all(test, target_os = "linux"))]
    #[test]
    fn gpio_line_gone_triggers_registry_evict() {
        // New test (no CM108 analog): rigctld-style lazy-retry-on-disconnect.
        // A `GpioError::LineGone` bubbling out of `set_line` must cause
        // `GpioPtt` to evict its `"chip:line"` entry from the registry's
        // `gpio_lines` map, so the next `build_driver` reopens cleanly.
        let mut registry = PortRegistry::new();
        let key = "/dev/fake-gpiochip:17";
        let (fake, _ops) = shared_fake_gpio_with_error(GpioError::LineGone {
            chip: "/dev/fake-gpiochip".into(),
            line: 17,
        });
        registry.install_gpio_for_test(key, fake.clone());

        // After install: test + registry each hold one strong Arc.
        assert_eq!(
            Arc::strong_count(&fake),
            2,
            "expected registry + test to own the fake before build_driver"
        );

        // build_driver performs an unkey()-on-construction that will
        // consume the armed LineGone error. Because the error fires
        // during construction, build_driver itself surfaces it — and
        // the GpioPtt that *would* have been returned has already run
        // its eviction hook via Drop (and via evict_from_cache called
        // from set()). Assert the map has been emptied.
        let build_result = registry.build_driver(&ConfigurePtt {
            method: "gpio".into(),
            device: "/dev/fake-gpiochip".into(),
            gpio_line: 17,
            ..base_cfg()
        });
        assert!(
            build_result.is_err(),
            "LineGone during construction unkey should surface as build error"
        );
        let err = build_result.unwrap_err();
        assert_eq!(
            err,
            "gpio line 17 on /dev/fake-gpiochip: device went away mid-operation",
            "error text must match GpioError::LineGone Display exactly",
        );

        // The registry must have evicted the entry so a subsequent
        // build_driver reopens cleanly.
        let map = registry
            .gpio_lines
            .lock()
            .expect("gpio_lines mutex poisoned");
        assert!(
            !map.contains_key(key),
            "registry must evict `{}` on LineGone; map still holds it",
            key
        );
        drop(map);

        // After eviction: only the test's Arc remains. (The partially
        // constructed GpioPtt dropped during build_driver's `?` unwind,
        // so its strong ref is gone.)
        assert_eq!(
            Arc::strong_count(&fake),
            1,
            "after eviction, only the test handle should reference the fake"
        );
    }

    #[cfg(all(test, target_os = "linux"))]
    #[test]
    fn gpio_chip_fd_released_when_last_line_dropped() {
        // Reinterpreted from the plan: the current registry only caches
        // per-line (no chip-fd refcount cache — see Phase 1 handoff §1
        // explaining why `gpiocdev::Request` prevents chip-fd sharing).
        // The surviving invariant worth exercising is that once a
        // `"chip:line"` entry is removed from `gpio_lines` AND every
        // driver pointed at it has been dropped, nothing in the registry
        // holds a strong Arc to the underlying `gpiocdev::Request` (here,
        // the fake standing in for it).
        //
        // This differs from `gpio_line_gone_triggers_registry_evict`:
        // that test drives eviction through an error path. This one
        // asserts the refcount story for the *success* path — after
        // a clean driver drop plus manual eviction, the fake's
        // strong_count must fall to 1 (the test alone).
        let mut registry = PortRegistry::new();
        let (fake, _ops) = shared_fake_gpio();
        let key = "/dev/fake-gpiochip:17";
        registry.install_gpio_for_test(key, fake.clone());

        let cfg = ConfigurePtt {
            method: "gpio".into(),
            device: "/dev/fake-gpiochip".into(),
            gpio_line: 17,
            ..base_cfg()
        };

        {
            let _driver = registry.build_driver(&cfg).unwrap();
            // With the driver alive: test + registry + driver = 3.
            assert_eq!(
                Arc::strong_count(&fake),
                3,
                "test + registry + driver should all hold the fake while driver is alive"
            );
        } // driver drops here — unkey()-on-Drop runs against the fake, then Arc released.

        // Driver dropped, but the registry still caches the line.
        assert_eq!(
            Arc::strong_count(&fake),
            2,
            "after driver drop, test + registry remain"
        );

        // Simulate the "last line evicted" case: remove the entry from
        // the registry's map the same way the LineGone path would.
        {
            let mut map = registry
                .gpio_lines
                .lock()
                .expect("gpio_lines mutex poisoned");
            map.remove(key);
        }

        // No one in the registry references the fake anymore.
        assert_eq!(
            Arc::strong_count(&fake),
            1,
            "after eviction, only the test handle should hold the fake"
        );
    }

    /// Instrumented [`PttDriver`] used by the modem-level tests (see
    /// `src/modem/tx_worker.rs`). Records every `key`/`unkey` call into
    /// a shared log so tests can assert the exact call order.
    #[derive(Clone, Default)]
    pub(crate) struct MockPtt {
        pub log: Arc<Mutex<Vec<PttCall>>>,
    }

    #[derive(Clone, Copy, Debug, PartialEq, Eq)]
    pub(crate) enum PttCall {
        Key,
        Unkey,
    }

    impl PttDriver for MockPtt {
        fn key(&mut self) -> Result<(), String> {
            self.log.lock().unwrap().push(PttCall::Key);
            Ok(())
        }

        fn unkey(&mut self) -> Result<(), String> {
            self.log.lock().unwrap().push(PttCall::Unkey);
            Ok(())
        }
    }

    #[test]
    fn mock_ptt_records_key_then_unkey_in_order() {
        let mock = MockPtt::default();
        let log = mock.log.clone();
        let mut driver: Box<dyn PttDriver> = Box::new(mock);
        driver.key().unwrap();
        driver.unkey().unwrap();
        driver.key().unwrap();
        assert_eq!(
            *log.lock().unwrap(),
            vec![PttCall::Key, PttCall::Unkey, PttCall::Key]
        );
    }

    // Per-platform smoke test: confirm the hardware adapter's `open()`
    // surfaces a descriptive `Err` for a path that doesn't exist rather
    // than panicking. The real PTT verification is the manual loopback
    // test; this just makes sure the FFI plumbing is hooked up.
    #[cfg(unix)]
    #[test]
    fn unix_serial_lines_open_rejects_nonexistent_path() {
        use super::ptt_unix::UnixSerialLines;
        let err = match UnixSerialLines::open("/dev/graywolf-ptt-definitely-not-real-xyz") {
            Err(e) => e,
            Ok(_) => panic!("must fail on missing device"),
        };
        assert!(
            err.contains("open") && err.to_lowercase().contains("no such"),
            "unexpected error: {}",
            err
        );
    }

    #[cfg(windows)]
    #[test]
    fn win_serial_lines_open_rejects_nonexistent_path() {
        use super::ptt_win::WinSerialLines;
        let err = match WinSerialLines::open("\\\\.\\COM_graywolf_ptt_bogus") {
            Err(e) => e,
            Ok(_) => panic!("must fail on missing device"),
        };
        assert!(err.contains("CreateFileW"), "unexpected error: {}", err);
    }

    // CM108 platform smoke tests: same pattern as serial — verify open()
    // returns a descriptive Err for a nonexistent path, not a panic.
    #[cfg(target_os = "linux")]
    #[test]
    fn unix_cm108_open_rejects_nonexistent_path() {
        use super::ptt_cm108_unix::UnixCm108Gpio;
        let err = match UnixCm108Gpio::open("/dev/graywolf-cm108-definitely-not-real-xyz") {
            Err(e) => e,
            Ok(_) => panic!("must fail on missing device"),
        };
        assert!(
            err.contains("open") && err.to_lowercase().contains("no such"),
            "unexpected error: {}",
            err
        );
    }

    // GPIO platform smoke test: same pattern as CM108 — open() a
    // definitely-missing chip path, confirm we get a descriptive
    // `GpioError` rather than a panic. The upstream `gpiocdev` crate
    // maps this to an ENOENT-backed `Error::Os` which our mapping in
    // `map_open_error` routes to `GpioError::Other("open …: no such
    // device …")`.
    #[cfg(target_os = "linux")]
    #[test]
    fn linux_gpiochip_open_rejects_nonexistent_path() {
        use super::ptt_gpio_linux::LinuxGpiochip;
        let err = match LinuxGpiochip::open("/dev/graywolf-gpio-definitely-not-real-xyz", 0) {
            Err(e) => e,
            Ok(_) => panic!("must fail on missing device"),
        };
        let msg = format!("{}", err);
        assert!(
            msg.to_lowercase().contains("no such"),
            "unexpected error: {}",
            msg
        );
    }

    #[cfg(target_os = "macos")]
    #[test]
    fn mac_cm108_open_rejects_nonexistent_path() {
        use super::ptt_cm108_macos::MacCm108Gpio;
        let err = match MacCm108Gpio::open("IOService:/nonexistent/graywolf-cm108-bogus") {
            Err(e) => e,
            Ok(_) => panic!("must fail on missing device"),
        };
        assert!(
            err.contains("hidapi"),
            "unexpected error: {}",
            err
        );
    }

    #[cfg(windows)]
    #[test]
    fn win_cm108_open_rejects_nonexistent_path() {
        use super::ptt_cm108_win::WinCm108Gpio;
        let err = match WinCm108Gpio::open("\\\\.\\HID#graywolf_cm108_bogus") {
            Err(e) => e,
            Ok(_) => panic!("must fail on missing device"),
        };
        assert!(err.contains("CreateFileW"), "unexpected error: {}", err);
    }
}
