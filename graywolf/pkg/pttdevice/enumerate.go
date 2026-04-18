// Package pttdevice enumerates serial ports, GPIO chips, and CM108 HID
// devices that can be used for push-to-talk control.
package pttdevice

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// AvailableDevice describes a detected PTT-capable device.
type AvailableDevice struct {
	Path        string `json:"path"`
	Type        string `json:"type"`        // serial, gpio, cm108
	Name        string `json:"name"`
	Description string `json:"description"` // human-friendly label (USB product, GPIO chip)
	USBVendor   string `json:"usb_vendor,omitempty"`
	USBProduct  string `json:"usb_product,omitempty"`
	// Recommended is true for the device path users should prefer. On macOS
	// we recommend /dev/cu.* over /dev/tty.* (which blocks until DCD).
	Recommended bool `json:"recommended"`
	// Warning is set when there's a known gotcha with this path.
	Warning string `json:"warning,omitempty"`
}

// Enumerate returns all detected PTT-capable devices on the host.
func Enumerate() []AvailableDevice {
	var devs []AvailableDevice
	devs = append(devs, enumerateSerial()...)
	devs = append(devs, enumerateGPIO()...)
	devs = append(devs, enumerateCM108()...)
	return annotateAndSort(devs)
}

func enumerateSerial() []AvailableDevice {
	var devs []AvailableDevice
	var patterns []string

	switch runtime.GOOS {
	case "linux":
		patterns = []string{
			"/dev/ttyUSB*",
			"/dev/ttyACM*",
			"/dev/ttyS*",
			"/dev/ttyAMA*",
		}
	case "darwin":
		patterns = []string{
			"/dev/cu.usbserial-*",
			"/dev/cu.usbmodem*",
			"/dev/tty.usbserial-*",
			"/dev/tty.usbmodem*",
		}
	case "windows":
		return enumerateSerialWindows()
	}

	seen := map[string]bool{}
	for _, pat := range patterns {
		matches, _ := filepath.Glob(pat)
		for _, m := range matches {
			if seen[m] {
				continue
			}
			seen[m] = true
			// Skip /dev/ttyS* ports that aren't real hardware (no open permission or no driver)
			if strings.HasPrefix(m, "/dev/ttyS") {
				if !isAccessible(m) {
					continue
				}
			}
			vendor, product, desc := usbInfoFromSysfs(m)
			// On Linux we read USB VID:PID from sysfs, so only recommend
			// serial ports whose chipset we recognize as a likely ham PTT
			// interface. Unknown USB devices (e.g., uBlox GPS on ttyACMx)
			// and bare platform UARTs (ttyS*, ttyAMA*) are listed but not
			// highlighted. On other platforms VID:PID is not populated
			// from sysfs, so fall back to recommending by default.
			recommended := true
			if runtime.GOOS == "linux" {
				recommended = lookupUSB(vendor, product).LikelyPTT
			}
			devs = append(devs, AvailableDevice{
				Path:        m,
				Type:        "serial",
				Name:        filepath.Base(m),
				Description: desc,
				USBVendor:   vendor,
				USBProduct:  product,
				Recommended: recommended,
			})
		}
	}
	return devs
}

func enumerateGPIO() []AvailableDevice {
	if runtime.GOOS != "linux" {
		return nil
	}
	var devs []AvailableDevice
	matches, _ := filepath.Glob("/dev/gpiochip*")
	for _, m := range matches {
		devs = append(devs, AvailableDevice{
			Path:        m,
			Type:        "gpio",
			Name:        filepath.Base(m),
			Description: gpioChipDescription(m),
		})
	}
	return devs
}

// annotateAndSort marks macOS tty.* serial devices as not recommended and
// sorts the list so recommended devices appear first.
func annotateAndSort(devs []AvailableDevice) []AvailableDevice {
	for i := range devs {
		if runtime.GOOS == "darwin" && devs[i].Type == "serial" && strings.HasPrefix(devs[i].Path, "/dev/tty.") {
			devs[i].Recommended = false
			devs[i].Warning = "macOS tty.* device blocks until DCD is asserted; use the matching cu.* device instead"
		}
		// Demote the CDC-ACM serial side of composite CM108-compatible
		// adapters (e.g., AIOC): the canonical PTT path is the CM108 HID
		// entry, not this serial port. Firmware RTS→GPIO mapping may
		// still work, but users should prefer the HID.
		if devs[i].Type == "serial" && devs[i].USBVendor != "" &&
			isCM108Compatible(devs[i].USBVendor, devs[i].USBProduct) {
			devs[i].Recommended = false
			if devs[i].Warning == "" {
				devs[i].Warning = "Use the CM108 HID entry for PTT on this adapter; this serial port is for data"
			}
		}
	}
	sort.SliceStable(devs, func(i, j int) bool {
		if devs[i].Recommended != devs[j].Recommended {
			return devs[i].Recommended
		}
		return devs[i].Path < devs[j].Path
	})
	return devs
}

func isAccessible(path string) bool {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}
