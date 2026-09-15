//go:build android

package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi"
)

// androidBLEScanner adapts kiss.ScanBLEDevice (backed by the
// platformsvc BLE bridge) to webapi.BLEScanner.
type androidBLEScanner struct{}

func (androidBLEScanner) Scan(ctx context.Context, cb func(webapi.BLEDevice)) error {
	return kiss.ScanBLEDevice(ctx, func(dev kiss.BLEDevice) {
		cb(webapi.BLEDevice{
			Addr: dev.Addr,
			Name: dev.Name,
			RSSI: dev.RSSI,
		})
	})
}

// bleDeviceScannerForWebapi returns an Android BLE scanner backed by the
// platformsvc client. The scanner's Scan method returns errBLEClientNotReady
// until SetAndroidBLEClient is called (in wireServicesInner), so no nil check
// is needed by the SSE handler.
func (a *App) bleDeviceScannerForWebapi() webapi.BLEScanner {
	return androidBLEScanner{}
}

// bleDeviceRepairerForWebapi returns the platformsvc client as a BLERepairer.
// Only valid after wireServicesInner wires the platform client.
func (a *App) bleDeviceRepairerForWebapi() webapi.BLERepairer {
	return a.platformClient
}
