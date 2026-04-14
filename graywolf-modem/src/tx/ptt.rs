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

#[cfg(unix)]
use ptt_unix::UnixSerialLines as PlatformSerialLines;
#[cfg(windows)]
use ptt_win::WinSerialLines as PlatformSerialLines;

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
}

impl PortRegistry {
    /// Build an empty registry. The modem owns one for the lifetime of
    /// the process; dropping it closes every cached port.
    pub(crate) fn new() -> Self {
        Self {
            ports: HashMap::new(),
        }
    }

    /// Build a [`PttDriver`] for the given channel configuration. May
    /// open a new serial port as a side effect (and cache it for reuse
    /// on subsequent calls with the same `device`). Returns an error
    /// for unknown method strings and for `cm108` / `gpio` until those
    /// drivers are implemented — the caller logs the error and leaves
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
            PttMethod::Cm108 => Err("cm108 ptt not yet implemented".into()),
            PttMethod::Gpio => Err("gpio ptt not yet implemented".into()),
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

    /// Test hook: pre-install a fake port so tests can verify fd-sharing
    /// semantics without touching real hardware.
    #[cfg(test)]
    fn install_for_test(&mut self, device: &str, port: SharedLines) {
        self.ports.insert(device.to_string(), port);
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

    #[test]
    fn build_driver_returns_unimplemented_errors_for_cm108_and_gpio() {
        let mut registry = PortRegistry::new();
        let mut cm = base_cfg();
        cm.method = "cm108".into();
        cm.device = "/dev/null".into();
        let err = expect_err(registry.build_driver(&cm));
        assert!(err.contains("cm108"), "unexpected error: {}", err);

        let mut gpio = base_cfg();
        gpio.method = "gpio".into();
        gpio.device = "/dev/null".into();
        let err = expect_err(registry.build_driver(&gpio));
        assert!(err.contains("gpio"), "unexpected error: {}", err);
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
