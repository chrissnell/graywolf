package pttdevice

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSerialByIDPreservesPathsAndDeviceClassification(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux udev aliases")
	}
	dir := t.TempDir()
	original := AvailableDevice{Path: os.DevNull, Name: "ttyUSB0", Type: "serial", Description: "USB adapter", USBVendor: "1a86", USBProduct: "7523", Recommended: true}
	alias := filepath.Join(dir, "usb-adapter-id")
	if err := os.Symlink(os.DevNull, alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "missing"), filepath.Join(dir, "broken")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "not-an-alias"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	got := withSerialByID([]AvailableDevice{original}, dir)
	if len(got) != 2 || got[0] != original {
		t.Fatalf("kernel path changed: %+v", got)
	}
	if got[1].Path != alias || got[1].USBVendor != original.USBVendor || !got[1].Recommended {
		t.Fatalf("alias identity: %+v", got[1])
	}
	original.Recommended = false
	original.Warning = "Use the HID entry"
	got = withSerialByID([]AvailableDevice{original}, dir)
	if got[1].Recommended || got[1].Warning != original.Warning {
		t.Fatalf("must preserve device warning: %+v", got[1])
	}
	if len(withSerialByID(got, dir)) != 2 {
		t.Fatal("duplicate alias")
	}
	if len(withSerialByID([]AvailableDevice{{Path: os.DevNull, Type: "cm108"}}, dir)) != 1 {
		t.Fatal("non-serial alias accepted")
	}
	if len(withSerialByID(nil, dir)) != 0 {
		t.Fatal("unknown target accepted")
	}
}

func TestSerialByIDMissingDirectoryAndNonCharacterTarget(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "regular")
	if err := os.WriteFile(file, nil, 0600); err != nil {
		t.Fatal(err)
	}
	aliases := filepath.Join(dir, "by-id")
	if err := os.Mkdir(aliases, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(file, filepath.Join(aliases, "usb-fake")); err != nil {
		t.Skip(err)
	}
	devs := []AvailableDevice{{Path: file, Type: "serial"}}
	if len(withSerialByID(devs, aliases)) != 1 {
		t.Fatal("regular file accepted as serial device")
	}
	if len(withSerialByID(devs, filepath.Join(dir, "missing"))) != 1 {
		t.Fatal("missing by-id directory dropped kernel paths")
	}
}
