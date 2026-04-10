package gps

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"go.bug.st/serial"
)

// SerialPortInfo describes one detected serial port for the web UI.
// Fields mirror the JSON shape returned by GET /api/gps/available.
type SerialPortInfo struct {
	Path         string `json:"path"`         // device path, e.g. /dev/cu.usbserial-110
	Name         string `json:"name"`         // basename of path
	Description  string `json:"description"`  // human-readable description
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

// EnumerateSerialPorts returns the list of serial ports visible to the OS.
// Implementation is pure Go (no cgo) so it cross-compiles cleanly. On Linux
// we read /sys/class/tty/*/device to enrich USB devices with VID/PID/product
// strings; other platforms get the path-based heuristics only.
func EnumerateSerialPorts() ([]SerialPortInfo, error) {
	paths, err := serial.GetPortsList()
	if err != nil {
		return nil, err
	}
	out := make([]SerialPortInfo, 0, len(paths))
	for _, p := range paths {
		info := SerialPortInfo{
			Path:        p,
			Name:        baseName(p),
			Recommended: true,
		}
		info.IsUSB = looksLikeUSB(p)
		enrichLinuxUSB(&info)
		if info.Description == "" {
			switch {
			case info.Product != "":
				info.Description = info.Product
			case info.IsUSB && info.VID != "" && info.PID != "":
				info.Description = "USB " + info.VID + ":" + info.PID
			case info.IsUSB:
				info.Description = "USB serial device"
			default:
				info.Description = info.Name
			}
		}
		out = append(out, info)
	}
	return annotateAndSort(out), nil
}

// looksLikeUSB applies path-prefix heuristics to flag USB serial devices on
// Linux and macOS. Hits common adapters: CP210x (ttyUSB), CDC ACM (ttyACM),
// Apple cu.usbserial / cu.usbmodem.
func looksLikeUSB(path string) bool {
	base := baseName(path)
	switch {
	case strings.HasPrefix(base, "ttyUSB"),
		strings.HasPrefix(base, "ttyACM"),
		strings.HasPrefix(base, "cu.usbserial"),
		strings.HasPrefix(base, "cu.usbmodem"),
		strings.HasPrefix(base, "tty.usbserial"),
		strings.HasPrefix(base, "tty.usbmodem"):
		return true
	}
	return false
}

// enrichLinuxUSB walks /sys/class/tty/<name>/device to populate VID/PID and
// product strings on Linux. Quietly does nothing on other platforms or when
// the device isn't backed by a USB interface.
func enrichLinuxUSB(info *SerialPortInfo) {
	if runtime.GOOS != "linux" || !info.IsUSB {
		return
	}
	// /sys/class/tty/ttyUSB0/device is a symlink whose ancestors hold the
	// USB interface and parent device. Walk up looking for idVendor.
	dev, err := filepath.EvalSymlinks("/sys/class/tty/" + info.Name + "/device")
	if err != nil {
		return
	}
	for i := 0; i < 6; i++ { // bounded walk up the tree
		if vid, err := os.ReadFile(filepath.Join(dev, "idVendor")); err == nil {
			info.VID = strings.ToUpper(strings.TrimSpace(string(vid)))
			if pid, err := os.ReadFile(filepath.Join(dev, "idProduct")); err == nil {
				info.PID = strings.ToUpper(strings.TrimSpace(string(pid)))
			}
			if prod, err := os.ReadFile(filepath.Join(dev, "product")); err == nil {
				info.Product = strings.TrimSpace(string(prod))
				info.Description = info.Product
			}
			if sn, err := os.ReadFile(filepath.Join(dev, "serial")); err == nil {
				info.SerialNumber = strings.TrimSpace(string(sn))
			}
			return
		}
		parent := filepath.Dir(dev)
		if parent == dev || parent == "/" {
			return
		}
		dev = parent
	}
}

// annotateAndSort applies macOS warnings and sorts ports so the most likely
// candidate appears at the top of the UI (USB first, recommended next).
func annotateAndSort(ports []SerialPortInfo) []SerialPortInfo {
	for i := range ports {
		if runtime.GOOS == "darwin" && strings.HasPrefix(ports[i].Path, "/dev/tty.") {
			ports[i].Recommended = false
			ports[i].Warning = "macOS tty.* device blocks until DCD is asserted; use the matching cu.* device instead"
		}
	}
	sort.SliceStable(ports, func(i, j int) bool {
		if ports[i].IsUSB != ports[j].IsUSB {
			return ports[i].IsUSB
		}
		if ports[i].Recommended != ports[j].Recommended {
			return ports[i].Recommended
		}
		return ports[i].Path < ports[j].Path
	})
	return ports
}

func baseName(p string) string {
	if i := strings.LastIndexByte(p, '/'); i >= 0 {
		return p[i+1:]
	}
	return p
}
