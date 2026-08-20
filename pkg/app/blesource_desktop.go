//go:build !android

package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi"
)

// desktopBLEMobilinkdScanner adapts kiss.ScanBLEMobilinkd to the
// webapi.BLEMobilinkdScanner interface. On macOS with CGO enabled and
// on Linux (BlueZ), this runs a real BLE scan covering both Mobilinkd
// (proprietary service UUID) and NUS-based devices (BTECH UV-PRO,
// VERO VR-N76, Radioddity GA-5WB). On macOS without CGO the
// underlying kiss.ScanBLEMobilinkd returns an "unsupported" error which
// the SSE handler streams to the client.
type desktopBLEMobilinkdScanner struct{}

func (desktopBLEMobilinkdScanner) Scan(ctx context.Context, cb func(webapi.BLEMobilinkdDevice)) error {
	return kiss.ScanBLEMobilinkd(ctx, func(dev kiss.BLEDevice) {
		cb(webapi.BLEMobilinkdDevice{
			Addr: dev.Addr,
			Name: dev.Name,
			RSSI: dev.RSSI,
		})
	})
}

func (a *App) bleMobilinkdScannerForWebapi() webapi.BLEMobilinkdScanner {
	return desktopBLEMobilinkdScanner{}
}
