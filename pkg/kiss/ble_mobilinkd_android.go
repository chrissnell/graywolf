//go:build android

// Android BLE KISS implementation. Scanning and GATT connection are
// handled by the Kotlin PlatformServer (BleFacade / BleAdapter); this
// file bridges those platform calls into the same BLEDevice / OpenFunc
// surface that the desktop tinygo/bluetooth implementation exposes.
package kiss

import (
	"context"
	"errors"
	"io"
	"sync/atomic"

	"github.com/chrissnell/graywolf/pkg/platformsvc"
)

// BLEDevice is a discovered BLE KISS TNC (matches the desktop type).
type BLEDevice struct {
	Addr string
	Name string
	RSSI int16
}

// androidBLEClient is the narrow surface used by this package.
type androidBLEClient interface {
	ScanBLEKISS(ctx context.Context, discovered func(platformsvc.BLEKISSDevice)) error
	BLEOpen(ctx context.Context, addr string) (io.ReadWriteCloser, error)
}

var androidBLEClientVal atomic.Value // stores androidBLEClient

// SetAndroidBLEClient injects the platform client for BLE. Called by
// app/wiring.go after the platform client is ready. Safe to call once.
func SetAndroidBLEClient(c androidBLEClient) {
	androidBLEClientVal.Store(c)
}

func getClient() androidBLEClient {
	v, _ := androidBLEClientVal.Load().(androidBLEClient)
	return v
}

var errBLEClientNotReady = errors.New("BLE KISS: platform client not initialized")

// ScanBLEMobilinkd scans for nearby BLE KISS TNCs (Mobilinkd and NUS-based).
// Calls discovered for each device found and blocks until ctx is cancelled.
func ScanBLEMobilinkd(ctx context.Context, discovered func(BLEDevice)) error {
	c := getClient()
	if c == nil {
		return errBLEClientNotReady
	}
	return c.ScanBLEKISS(ctx, func(d platformsvc.BLEKISSDevice) {
		discovered(BLEDevice{Addr: d.Addr, Name: d.Name, RSSI: d.RSSI})
	})
}

// OpenBLEMobilinkd connects to the BLE KISS TNC at addr (MAC string).
// The baud parameter is ignored (BLE is a framed byte stream).
func OpenBLEMobilinkd(addr string, _ uint32) (io.ReadWriteCloser, error) {
	c := getClient()
	if c == nil {
		return nil, errBLEClientNotReady
	}
	return c.BLEOpen(context.Background(), addr)
}
