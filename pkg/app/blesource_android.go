//go:build android

package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi"
)

// androidBLEMobilinkdScanner adapts kiss.ScanBLEMobilinkd (backed by the
// platformsvc BLE bridge) to webapi.BLEMobilinkdScanner.
type androidBLEMobilinkdScanner struct{}

func (androidBLEMobilinkdScanner) Scan(ctx context.Context, cb func(webapi.BLEMobilinkdDevice)) error {
	return kiss.ScanBLEMobilinkd(ctx, func(dev kiss.BLEDevice) {
		cb(webapi.BLEMobilinkdDevice{
			Addr: dev.Addr,
			Name: dev.Name,
			RSSI: dev.RSSI,
		})
	})
}

// bleMobilinkdScannerForWebapi returns an Android BLE scanner backed by the
// platformsvc client. The scanner's Scan method returns errBLEClientNotReady
// until SetAndroidBLEClient is called (in wireServicesInner), so no nil check
// is needed by the SSE handler.
func (a *App) bleMobilinkdScannerForWebapi() webapi.BLEMobilinkdScanner {
	return androidBLEMobilinkdScanner{}
}
