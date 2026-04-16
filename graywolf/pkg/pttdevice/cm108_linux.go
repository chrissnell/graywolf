//go:build linux

package pttdevice

import (
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// SetModemPath is a no-op on Linux where CM108 enumeration uses sysfs
// directly. Non-linux platforms store this for modem shell-out enumeration.
func SetModemPath(_ string) {}

// cm108Entry represents a correlated CM108-compatible device found via sysfs,
// linking its ALSA sound card identity to its HID (hidraw) control path.
type cm108Entry struct {
	USBParent    string // realpath of USB device dir (join key)
	Vendor       string // USB vendor ID (e.g. "0d8c")
	Product      string // USB product ID
	CardNumber   string // ALSA card number (e.g. "1")
	CardName     string // ALSA card id string
	HidrawPath   string // /dev/hidrawN
	InterfaceNum string // bInterfaceNumber of the hidraw's USB interface
	Description  string
}

// buildCM108Inventory correlates ALSA sound cards with their HID (hidraw)
// control interfaces via the sysfs tree. Both the sound and hidraw nodes
// for a physical USB device share a common USB device ancestor; this
// ancestor's realpath is used as the join key (Direwolf uses the same
// approach via libudev).
func buildCM108Inventory() []cm108Entry {
	return buildCM108InventoryFrom("/sys")
}

// buildCM108InventoryFrom is the testable core of buildCM108Inventory.
// sysRoot is the sysfs mount point ("/sys" in production, a temp dir in tests).
func buildCM108InventoryFrom(sysRoot string) []cm108Entry {
	cardsByParent := map[string]*cm108Entry{}

	// Pass 1: /sys/class/sound/card* → USB parent → card info.
	// Only records cards whose USB ancestor is a CM108-compatible vendor.
	soundCards, _ := filepath.Glob(filepath.Join(sysRoot, "class/sound/card[0-9]*"))
	for _, cardPath := range soundCards {
		usbParent := usbParentDir(cardPath)
		if usbParent == "" {
			slog.Debug("cm108: skipping sound card (no USB parent)", "path", cardPath)
			continue
		}

		vendor := readSysfsFile(filepath.Join(usbParent, "idVendor"))
		product := readSysfsFile(filepath.Join(usbParent, "idProduct"))
		vidpid := vendor + ":" + product

		if !isCM108Compatible(vendor, product) {
			slog.Debug("cm108: skipping sound card (not CM108-compatible)", "path", cardPath, "vidpid", vidpid)
			continue
		}

		cardNum := strings.TrimPrefix(filepath.Base(cardPath), "card")
		cardName := readSysfsFile(filepath.Join(cardPath, "id"))

		desc := readSysfsFile(filepath.Join(usbParent, "product"))
		if name := usbDeviceName(vendor, product); name != "" {
			desc = name
		}

		cardsByParent[usbParent] = &cm108Entry{
			USBParent:   usbParent,
			Vendor:      vendor,
			Product:     product,
			CardNumber:  cardNum,
			CardName:    cardName,
			Description: desc,
		}
	}

	// Pass 2: /sys/class/hidraw/hidraw* → find USB parent → join with Pass 1.
	hidrawPaths, _ := filepath.Glob(filepath.Join(sysRoot, "class/hidraw/hidraw[0-9]*"))
	for _, hidrawSys := range hidrawPaths {
		usbParent := usbParentDir(hidrawSys)
		if usbParent == "" {
			continue
		}

		entry, ok := cardsByParent[usbParent]
		if !ok {
			continue
		}

		// Resolve device symlink to the USB interface directory to read
		// bInterfaceNumber. CM108 HID is interface 03; AIOC mirrors this.
		ifaceDir, err := filepath.EvalSymlinks(filepath.Join(hidrawSys, "device"))
		if err != nil {
			slog.Debug("cm108: cannot resolve hidraw device symlink", "path", hidrawSys, "err", err)
			continue
		}
		ifaceNum := readSysfsFile(filepath.Join(ifaceDir, "bInterfaceNumber"))

		// On composite devices (multiple USB interfaces), only accept
		// interface 03 to avoid opening the wrong hidraw node.
		// Non-composite devices (single interface): accept any number.
		if ifaceNum != "03" && isCompositeUSBDevice(usbParent) {
			slog.Debug("cm108: skipping hidraw (wrong interface on composite device)",
				"path", hidrawSys, "iface", ifaceNum)
			continue
		}

		entry.HidrawPath = "/dev/" + filepath.Base(hidrawSys)
		entry.InterfaceNum = ifaceNum
		slog.Debug("cm108: matched hidraw to sound card",
			"hidraw", entry.HidrawPath, "card", entry.CardNumber, "iface", ifaceNum)
	}

	// Return entries that have both a sound card and a hidraw path.
	var result []cm108Entry
	for _, entry := range cardsByParent {
		if entry.HidrawPath != "" {
			result = append(result, *entry)
		}
	}
	return result
}

// enumerateCM108 returns CM108-compatible HID devices correlated with their
// ALSA sound cards via the sysfs tree. Linux-only; non-linux platforms use
// the modem shell-out in cm108_modem.go.
func enumerateCM108() []AvailableDevice {
	var devs []AvailableDevice
	for _, entry := range buildCM108Inventory() {
		devs = append(devs, AvailableDevice{
			Path:        entry.HidrawPath,
			Type:        "cm108",
			Name:        filepath.Base(entry.HidrawPath),
			Description: fmt.Sprintf("%s (ALSA card %s)", entry.Description, entry.CardNumber),
			USBVendor:   entry.Vendor,
			USBProduct:  entry.Product,
		})
	}
	return devs
}

// isCompositeUSBDevice returns true if the USB device at the given sysfs
// path has multiple interfaces. USB interface directories are named by the
// kernel as "busnum-devpath:config.iface" (e.g. "1-2:1.0"), so the presence
// of a colon distinguishes them from other child directories.
func isCompositeUSBDevice(usbDevDir string) bool {
	entries, err := os.ReadDir(usbDevDir)
	if err != nil {
		return false
	}
	count := 0
	for _, e := range entries {
		if e.IsDir() && strings.Contains(e.Name(), ":") {
			count++
			if count > 1 {
				return true
			}
		}
	}
	return false
}
