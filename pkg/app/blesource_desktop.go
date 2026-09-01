//go:build !android

package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi"
)

// desktopBLEScanner adapts kiss.ScanBLEDevice to the
// webapi.BLEScanner interface. On macOS with CGO enabled and
// on Linux (BlueZ), this runs a real BLE scan covering both Mobilinkd
// (proprietary service UUID) and NUS-based devices (BTECH UV-PRO,
// VERO VR-N76, Radioddity GA-5WB). On macOS without CGO the
// underlying kiss.ScanBLEDevice returns an "unsupported" error which
// the SSE handler streams to the client.
type desktopBLEScanner struct{}

func (desktopBLEScanner) Scan(ctx context.Context, cb func(webapi.BLEDevice)) error {
	return kiss.ScanBLEDevice(ctx, func(dev kiss.BLEDevice) {
		cb(webapi.BLEDevice{
			Addr: dev.Addr,
			Name: dev.Name,
			RSSI: dev.RSSI,
		})
	})
}

func (a *App) bleDeviceScannerForWebapi() webapi.BLEScanner {
	return desktopBLEScanner{}
}

func (a *App) bleDeviceRepairerForWebapi() webapi.BLERepairer { return nil }
