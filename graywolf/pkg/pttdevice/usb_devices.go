package pttdevice

import "strings"

// usbDevice describes a known USB device in the PTT ecosystem.
type usbDevice struct {
	// VID and PID are lowercase hex, no "0x" prefix. An empty PID matches
	// any product from the given vendor (vendor-only fallback).
	VID, PID string
	// Name is the human-friendly description shown in Detect Devices.
	Name string
	// HasCM108 is true if this VID:PID has CM108-compatible HID GPIO and
	// can be driven by the CM108 PTT path.
	HasCM108 bool
	// LikelyPTT is true for USB-serial chipsets commonly seen on ham
	// radio PTT interfaces (CH340, CP210x, FTDI FT232R, PL2303).
	// Detection marks these "Recommended"; all other serial devices
	// (GPS receivers, dev boards, bare platform UARTs) are listed but
	// not recommended so users aren't steered at the wrong port. For
	// CM108-family adapters the HID is the canonical path, so their
	// serial side stays LikelyPTT=false.
	LikelyPTT bool
}

// knownUSBDevices is the single source of truth for USB VID:PID metadata
// across the Go side of Graywolf. Windows enumeration, Linux CM108 HID
// filtering, and Linux sysfs descriptions all consult this table.
//
// lookupUSB prefers a specific-PID match over a vendor-only fallback
// regardless of position in the slice, so entries can be ordered for
// readability.
var knownUSBDevices = []usbDevice{
	// C-Media CM108 family. The 0d8c vendor only ships CM108-family audio
	// chips with compatible HID GPIO, so the vendor-only fallback is safe:
	// any unlisted 0d8c product is still treated as CM108.
	{VID: "0d8c", PID: "000c", Name: "CM108 USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "000e", Name: "CM108 USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "0008", Name: "CM108B USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "0012", Name: "CM108AH USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "0014", Name: "CM108AH USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "013c", Name: "CM108 USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "0013", Name: "CM119 USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "013a", Name: "CM119 USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "0139", Name: "CM119A USB Audio (GPIO PTT capable)", HasCM108: true},
	{VID: "0d8c", PID: "", Name: "C-Media CM108-family USB Audio", HasCM108: true},

	// SSS — small vendor with CM108-compatible audio chips.
	{VID: "0c76", PID: "", Name: "SSS USB Audio (CM108-compatible PTT)", HasCM108: true},

	// AIOC — All-In-One-Cable under the pid.codes VID range, with
	// CM108-compatible HID.
	{VID: "1209", PID: "7388", Name: "AIOC All-In-One-Cable (CM108-compatible PTT)", HasCM108: true},

	// USB-serial chipsets commonly found on ham PTT interfaces. These get
	// the Recommended badge when they show up as /dev/ttyUSB* or a COM port.
	{VID: "1a86", PID: "7523", Name: "CH340 USB-Serial (Digirig, Mobilinkd, generic)", LikelyPTT: true},
	{VID: "0403", PID: "6001", Name: "FTDI FT232R USB-Serial", LikelyPTT: true},
	{VID: "0403", PID: "6010", Name: "FTDI FT2232 Dual USB-Serial", LikelyPTT: true},
	{VID: "0403", PID: "6014", Name: "FTDI FT232H USB-Serial", LikelyPTT: true},
	{VID: "0403", PID: "6015", Name: "FTDI FT-X USB-Serial", LikelyPTT: true},
	{VID: "067b", PID: "2303", Name: "Prolific PL2303 USB-Serial", LikelyPTT: true},
	{VID: "10c4", PID: "ea60", Name: "CP2102 USB-Serial (SignaLink, Digirig)", LikelyPTT: true},
	{VID: "10c4", PID: "ea70", Name: "CP2105 Dual USB-Serial", LikelyPTT: true},

	// Known-but-not-PTT: dev boards. Named so Detect Devices shows what
	// they are, but not recommended — they aren't ham PTT interfaces.
	{VID: "2341", PID: "0043", Name: "Arduino Mega 2560"},
	{VID: "2341", PID: "0001", Name: "Arduino Uno"},
	{VID: "1b4f", PID: "9206", Name: "SparkFun Pro Micro"},
}

// lookupUSB returns the knownUSBDevices entry for a VID:PID, preferring a
// specific PID match, then a vendor-only fallback, then the zero value.
// VID and PID are matched case-insensitively.
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

// isCM108Compatible reports whether a device with the given VID:PID has
// CM108-compatible HID GPIO and can be driven by the CM108 PTT path.
func isCM108Compatible(vid, pid string) bool {
	return lookupUSB(vid, pid).HasCM108
}

// usbDeviceName returns a human-friendly name for a known USB device, or
// empty string if the VID:PID isn't in the table.
func usbDeviceName(vid, pid string) string {
	return lookupUSB(vid, pid).Name
}
