//go:build android

package app

import "github.com/chrissnell/graywolf/pkg/kiss"

// injectAndroidBLEClient pushes the live platformsvc client into the kiss
// package so ScanBLEMobilinkd and OpenBLEMobilinkd can route through the
// Kotlin BLE bridge. Called once during wireServicesInner after the platform
// client is confirmed non-nil.
func (a *App) injectAndroidBLEClient() {
	if a.platformClient != nil {
		kiss.SetAndroidBLEClient(a.platformClient)
	}
}
