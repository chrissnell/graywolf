//go:build !linux

package pttdevice

import (
	"context"
	"encoding/json"
	"log/slog"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

var (
	modemPathMu sync.RWMutex
	modemPath   string
)

// SetModemPath stores the resolved modem binary path for CM108 enumeration
// on platforms that shell out to graywolf-modem --list-cm108.
func SetModemPath(path string) {
	modemPathMu.Lock()
	modemPath = path
	modemPathMu.Unlock()
}

func getModemPath() string {
	modemPathMu.RLock()
	defer modemPathMu.RUnlock()
	return modemPath
}

// enumerateCM108 shells out to the graywolf-modem binary for CM108 HID
// enumeration on macOS and Windows (no sysfs available).
func enumerateCM108() []AvailableDevice {
	p := getModemPath()
	if p == "" {
		slog.Debug("cm108: modem path not set, skipping CM108 enumeration")
		return nil
	}
	return cm108ModemInventory(p)
}

// cm108ModemInventory runs graywolf-modem --list-cm108 and parses the
// JSON output into AvailableDevice entries.
func cm108ModemInventory(modemBin string) []AvailableDevice {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, modemBin, "--list-cm108").Output()
	if err != nil {
		return nil
	}
	var entries []struct {
		Path        string `json:"path"`
		Vendor      string `json:"vendor"`
		Product     string `json:"product"`
		Description string `json:"description"`
	}
	if json.Unmarshal(out, &entries) != nil {
		return nil
	}
	var devs []AvailableDevice
	for _, e := range entries {
		devs = append(devs, AvailableDevice{
			Path:        e.Path,
			Type:        "cm108",
			Name:        filepath.Base(e.Path),
			Description: e.Description,
			USBVendor:   e.Vendor,
			USBProduct:  e.Product,
		})
	}
	return devs
}
