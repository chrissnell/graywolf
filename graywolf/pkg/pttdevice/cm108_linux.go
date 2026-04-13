package pttdevice

import (
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

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

// cm108VendorSet lists USB vendor IDs whose devices have CM108-compatible
// HID GPIO. Matched by vendor alone (any product ID).
//
// When adding a new CM108-compatible device: if its vendor ID covers only
// CM108 parts, add the vendor here. Otherwise add the specific VID:PID to
// cm108VIDPIDSet below. Also add a description to knownUSBDevices in
// sysfs_linux.go.
var cm108VendorSet = map[string]bool{
	"0d8c": true, // C-Media (CM108, CM108B, CM108AH, CM109, CM119, CM119A)
	"0c76": true, // SSS
}

// cm108VIDPIDSet lists specific VID:PID pairs for CM108-compatible devices
// whose vendor ID alone is not sufficient (e.g. AIOC uses pid.codes VID).
// See cm108VendorSet comment for maintenance instructions.
var cm108VIDPIDSet = map[string]bool{
	"1209:7388": true, // AIOC All-In-One-Cable
}

// buildCM108Inventory correlates ALSA sound cards with their HID (hidraw)
// control interfaces via the sysfs tree. Both the sound and hidraw nodes
// for a physical USB device share a common USB device ancestor; this
// ancestor's realpath is used as the join key (Direwolf uses the same
// approach via libudev).
func buildCM108Inventory() []cm108Entry {
	cardsByParent := map[string]*cm108Entry{}

	// Pass 1: /sys/class/sound/card* → USB parent → card info.
	// Only records cards whose USB ancestor is a CM108-compatible vendor.
	soundCards, _ := filepath.Glob("/sys/class/sound/card[0-9]*")
	for _, cardPath := range soundCards {
		usbParent := usbParentDir(cardPath)
		if usbParent == "" {
			slog.Debug("cm108: skipping sound card (no USB parent)", "path", cardPath)
			continue
		}

		vendor := readSysfsFile(filepath.Join(usbParent, "idVendor"))
		product := readSysfsFile(filepath.Join(usbParent, "idProduct"))
		vidpid := vendor + ":" + product

		if !cm108VendorSet[vendor] && !cm108VIDPIDSet[vidpid] {
			slog.Debug("cm108: skipping sound card (not CM108-compatible)", "path", cardPath, "vidpid", vidpid)
			continue
		}

		cardNum := strings.TrimPrefix(filepath.Base(cardPath), "card")
		cardName := readSysfsFile(filepath.Join(cardPath, "id"))

		desc := readSysfsFile(filepath.Join(usbParent, "product"))
		if known, ok := knownUSBDevices[vidpid]; ok {
			desc = known
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
	hidrawPaths, _ := filepath.Glob("/sys/class/hidraw/hidraw[0-9]*")
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
