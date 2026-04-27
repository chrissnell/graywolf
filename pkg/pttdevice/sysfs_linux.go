package pttdevice

import (
	"os"
	"path/filepath"
	"strings"
)

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
	if name := usbDeviceName(vendor, product); name != "" {
		description = name
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
