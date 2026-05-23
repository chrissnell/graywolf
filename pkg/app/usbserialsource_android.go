//go:build android

package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/webapi"
)

// platformUsbSerialSource adapts the App's live platformsvc.Client to
// webapi.AvailableUsbSerialDevicesSource. Reads a.platformClient per call so
// a late SetPlatformClient / reconnect-swap is reflected immediately.
type platformUsbSerialSource struct{ app *App }

// AvailableUsbSerialDevices forwards to the injected platformsvc client and
// converts platformsvc.UsbSerialDevice into the webapi wire type. Returns an
// empty (non-nil) slice when the client isn't ready yet so the handler ships
// [] rather than 500.
func (p platformUsbSerialSource) AvailableUsbSerialDevices(ctx context.Context) ([]webapi.AvailableUsbSerialDevice, error) {
	c := p.app.platformClient
	if c == nil {
		return []webapi.AvailableUsbSerialDevice{}, nil
	}
	devs, err := c.AvailableUsbSerialDevices(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]webapi.AvailableUsbSerialDevice, 0, len(devs))
	for _, d := range devs {
		out = append(out, webapi.AvailableUsbSerialDevice{
			VidPid:        d.VidPid,
			Product:       d.Product,
			Manufacturer:  d.Manufacturer,
			HasPermission: d.HasPermission,
		})
	}
	return out, nil
}

// usbSerialSourceForWebapi returns the webapi.AvailableUsbSerialDevicesSource
// adapter backed by the App's platformsvc client (Android only; the adapter
// handles a nil client gracefully).
func (a *App) usbSerialSourceForWebapi() webapi.AvailableUsbSerialDevicesSource {
	return platformUsbSerialSource{app: a}
}
