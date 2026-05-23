//go:build android

package platformsvc

import (
	"context"
	"fmt"
	"io"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// UsbSerialDevice is the Go-side view of an attached, serial-capable USB
// device returned by Client.AvailableUsbSerialDevices. VidPid is lowercase
// hex "vid:pid" (e.g. "2341:0043"); HasPermission reflects whether the
// Android USB permission has been granted for this device.
type UsbSerialDevice struct {
	VidPid        string
	Product       string
	Manufacturer  string
	HasPermission bool
}

// AvailableUsbSerialDevices enumerates the attached USB devices the platform
// service recognizes as serial-capable (probeable by usb-serial-for-android).
// One-shot request/response; safe to call repeatedly. No USB permission is
// required to enumerate.
func (c *clientImpl) AvailableUsbSerialDevices(ctx context.Context) ([]UsbSerialDevice, error) {
	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_AvailableUsbSerialDevicesRequest{
		AvailableUsbSerialDevicesRequest: &pb.AvailableUsbSerialDevicesRequest{},
	}}
	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	body := resp.GetAvailableUsbSerialDevicesResponse()
	if body == nil {
		return nil, fmt.Errorf("platformsvc: expected AvailableUsbSerialDevicesResponse, got %T", resp.GetBody())
	}
	out := make([]UsbSerialDevice, 0, len(body.GetDevices()))
	for _, d := range body.GetDevices() {
		out = append(out, UsbSerialDevice{
			VidPid:        d.GetVidPid(),
			Product:       d.GetProduct(),
			Manufacturer:  d.GetManufacturer(),
			HasPermission: d.GetHasPermission(),
		})
	}
	return out, nil
}

// UsbSerialOpen opens a serial stream to the attached USB device matching
// vidPid ("vid:pid" hex) at the given baud, returning a multiplexed
// io.ReadWriteCloser. Reuses the shared serial-stream machinery; the only
// USB-specific input is SerialKind_USB plus the baud rate. Close tears down
// the USB port server-side.
func (c *clientImpl) UsbSerialOpen(ctx context.Context, vidPid string, baud uint32) (io.ReadWriteCloser, error) {
	return c.openSerialStream(ctx, func(handle uint32) *pb.SerialOpen {
		return &pb.SerialOpen{
			Handle:  handle,
			Kind:    pb.SerialKind_SERIAL_KIND_USB,
			Address: vidPid,
			Baud:    baud,
		}
	})
}
