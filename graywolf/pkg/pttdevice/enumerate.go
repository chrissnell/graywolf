// Package pttdevice enumerates serial ports, GPIO chips, and CM108 HID
// devices that can be used for push-to-talk control.
package pttdevice

import (
	"os"
	"path/filepath"
	"runtime"
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
}

// Enumerate returns all detected PTT-capable devices on the host.
func Enumerate() []AvailableDevice {
	var devs []AvailableDevice
	devs = append(devs, enumerateSerial()...)
	devs = append(devs, enumerateGPIO()...)
	devs = append(devs, enumerateCM108()...)
	return devs
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
			"/dev/tty.usbserial-*",
			"/dev/tty.usbmodem*",
			"/dev/cu.usbserial-*",
			"/dev/cu.usbmodem*",
		}
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
			devs = append(devs, AvailableDevice{
				Path:        m,
				Type:        "serial",
				Name:        filepath.Base(m),
				Description: desc,
				USBVendor:   vendor,
				USBProduct:  product,
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

func enumerateCM108() []AvailableDevice {
	if runtime.GOOS != "linux" {
		return nil
	}
	var devs []AvailableDevice
	// CM108-compatible USB audio adapters expose HID interfaces under /dev/hidraw*
	matches, _ := filepath.Glob("/dev/hidraw*")
	for _, m := range matches {
		vendor, product, desc := usbInfoFromSysfs(m)
		devs = append(devs, AvailableDevice{
			Path:        m,
			Type:        "cm108",
			Name:        filepath.Base(m),
			Description: desc,
			USBVendor:   vendor,
			USBProduct:  product,
		})
	}
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
