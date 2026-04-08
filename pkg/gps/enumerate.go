package gps

import (
	"runtime"
	"sort"
	"strings"

	"go.bug.st/serial/enumerator"
)

// SerialPortInfo describes one detected serial port for the web UI.
// Fields mirror the JSON shape returned by GET /api/gps/available.
type SerialPortInfo struct {
	Path         string `json:"path"`         // device path, e.g. /dev/cu.usbserial-110
	Name         string `json:"name"`         // basename of path
	Description  string `json:"description"`  // human-readable description if available
	IsUSB        bool   `json:"is_usb"`
	VID          string `json:"vid,omitempty"`
	PID          string `json:"pid,omitempty"`
	SerialNumber string `json:"serial_number,omitempty"`
	Product      string `json:"product,omitempty"`
	// Recommended is true for the device path users should pick. On macOS
	// we recommend the /dev/cu.* callout device over /dev/tty.* (which
	// blocks until DCD is asserted).
	Recommended bool `json:"recommended"`
	// Warning is set when there's a known gotcha with this path (e.g. the
	// macOS tty.* / cu.* distinction).
	Warning string `json:"warning,omitempty"`
}

// EnumerateSerialPorts returns the list of serial ports visible to the OS,
// sorted with USB devices first and recommended paths last within each group
// so the most likely candidate appears at the top of the UI.
func EnumerateSerialPorts() ([]SerialPortInfo, error) {
	ports, err := enumerator.GetDetailedPortsList()
	if err != nil {
		return nil, err
	}
	out := make([]SerialPortInfo, 0, len(ports))
	for _, p := range ports {
		if p == nil || p.Name == "" {
			continue
		}
		info := SerialPortInfo{
			Path:         p.Name,
			Name:         baseName(p.Name),
			IsUSB:        p.IsUSB,
			VID:          p.VID,
			PID:          p.PID,
			SerialNumber: p.SerialNumber,
			Product:      p.Product,
			Recommended:  true,
		}
		// Build a friendly description.
		switch {
		case p.Product != "":
			info.Description = p.Product
		case p.IsUSB && p.VID != "" && p.PID != "":
			info.Description = "USB " + p.VID + ":" + p.PID
		default:
			info.Description = info.Name
		}
		// macOS-specific: warn about /dev/tty.* and prefer /dev/cu.*.
		if runtime.GOOS == "darwin" && strings.HasPrefix(p.Name, "/dev/tty.") {
			info.Recommended = false
			info.Warning = "macOS tty.* device blocks until DCD is asserted; use the matching cu.* device instead"
		}
		out = append(out, info)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// USB first, then recommended, then by path.
		if out[i].IsUSB != out[j].IsUSB {
			return out[i].IsUSB
		}
		if out[i].Recommended != out[j].Recommended {
			return out[i].Recommended
		}
		return out[i].Path < out[j].Path
	})
	return out, nil
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
