//go:build linux

package pttdevice

import (
	"os"
	"path/filepath"
	"testing"
)

// sysfsTree builds a fake sysfs directory tree in t.TempDir() for testing
// buildCM108InventoryFrom without touching real hardware.
type sysfsTree struct {
	root string
	t    *testing.T
}

func newSysfsTree(t *testing.T) *sysfsTree {
	t.Helper()
	return &sysfsTree{root: t.TempDir(), t: t}
}

func (s *sysfsTree) writeFile(relPath, content string) {
	s.t.Helper()
	abs := filepath.Join(s.root, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sysfsTree) symlink(relTarget, relLink string) {
	s.t.Helper()
	absLink := filepath.Join(s.root, relLink)
	absTarget := filepath.Join(s.root, relTarget)
	if err := os.MkdirAll(filepath.Dir(absLink), 0o755); err != nil {
		s.t.Fatal(err)
	}
	if err := os.Symlink(absTarget, absLink); err != nil {
		s.t.Fatal(err)
	}
}

func (s *sysfsTree) mkdir(relPath string) {
	s.t.Helper()
	if err := os.MkdirAll(filepath.Join(s.root, relPath), 0o755); err != nil {
		s.t.Fatal(err)
	}
}

// addUSBDevice creates a USB device parent with vendor/product identity files.
func (s *sysfsTree) addUSBDevice(busPath, vendor, product, productStr string) {
	s.t.Helper()
	s.writeFile("devices/"+busPath+"/idVendor", vendor)
	s.writeFile("devices/"+busPath+"/idProduct", product)
	if productStr != "" {
		s.writeFile("devices/"+busPath+"/product", productStr)
	}
}

// addSoundCard creates a sound card sysfs node under the given USB interface
// and a class/sound/cardN symlink pointing to it.
func (s *sysfsTree) addSoundCard(busPath, iface, cardNum, cardID string) {
	s.t.Helper()
	devPath := "devices/" + busPath + "/" + iface + "/sound/card" + cardNum
	s.writeFile(devPath+"/id", cardID)
	s.symlink(devPath, "class/sound/card"+cardNum)
}

// addHidraw creates a hidraw sysfs node under the given USB interface, writes
// the bInterfaceNumber file, creates the device→interface symlink, and adds
// a class/hidraw/hidrawN symlink.
func (s *sysfsTree) addHidraw(busPath, iface, hidrawNum, bIfaceNum string) {
	s.t.Helper()
	ifacePath := "devices/" + busPath + "/" + iface
	hidrawPath := ifacePath + "/hidraw/hidraw" + hidrawNum
	s.writeFile(ifacePath+"/bInterfaceNumber", bIfaceNum)
	s.mkdir(hidrawPath)
	s.symlink(ifacePath, hidrawPath+"/device")
	s.symlink(hidrawPath, "class/hidraw/hidraw"+hidrawNum)
}

func TestBuildCM108Inventory(t *testing.T) {
	t.Run("CM108 composite device with correct interface", func(t *testing.T) {
		s := newSysfsTree(t)
		s.addUSBDevice("usb1/1-2", "0d8c", "000c", "USB Audio Device")
		s.addSoundCard("usb1/1-2", "1-2:1.0", "0", "Device")
		s.addHidraw("usb1/1-2", "1-2:1.3", "0", "03")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 1 {
			t.Fatalf("got %d entries, want 1", len(entries))
		}
		e := entries[0]
		if e.HidrawPath != "/dev/hidraw0" {
			t.Errorf("HidrawPath = %q, want /dev/hidraw0", e.HidrawPath)
		}
		if e.Vendor != "0d8c" || e.Product != "000c" {
			t.Errorf("VID:PID = %s:%s, want 0d8c:000c", e.Vendor, e.Product)
		}
		if e.CardNumber != "0" {
			t.Errorf("CardNumber = %q, want 0", e.CardNumber)
		}
		if e.InterfaceNum != "03" {
			t.Errorf("InterfaceNum = %q, want 03", e.InterfaceNum)
		}
		// knownUSBDevices maps 0d8c:000c to a specific description
		if e.Description != "CM108 USB Audio (GPIO PTT capable)" {
			t.Errorf("Description = %q, want known-device description", e.Description)
		}
	})

	t.Run("non-CM108 vendor excluded", func(t *testing.T) {
		s := newSysfsTree(t)
		// FTDI device — not in cm108VendorSet or cm108VIDPIDSet
		s.addUSBDevice("usb1/1-3", "0403", "6001", "FTDI Serial")
		s.addSoundCard("usb1/1-3", "1-3:1.0", "1", "FTDIAudio")
		s.addHidraw("usb1/1-3", "1-3:1.2", "1", "03")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 0 {
			t.Fatalf("got %d entries, want 0 (non-CM108 should be excluded)", len(entries))
		}
	})

	t.Run("wrong bInterfaceNumber on composite device excluded", func(t *testing.T) {
		s := newSysfsTree(t)
		s.addUSBDevice("usb1/1-4", "0d8c", "000e", "USB Audio")
		s.addSoundCard("usb1/1-4", "1-4:1.0", "2", "Audio")
		// Interface 02 instead of 03 on a composite device
		s.addHidraw("usb1/1-4", "1-4:1.2", "2", "02")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 0 {
			t.Fatalf("got %d entries, want 0 (wrong interface should be excluded)", len(entries))
		}
	})

	t.Run("AIOC matched by VID:PID", func(t *testing.T) {
		s := newSysfsTree(t)
		// AIOC uses pid.codes VID 1209, not in cm108VendorSet — matched by cm108VIDPIDSet
		s.addUSBDevice("usb1/1-5", "1209", "7388", "AIOC")
		s.addSoundCard("usb1/1-5", "1-5:1.0", "3", "AIOC")
		s.addHidraw("usb1/1-5", "1-5:1.3", "3", "03")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 1 {
			t.Fatalf("got %d entries, want 1", len(entries))
		}
		if entries[0].Vendor != "1209" || entries[0].Product != "7388" {
			t.Errorf("VID:PID = %s:%s, want 1209:7388", entries[0].Vendor, entries[0].Product)
		}
		if entries[0].Description != "AIOC All-In-One-Cable (CM108-compatible PTT)" {
			t.Errorf("Description = %q, want known AIOC description", entries[0].Description)
		}
	})

	t.Run("non-composite device accepts any interface number", func(t *testing.T) {
		s := newSysfsTree(t)
		s.addUSBDevice("usb1/1-6", "0d8c", "013c", "USB Audio")
		// Single interface: sound and hidraw both under 1-6:1.0
		s.addSoundCard("usb1/1-6", "1-6:1.0", "4", "Audio")
		s.addHidraw("usb1/1-6", "1-6:1.0", "4", "00")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 1 {
			t.Fatalf("got %d entries, want 1 (non-composite should accept any iface)", len(entries))
		}
		if entries[0].InterfaceNum != "00" {
			t.Errorf("InterfaceNum = %q, want 00", entries[0].InterfaceNum)
		}
	})

	t.Run("hidraw without matching sound card excluded", func(t *testing.T) {
		s := newSysfsTree(t)
		s.addUSBDevice("usb1/1-7", "0d8c", "000c", "USB Audio")
		// Only hidraw, no sound card for this USB device
		s.addHidraw("usb1/1-7", "1-7:1.3", "5", "03")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 0 {
			t.Fatalf("got %d entries, want 0 (no sound card = no match)", len(entries))
		}
	})

	t.Run("sound card without hidraw excluded from results", func(t *testing.T) {
		s := newSysfsTree(t)
		s.addUSBDevice("usb1/1-8", "0d8c", "000c", "USB Audio")
		// Only sound card, no hidraw for this USB device
		s.addSoundCard("usb1/1-8", "1-8:1.0", "6", "Audio")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 0 {
			t.Fatalf("got %d entries, want 0 (no hidraw = no match)", len(entries))
		}
	})

	t.Run("SSS vendor matched", func(t *testing.T) {
		s := newSysfsTree(t)
		s.addUSBDevice("usb1/1-9", "0c76", "161f", "SSS Audio")
		s.addSoundCard("usb1/1-9", "1-9:1.0", "7", "SSS")
		s.addHidraw("usb1/1-9", "1-9:1.3", "7", "03")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 1 {
			t.Fatalf("got %d entries, want 1", len(entries))
		}
		if entries[0].Vendor != "0c76" {
			t.Errorf("Vendor = %q, want 0c76", entries[0].Vendor)
		}
	})

	t.Run("multiple devices return multiple entries", func(t *testing.T) {
		s := newSysfsTree(t)
		// Two CM108 devices on the same bus
		s.addUSBDevice("usb1/1-2", "0d8c", "000c", "USB Audio 1")
		s.addSoundCard("usb1/1-2", "1-2:1.0", "0", "Dev0")
		s.addHidraw("usb1/1-2", "1-2:1.3", "0", "03")

		s.addUSBDevice("usb1/1-3", "0d8c", "000e", "USB Audio 2")
		s.addSoundCard("usb1/1-3", "1-3:1.0", "1", "Dev1")
		s.addHidraw("usb1/1-3", "1-3:1.3", "1", "03")

		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 2 {
			t.Fatalf("got %d entries, want 2", len(entries))
		}
	})

	t.Run("empty sysfs returns nothing", func(t *testing.T) {
		s := newSysfsTree(t)
		entries := buildCM108InventoryFrom(s.root)
		if len(entries) != 0 {
			t.Fatalf("got %d entries, want 0", len(entries))
		}
	})
}

func TestIsCompositeUSBDevice(t *testing.T) {
	t.Run("multiple interfaces is composite", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "1-2:1.0"), 0o755)
		os.MkdirAll(filepath.Join(dir, "1-2:1.3"), 0o755)
		if !isCompositeUSBDevice(dir) {
			t.Error("expected composite=true with two interface dirs")
		}
	})

	t.Run("single interface is not composite", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "1-2:1.0"), 0o755)
		if isCompositeUSBDevice(dir) {
			t.Error("expected composite=false with one interface dir")
		}
	})

	t.Run("no interface dirs is not composite", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "power"), 0o755)
		if isCompositeUSBDevice(dir) {
			t.Error("expected composite=false with no interface dirs")
		}
	})

	t.Run("non-directory entries with colon ignored", func(t *testing.T) {
		dir := t.TempDir()
		os.MkdirAll(filepath.Join(dir, "1-2:1.0"), 0o755)
		os.WriteFile(filepath.Join(dir, "1-2:1.1"), []byte("file"), 0o644)
		if isCompositeUSBDevice(dir) {
			t.Error("expected composite=false (file with colon should not count)")
		}
	})
}
