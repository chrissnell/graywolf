package pttdevice

import (
	"strings"
	"testing"
)

func TestAnnotateAndSort_DemotesCM108CompositeSerial(t *testing.T) {
	// AIOC (VID 1209 PID 7388) exposes both a CDC-ACM serial port and a
	// CM108-compatible HID. The serial port should be demoted so the UI
	// steers users to the HID for PTT, and the warning should name the
	// concrete hidraw path so users know exactly which card to pick.
	devs := []AvailableDevice{
		{
			Path:        "/dev/ttyACM1",
			Type:        "serial",
			USBVendor:   "1209",
			USBProduct:  "7388",
			Recommended: true,
		},
		{
			Path:        "/dev/hidraw0",
			Type:        "cm108",
			USBVendor:   "1209",
			USBProduct:  "7388",
			Recommended: true,
		},
	}
	got := annotateAndSort(devs)

	// CM108 must sort first (Recommended), AIOC serial last (not Recommended).
	if got[0].Type != "cm108" {
		t.Errorf("got[0].Type = %q, want cm108", got[0].Type)
	}
	if got[1].Type != "serial" {
		t.Errorf("got[1].Type = %q, want serial", got[1].Type)
	}
	if got[1].Recommended {
		t.Error("AIOC serial should be demoted to Recommended=false")
	}
	if !strings.Contains(got[1].Warning, "/dev/hidraw0") {
		t.Errorf("AIOC serial warning should name /dev/hidraw0, got %q", got[1].Warning)
	}
}

func TestAnnotateAndSort_CM108SerialWarningFallback(t *testing.T) {
	// If only the serial side of a CM108 composite adapter is listed (the
	// HID didn't enumerate for some reason), the warning falls back to a
	// generic message — we can't name a hidraw we don't have.
	devs := []AvailableDevice{
		{
			Path:        "/dev/ttyACM1",
			Type:        "serial",
			USBVendor:   "1209",
			USBProduct:  "7388",
			Recommended: true,
		},
	}
	got := annotateAndSort(devs)
	if got[0].Recommended {
		t.Error("AIOC serial should be demoted even without a CM108 companion")
	}
	if !strings.Contains(got[0].Warning, "CM108 HID") {
		t.Errorf("fallback warning should mention CM108 HID, got %q", got[0].Warning)
	}
}

func TestAnnotateAndSort_KeepsNonCM108SerialRecommended(t *testing.T) {
	// CH340 (Digirig, generic USB-serial) is NOT CM108-compatible — its
	// serial port IS the canonical PTT path via RTS. Must stay recommended.
	devs := []AvailableDevice{
		{
			Path:        "/dev/ttyUSB0",
			Type:        "serial",
			USBVendor:   "1a86",
			USBProduct:  "7523",
			Recommended: true,
		},
	}
	got := annotateAndSort(devs)
	if !got[0].Recommended {
		t.Error("CH340 serial must remain Recommended=true")
	}
	if got[0].Warning != "" {
		t.Errorf("CH340 serial should not carry a warning; got %q", got[0].Warning)
	}
}

func TestAnnotateAndSort_HandlesMissingUSBInfo(t *testing.T) {
	// Non-USB serial (e.g., /dev/ttyS0) has empty USBVendor. Must not
	// trigger the CM108-compatible demotion (would be a lookup false hit).
	devs := []AvailableDevice{
		{
			Path:        "/dev/ttyS0",
			Type:        "serial",
			Recommended: true,
		},
	}
	got := annotateAndSort(devs)
	if !got[0].Recommended {
		t.Error("plain serial with no USB info must remain Recommended=true")
	}
}
