//! CM108 HID device enumeration via hidapi.
//!
//! Used by the `--list-cm108` subcommand to enumerate CM108-compatible USB
//! HID devices. The Go parent shells out to this on macOS and Windows
//! where sysfs is unavailable. Output is a JSON array consumed by the Go
//! app's `cm108ModemInventory()`.

use hidapi::HidApi;
use serde::Serialize;

#[derive(Serialize)]
struct Cm108Device {
    path: String,
    vendor: String,
    product: String,
    description: String,
}

/// CM108-compatible vendor IDs (matched by vendor alone).
const CM108_VENDORS: &[u16] = &[
    0x0d8c, // C-Media (CM108, CM108B, CM108AH, CM109, CM119, CM119A)
    0x0c76, // SSS
];

/// CM108-compatible devices matched by full VID:PID.
const CM108_VIDPID: &[(u16, u16)] = &[
    (0x1209, 0x7388), // AIOC All-In-One-Cable
];

/// Enumerate all CM108-compatible USB HID devices and return their paths,
/// vendor/product IDs, and descriptions as a JSON array string.
pub fn enumerate_cm108() -> Result<String, String> {
    let api = HidApi::new().map_err(|e| format!("hidapi init: {}", e))?;
    let mut devices = Vec::new();
    for dev in api.device_list() {
        let vid = dev.vendor_id();
        let pid = dev.product_id();
        if !CM108_VENDORS.contains(&vid) && !CM108_VIDPID.contains(&(vid, pid)) {
            continue;
        }
        // CM108 GPIO is on USB interface 3. Skip other interfaces on
        // composite devices to avoid listing audio-control HID endpoints
        // that don't accept GPIO output reports. -1 means hidapi couldn't
        // determine the interface (common on macOS) — include it so we
        // don't silently hide the only device the user has.
        let iface = dev.interface_number();
        if iface != -1 && iface != 3 {
            continue;
        }
        devices.push(Cm108Device {
            path: dev.path().to_string_lossy().into_owned(),
            vendor: format!("{:04x}", vid),
            product: format!("{:04x}", pid),
            description: dev.product_string().unwrap_or_default().to_string(),
        });
    }
    serde_json::to_string(&devices).map_err(|e| format!("json: {}", e))
}
