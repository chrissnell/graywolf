package pttdevice

import (
	"fmt"
	"log/slog"

	"go.bug.st/serial/enumerator"
)

// enumerateSerialWindows lists COM ports via go.bug.st/serial's enumerator,
// which exposes USB VID/PID and product strings on Windows.
//
// The enumerator subpackage requires CGO on linux/darwin but not on Windows,
// so this file is windows-only to keep non-windows builds CGO-free.
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
			// Recommend only COM ports whose USB chipset we recognize as
			// a likely ham PTT interface. Unknown/non-USB ports are
			// listed but not highlighted. Matches the Linux policy.
			Recommended: port.IsUSB && lookupUSB(port.VID, port.PID).LikelyPTT,
		}
		if port.IsUSB {
			dev.USBVendor = port.VID
			dev.USBProduct = port.PID
		}
		devs = append(devs, dev)
	}
	return devs
}
