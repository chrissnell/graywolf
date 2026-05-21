//go:build android

package pttdevice

import (
	"context"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// fakePlatformClient stands in for the live platformsvc client in
// EnumerateFromPlatformsvc tests. Only the slice of methods used by the
// enumerator is implemented; the production Client interface is larger.
type fakePlatformClient struct {
	devices []*pb.UsbDevice
	err     error
}

func (f *fakePlatformClient) ListUsbDevices(_ context.Context, _ pb.UsbClass) ([]*pb.UsbDevice, error) {
	return f.devices, f.err
}

// TestEnumerateAndroidClassifiesKnownVidPids verifies the four
// VID:PID classification buckets EnumerateFromPlatformsvc must produce
// for the SPA wire contract.
func TestEnumerateAndroidClassifiesKnownVidPids(t *testing.T) {
	client := &fakePlatformClient{devices: []*pb.UsbDevice{
		{Vid: 0x10c4, Pid: 0xea60, Product: "CP2102N USB to UART Bridge Controller"},
		{Vid: 0x1209, Pid: 0x7388, Product: "AIOC All-In-One-Cable"},
		{Vid: 0x0d8c, Pid: 0x013c, Product: "C-Media Electronics, Inc. Audio Adapter"},
		{Vid: 0x1234, Pid: 0x5678, Product: "Unknown USB Device"},
	}}
	out := EnumerateFromPlatformsvc(context.Background(), client)
	if len(out) != 4 {
		t.Fatalf("want 4 devices, got %d", len(out))
	}

	byVid := map[string]AvailableDevice{}
	for _, d := range out {
		byVid[d.USBVendor+":"+d.USBProduct] = d
	}

	cp := byVid["10c4:ea60"]
	if cp.Type != "usb-cp2102n" || !cp.Recommended {
		t.Errorf("CP2102N: want type=usb-cp2102n recommended=true, got type=%s recommended=%v", cp.Type, cp.Recommended)
	}
	aioc := byVid["1209:7388"]
	if aioc.Type != "usb-cdc-acm" || !aioc.Recommended {
		t.Errorf("AIOC: want type=usb-cdc-acm recommended=true, got type=%s recommended=%v", aioc.Type, aioc.Recommended)
	}
	cm := byVid["0d8c:013c"]
	if cm.Type != "usb-hid" || !cm.Recommended {
		t.Errorf("CM108: want type=usb-hid recommended=true, got type=%s recommended=%v", cm.Type, cm.Recommended)
	}
	other := byVid["1234:5678"]
	if other.Type != "usb-other" || other.Recommended {
		t.Errorf("Unknown: want type=usb-other recommended=false, got type=%s recommended=%v", other.Type, other.Recommended)
	}
}

// TestEnumerateFromPlatformsvcReturnsNilOnNilClient ensures the
// handler-friendly nil-source path returns nil so the webapi layer
// ships [] rather than 500.
func TestEnumerateFromPlatformsvcReturnsNilOnNilClient(t *testing.T) {
	if out := EnumerateFromPlatformsvc(context.Background(), nil); out != nil {
		t.Errorf("nil client should return nil, got %v", out)
	}
}
