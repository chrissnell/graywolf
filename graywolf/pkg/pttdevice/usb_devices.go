package pttdevice

import "strings"

// usbDevice describes a known USB device in the PTT ecosystem. Fields drive
// three separate decisions in the codebase: friendly-name display, CM108 HID
// filter matching, and CM108-composite-sibling annotation.
type usbDevice struct {
	// VID and PID are lowercase hex, no "0x" prefix. An empty PID matches
	// any product from the given vendor (vendor-only fallback).
	VID, PID string
	// Name is the human-friendly description shown in Detect Devices.
	Name string
	// HasCM108 is true if this VID:PID has CM108-compatible HID GPIO and
	// can be driven by the CM108 PTT path.
	HasCM108 bool
	// Composite is true if this VID:PID belongs to a CM108 composite adapter
	// (a single physical product exposing both CM108 HID and another USB
	// interface such as a serial port). Used to annotate the sibling serial
	// port as "(RTS/DTR PTT)" so users can distinguish it from the CM108 HID
	// entry enumerated separately.
	Composite bool
}

// knownUSBDevices is the single source of truth for USB VID:PID metadata
// across the Go side of Graywolf. Windows serial annotation, Linux CM108 HID
// filtering, and Linux sysfs descriptions all consult this table.
//
// Ordering matters: specific PID entries must appear before vendor-only
// fallbacks so lookupUSB prefers the more specific match.
//
// When adding a new adapter, set HasCM108/Composite to describe its actual
// hardware role — don't conflate the two.
var knownUSBDevices = []usbDevice{
	// C-Media CM108 family. The 0d8c vendor only ships CM108-family audio
	// chips with compatible HID GPIO, so the vendor-only fallback at the end
	// is safe: any unlisted 0d8c product is still treated as CM108.
	{VID: "0d8c", PID: "000c", Name: "CM108 USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "000e", Name: "CM108 USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "0008", Name: "CM108B USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "0012", Name: "CM108AH USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "0014", Name: "CM108AH USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "013c", Name: "CM108 USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "0013", Name: "CM119 USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "013a", Name: "CM119 USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "0139", Name: "CM119A USB Audio (GPIO PTT capable)", HasCM108: true, Composite: true},
	{VID: "0d8c", PID: "", Name: "C-Media CM108-family USB Audio", HasCM108: true, Composite: true},

	// SSS — small vendor with CM108-compatible audio chips.
	{VID: "0c76", PID: "161f", Name: "SSS USB Audio (CM108-compatible PTT)", HasCM108: true, Composite: true},
	{VID: "0c76", PID: "161e", Name: "SSS USB Audio (CM108-compatible PTT)", HasCM108: true, Composite: true},
	{VID: "0c76", PID: "", Name: "SSS USB Audio (CM108-compatible)", HasCM108: true, Composite: true},

	// AIOC — All-In-One-Cable under the pid.codes VID range, with CM108-
	// compatible HID.
	{VID: "1209", PID: "7388", Name: "AIOC All-In-One-Cable (CM108-compatible PTT)", HasCM108: true, Composite: true},

	// CH340 — generic USB-to-serial chip. Not itself a CM108 device, but
	// Digirig pairs a CH340 serial interface alongside its C-Media CM108
	// audio/HID, so Composite=true. False positives on plain CH340 cables
	// receive the "(RTS/DTR PTT)" label, which is still technically accurate
	// for any serial-PTT cable.
	{VID: "1a86", PID: "7523", Name: "CH340 USB-Serial (Digirig, Mobilinkd, generic)", HasCM108: false, Composite: true},

	// Generic USB-serial chips and hobby boards. Display names only — not
	// CM108, not composite siblings of CM108.
	{VID: "0403", PID: "6001", Name: "FTDI FT232R USB-Serial"},
	{VID: "0403", PID: "6010", Name: "FTDI FT2232 Dual USB-Serial"},
	{VID: "0403", PID: "6014", Name: "FTDI FT232H USB-Serial"},
	{VID: "0403", PID: "6015", Name: "FTDI FT-X USB-Serial"},
	{VID: "067b", PID: "2303", Name: "Prolific PL2303 USB-Serial"},
	{VID: "10c4", PID: "ea60", Name: "CP2102 USB-Serial (SignaLink, Digirig)"},
	{VID: "10c4", PID: "ea70", Name: "CP2105 Dual USB-Serial"},
	{VID: "2341", PID: "0043", Name: "Arduino Mega 2560"},
	{VID: "2341", PID: "0001", Name: "Arduino Uno"},
	{VID: "1b4f", PID: "9206", Name: "SparkFun Pro Micro"},
}

// lookupUSB returns the knownUSBDevices entry for a VID:PID, preferring a
// specific PID match, then a vendor-only fallback, then the zero value.
func lookupUSB(vid, pid string) usbDevice {
	vid = strings.ToLower(vid)
	pid = strings.ToLower(pid)
	var fallback usbDevice
	var haveFallback bool
	for _, d := range knownUSBDevices {
		if d.VID != vid {
			continue
		}
		if d.PID == pid {
			return d
		}
		if d.PID == "" {
			fallback = d
			haveFallback = true
		}
	}
	if haveFallback {
		return fallback
	}
	return usbDevice{}
}

// IsCM108Compatible reports whether a device with the given VID:PID has
// CM108-compatible HID GPIO and can be driven by the CM108 PTT path.
func IsCM108Compatible(vid, pid string) bool {
	return lookupUSB(vid, pid).HasCM108
}

// IsCM108Composite reports whether the given VID:PID belongs to a CM108
// composite adapter. Callers use this to annotate sibling serial ports so
// users can distinguish them from the CM108 HID entry.
func IsCM108Composite(vid, pid string) bool {
	return lookupUSB(vid, pid).Composite
}

// USBDeviceName returns a human-friendly name for a known USB device, or
// empty string if the VID:PID isn't in the table.
func USBDeviceName(vid, pid string) string {
	return lookupUSB(vid, pid).Name
}
