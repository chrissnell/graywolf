// Package pttdevice enumerates serial ports, GPIO chips, and CM108 HID
// devices that can be used for push-to-talk control.
package pttdevice

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"go.bug.st/serial/enumerator"
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
			devs = append(devs, AvailableDevice{
				Path:        m,
				Type:        "serial",
				Name:        filepath.Base(m),
				Description: desc,
				USBVendor:   vendor,
				USBProduct:  product,
				Recommended: true,
			})
		}
	}
	return devs
}

// enumerateSerialWindows lists COM ports via go.bug.st/serial's enumerator,
// which exposes USB VID/PID and product strings on Windows. CM108 composite
// devices (Digirig, AIOC, generic C-Media) get their Description annotated so
// users can distinguish the RTS/DTR serial PTT interface from the CM108 HID
// interface that's enumerated separately by enumerateCM108().
func enumerateSerialWindows() []AvailableDevice {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		slog.Warn("pttdevice: COM port enumeration failed", "err", err)
		return []AvailableDevice{{
			Type:    "serial",
			Path:    "",
			Warning: fmt.Sprintf("COM port enumeration failed: %v", err),
		}}
	}
	devs := make([]AvailableDevice, 0, len(ports))
	for _, port := range ports {
		if port == nil {
			continue
		}
		desc := port.Product
		if desc == "" {
			if port.IsUSB {
				desc = fmt.Sprintf("%s (USB %s:%s)", port.Name, port.VID, port.PID)
			} else {
				desc = port.Name
			}
		}
		dev := AvailableDevice{
			Path:        port.Name,
			Type:        "serial",
			Name:        port.Name,
			Description: desc,
			Recommended: true,
		}
		if port.IsUSB {
			dev.USBVendor = port.VID
			dev.USBProduct = port.PID
		}
		devs = append(devs, dev)
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
