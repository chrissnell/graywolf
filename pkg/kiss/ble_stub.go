//go:build !android && !linux && !cgo

// Stub for platforms that cannot support BLE KISS:
//   - Android: use Bluetooth Serial (SPP/RFCOMM) instead.
//   - macOS/Windows without CGO: CoreBluetooth/WinRT requires CGO; rebuild
//     the binary with CGO_ENABLED=1 to enable BLE support.
package kiss

import (
	"context"
	"errors"
	"io"
)

// BLEDevice is the device type exposed even when BLE is unavailable, so
// blesource_desktop.go and the webapi package always compile cleanly.
type BLEDevice struct {
	Addr string
	Name string
	RSSI int16
}

func ScanBLEDevice(_ context.Context, _ func(BLEDevice)) error {
	return errBLEUnsupported
}

func OpenBLEDevice(_ string, _ uint32) (io.ReadWriteCloser, error) {
	return nil, errBLEUnsupported
}

var errBLEUnsupported = errors.New(
	"BLE KISS not available in this build — " +
		"on macOS/Windows rebuild with CGO_ENABLED=1; " +
		"on Android use Bluetooth Serial instead",
)
