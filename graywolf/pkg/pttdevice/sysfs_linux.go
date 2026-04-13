package pttdevice

import (
	"os"
	"path/filepath"
	"strings"
)

// Known USB vendor:product → friendly name mappings for common ham radio devices.
// CM108-compatible entries must also be registered in cm108VendorSet or
// cm108VIDPIDSet (cm108_linux.go) for enumeration filtering.
//
// NOTE: this table is shared by both serial (usbInfoFromSysfs) and CM108
// (buildCM108Inventory) codepaths. Descriptions should make sense in both
// contexts; avoid adding non-CM108 devices under CM108 vendor IDs here.
var knownUSBDevices = map[string]string{
	"0d8c:000c": "CM108 USB Audio (GPIO PTT capable)",
	"0d8c:000e": "CM108 USB Audio (GPIO PTT capable)",
	"0d8c:0008": "CM108B USB Audio (GPIO PTT capable)",
	"0d8c:0012": "CM108AH USB Audio (GPIO PTT capable)",
	"0d8c:0014": "CM108AH USB Audio (GPIO PTT capable)",
	"0d8c:013c": "CM108 USB Audio (GPIO PTT capable)",
	"0d8c:0013": "CM119 USB Audio (GPIO PTT capable)",
	"0d8c:0139": "CM119A USB Audio (GPIO PTT capable)",
	"1209:7388": "AIOC All-In-One-Cable (CM108-compatible PTT)",
	"0c76:161f": "SSS USB Audio (CM108-compatible PTT)",
	"0c76:161e": "SSS USB Audio (CM108-compatible PTT)",
	"1a86:7523": "CH340 USB-Serial (Digirig, Mobilinkd, generic)",
	"0403:6001": "FTDI FT232R USB-Serial",
	"0403:6010": "FTDI FT2232 Dual USB-Serial",
	"0403:6014": "FTDI FT232H USB-Serial",
	"0403:6015": "FTDI FT-X USB-Serial",
	"067b:2303": "Prolific PL2303 USB-Serial",
	"10c4:ea60": "CP2102 USB-Serial (SignaLink, Digirig)",
	"10c4:ea70": "CP2105 Dual USB-Serial",
	"2341:0043": "Arduino Mega 2560",
	"2341:0001": "Arduino Uno",
	"1b4f:9206": "SparkFun Pro Micro",
}

// usbInfoFromSysfs reads USB vendor/product strings from sysfs for a /dev node.
// Returns vendor, product, description. All may be empty if not USB.
func usbInfoFromSysfs(devPath string) (vendor, product, description string) {
	base := filepath.Base(devPath)

	// Locate the sysfs class entry for this device.
	// /sys/class/tty/ttyUSB0 or /sys/class/hidraw/hidraw0
	var sysPath string
	for _, class := range []string{"tty", "hidraw"} {
		p := filepath.Join("/sys/class", class, base)
		if _, err := os.Stat(p); err == nil {
			sysPath = p
			break
		}
	}
	if sysPath == "" {
		return
	}

	dir := usbParentDir(sysPath)
	if dir == "" {
		return
	}

	vendor = readSysfsFile(filepath.Join(dir, "idVendor"))
	product = readSysfsFile(filepath.Join(dir, "idProduct"))

	// Try the USB product string first (most descriptive).
	description = readSysfsFile(filepath.Join(dir, "product"))

	// Check known device table for even better descriptions.
	key := vendor + ":" + product
	if known, ok := knownUSBDevices[key]; ok {
		description = known
	} else if description == "" {
		// Fallback to manufacturer + product ID.
		mfg := readSysfsFile(filepath.Join(dir, "manufacturer"))
		if mfg != "" {
			description = mfg
		}
	}
	return
}

// gpioChipDescription reads the label from /sys/class/gpio/gpiochipN/label.
func gpioChipDescription(devPath string) string {
	base := filepath.Base(devPath)
	label := readSysfsFile(filepath.Join("/sys/class/gpio", base, "label"))
	if label != "" {
		return label
	}
	// Try device-tree compatible for RPi/BeagleBone GPIO detection.
	compat := readSysfsFile(filepath.Join("/sys/class/gpio", base, "device/of_node/compatible"))
	if strings.Contains(compat, "brcm") || strings.Contains(compat, "broadcom") {
		return "Raspberry Pi GPIO"
	}
	if strings.Contains(compat, "omap") || strings.Contains(compat, "ti,") {
		return "BeagleBone GPIO"
	}
	return ""
}

// usbParentDir resolves a sysfs path and walks up to find the USB device
// ancestor (the directory containing idVendor). Returns the realpath of
// that directory, or "" if no USB ancestor is found.
func usbParentDir(path string) string {
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return ""
	}
	for i := 0; i < 10 && resolved != "/"; i++ {
		if _, err := os.Stat(filepath.Join(resolved, "idVendor")); err == nil {
			return resolved
		}
		resolved = filepath.Dir(resolved)
	}
	return ""
}

func readSysfsFile(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}
