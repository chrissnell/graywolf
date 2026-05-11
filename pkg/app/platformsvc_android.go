//go:build android

package app

import "github.com/chrissnell/graywolf/pkg/platformsvc"

// On Android, App carries a live platformsvc client used by the GPS
// reader (phase 4a), the PTT relay (phase 4b), and other Service-side
// notifications. The field lives in this build-tagged file so desktop
// builds carry no platformsvc dependency at all.
type appAndroidExt struct {
	platformClient platformsvc.Client
}

// PlatformClient returns the injected platformsvc client (may be nil
// before SetPlatformClient is called).
func (a *App) PlatformClient() platformsvc.Client { return a.platformClient }

// SetPlatformClient injects the platformsvc client. Must be called
// before App.Run; gpsManager.start() reads it through platformGpsRunner.
func (a *App) SetPlatformClient(c platformsvc.Client) { a.platformClient = c }
