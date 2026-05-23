//go:build !android

package app

import "github.com/chrissnell/graywolf/pkg/webapi"

// usbSerialSourceForWebapi returns nil on non-Android builds so the webapi
// handler responds 501 Not Implemented to
// GET /api/kiss/available-usb-serial-devices. Desktop platforms have no
// platformsvc client to enumerate USB serial devices through.
func (a *App) usbSerialSourceForWebapi() webapi.AvailableUsbSerialDevicesSource { return nil }
