//go:build android

package pttdevice

import (
	"context"
	"fmt"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// PlatformUsbLister is the narrow slice of the platformsvc.Client surface
// EnumerateFromPlatformsvc depends on. Declared as an exported interface
// (rather than importing platformsvc.Client directly) so pkg/app can
// substitute a fake in tests and so the dependency direction stays
// pttdevice ← app rather than pttdevice → platformsvc — keeping the
// //go:build android import surface contained to pkg/app.
type PlatformUsbLister interface {
	ListUsbDevices(ctx context.Context, class pb.UsbClass) ([]*pb.UsbDevice, error)
}

// EnumerateFromPlatformsvc is the Android USB enumerator for the unified
// PTT tab. It lists USB devices via the supplied platformsvc client and
// classifies each into an AvailableDevice row matching the SPA wire
// contract (Type / USBVendor / USBProduct / Recommended). Returns nil
// when client is nil (platformsvc not yet connected) so the webapi
// handler ships [] rather than surfacing 500 during the brief startup
// window before the UDS handshake completes.
func EnumerateFromPlatformsvc(ctx context.Context, client PlatformUsbLister) []AvailableDevice {
	if client == nil {
		return nil
	}
	ctx2, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	devs, err := client.ListUsbDevices(ctx2, pb.UsbClass_USB_CLASS_UNKNOWN)
	if err != nil {
		return nil
	}
	out := make([]AvailableDevice, 0, len(devs))
	for _, d := range devs {
		vid := fmt.Sprintf("%04x", d.Vid)
		pid := fmt.Sprintf("%04x", d.Pid)
		typ, recommended := classifyAndroid(vid, pid)
		out = append(out, AvailableDevice{
			Path:        "",
			Type:        typ,
			Name:        d.Product,
			Description: d.Product,
			USBVendor:   vid,
			USBProduct:  pid,
			Recommended: recommended,
		})
	}
	return out
}

// classifyAndroid maps a lowercase-hex VID:PID to the unified PTT tab's
// device-type taxonomy. The four buckets correspond to the SPA's Android
// method dropdown:
//
//   - usb-cp2102n  CP2102N USB-UART bridge (Silicon Labs)
//   - usb-cdc-acm  AIOC and other CDC-ACM PTT cables
//   - usb-hid      CM108-compatible HID audio adapters with GPIO PTT
//   - usb-other    anything else; listed but not recommended
//
// Anything in usb-other is shown to the operator (so they can confirm a
// new device is enumerated even if we don't recognise it) but marked
// Recommended=false so the picker doesn't surface it as a default.
func classifyAndroid(vid, pid string) (typ string, recommended bool) {
	if vid == "10c4" && pid == "ea60" {
		return "usb-cp2102n", true
	}
	if vid == "1209" && pid == "7388" {
		return "usb-cdc-acm", true
	}
	if isCM108Compatible(vid, pid) {
		return "usb-hid", true
	}
	return "usb-other", false
}
