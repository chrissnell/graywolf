//! `--list-usb` subcommand: emit a USB device tree as JSON matching
//! `graywolf/pkg/flareschema.USBTopology`. Used by the future
//! `graywolf flare` CLI to capture USB topology from the same crate the
//! modem uses at runtime so audio/HID/USB enumeration cannot disagree.

use serde::Serialize;

#[derive(Serialize)]
struct USBTopology {
    devices: Vec<USBDevice>,
    #[serde(skip_serializing_if = "Vec::is_empty")]
    issues: Vec<CollectorIssue>,
}

#[derive(Serialize)]
struct USBDevice {
    bus_number: u32,
    #[serde(skip_serializing_if = "String::is_empty")]
    port_path: String,
    vendor_id: String,
    product_id: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    vendor_name: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    product_name: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    manufacturer: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    serial: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    class: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    subclass: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    usb_version: String,
    #[serde(skip_serializing_if = "String::is_empty")]
    speed: String,
    #[serde(skip_serializing_if = "is_zero")]
    max_power_ma: u32,
    #[serde(skip_serializing_if = "String::is_empty")]
    hub_power_source: String,
}

#[derive(Serialize)]
struct CollectorIssue {
    kind: String,
    message: String,
    #[serde(skip_serializing_if = "Option::is_none")]
    path: Option<String>,
}

fn is_zero(v: &u32) -> bool {
    *v == 0
}

/// Run the enumeration. Always emits a parseable JSON document — every
/// failure is recorded as an issue rather than thrown out of the binary,
/// so the Go-side collector sees a structured response in every case.
pub fn run() -> String {
    let mut topology = USBTopology {
        devices: Vec::new(),
        issues: Vec::new(),
    };

    let iter = match nusb::list_devices() {
        Ok(it) => it,
        Err(e) => {
            topology.issues.push(CollectorIssue {
                kind: "list_devices_failed".into(),
                message: format!("{}", e),
                path: None,
            });
            return marshal(&topology);
        }
    };

    for d in iter {
        // nusb 0.1.14 does not expose a portable port-chain accessor.
        // The platform-specific accessors are:
        //   * Linux/Android: `sysfs_path()` (full chain in the path)
        //   * macOS: `location_id()` (encodes the hub chain in nibbles)
        //   * Windows: `port_number()` (single u32)
        // For now, fall back to the portable `device_address()` so the
        // field is populated on every platform; the JSON tag stays
        // `port_path` so the Go-side wire contract (USBDevice.PortPath)
        // is unchanged. A richer multi-segment chain is a follow-up
        // that walks the parent device tree per platform.
        let port_path = port_path_for(&d);

        topology.devices.push(USBDevice {
            bus_number: d.bus_number() as u32,
            port_path,
            vendor_id: format!("{:04x}", d.vendor_id()),
            product_id: format!("{:04x}", d.product_id()),
            vendor_name: String::new(), // not exposed by nusb without descriptor read
            product_name: d.product_string().unwrap_or_default().to_string(),
            manufacturer: d.manufacturer_string().unwrap_or_default().to_string(),
            serial: d.serial_number().unwrap_or_default().to_string(),
            class: format!("{:02x}", d.class()),
            subclass: format!("{:02x}", d.subclass()),
            // nusb 0.1.14 renamed `usb_version()` to `device_version()`;
            // the JSON field stays `usb_version` (Go-side wire contract).
            usb_version: format_bcd(d.device_version()),
            speed: format_speed(d.speed()),
            // bMaxPower / hub power source require a descriptor read
            // which on some platforms requires open(); leave them unset
            // when nusb's DeviceInfo doesn't surface them. Reading from
            // sysfs on Linux or IOKit on macOS is a follow-up.
            max_power_ma: 0,
            hub_power_source: String::new(),
        });
    }

    marshal(&topology)
}

fn marshal(t: &USBTopology) -> String {
    serde_json::to_string(t).unwrap_or_else(|e| {
        format!(
            "{{\"devices\":[],\"issues\":[{{\"kind\":\"json_marshal_failed\",\"message\":{:?}}}]}}",
            e.to_string()
        )
    })
}

fn format_bcd(bcd: u16) -> String {
    let major = (bcd >> 8) & 0xff;
    let minor = bcd & 0xff;
    format!("{:x}.{:02x}", major, minor)
}

/// Render a stable per-device locator string for the `port_path` JSON
/// field. nusb 0.1.x's portable `DeviceInfo` only exposes
/// `device_address()` cross-platform; richer hub chains live behind
/// platform-specific accessors and are a follow-up.
fn port_path_for(d: &nusb::DeviceInfo) -> String {
    #[cfg(target_os = "macos")]
    {
        // macOS location IDs encode the full hub chain in 4-bit
        // nibbles (e.g. 0x14310000 → root 1, hub 4, port 3, port 1).
        // Render as zero-padded 8-digit hex to match Apple's
        // System Information display.
        return format!("{:08x}", d.location_id());
    }
    #[cfg(target_os = "windows")]
    {
        return d.port_number().to_string();
    }
    #[cfg(not(any(target_os = "macos", target_os = "windows")))]
    {
        // Linux + everything else: the device address is unique per
        // bus and the simplest portable identifier nusb exposes.
        return d.device_address().to_string();
    }
}

fn format_speed(s: Option<nusb::Speed>) -> String {
    match s {
        Some(nusb::Speed::Low) => "low".into(),
        Some(nusb::Speed::Full) => "full".into(),
        Some(nusb::Speed::High) => "high".into(),
        Some(nusb::Speed::Super) => "super".into(),
        Some(nusb::Speed::SuperPlus) => "super_plus".into(),
        None => "unknown".into(),
        // Future nusb versions may add variants; collapse them to
        // "unknown" rather than a debug-formatted string so the
        // operator UI's drop-down list stays curated.
        Some(_) => "unknown".into(),
    }
}
