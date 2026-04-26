package flareschema

// USBTopology is the top-level shape emitted by
// `graywolf-modem --list-usb`. It walks every device the kernel exposes
// and records the minimum needed to diagnose:
//   - "is the radio interface plugged in" (vendor/product strings)
//   - "is it on a powered hub" (hub_power_source, max_power_ma)
//   - "is it negotiating a sane speed" (speed, usb_version)
//   - "what bus position" (port_path) so two identical devices can be
//     told apart between resubmits
type USBTopology struct {
	Devices []USBDevice      `json:"devices"`
	Issues  []CollectorIssue `json:"issues,omitempty"`
}

// USBDevice is one device entry in the topology. Vendor/product IDs are
// rendered as zero-padded 4-digit lowercase hex (e.g. "0d8c") to match
// the convention used by the rest of graywolf's USB-aware code (see
// existing graywolf-modem CM108Device.vendor at cm108.rs:52).
//
// Speed is one of "low", "full", "high", "super", "super_plus", or
// "unknown" — the nusb Speed enum collapsed to lower-snake-case.
//
// HubPowerSource is "bus", "self", or "unknown" — readable from the hub
// descriptor only on Linux today; macOS/Windows leave it as "unknown".
type USBDevice struct {
	BusNumber      int    `json:"bus_number"`
	PortPath       string `json:"port_path,omitempty"`
	VendorID       string `json:"vendor_id"`
	ProductID      string `json:"product_id"`
	VendorName     string `json:"vendor_name,omitempty"`
	ProductName    string `json:"product_name,omitempty"`
	Manufacturer   string `json:"manufacturer,omitempty"`
	Serial         string `json:"serial,omitempty"`
	Class          string `json:"class,omitempty"`
	Subclass       string `json:"subclass,omitempty"`
	USBVersion     string `json:"usb_version,omitempty"`
	Speed          string `json:"speed,omitempty"`
	MaxPowerMA     int    `json:"max_power_ma,omitempty"`
	HubPowerSource string `json:"hub_power_source,omitempty"`
}
