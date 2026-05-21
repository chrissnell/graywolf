//go:build android

package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/pttdevice"
	"github.com/chrissnell/graywolf/pkg/webapi"
)

// platformPttSource adapts the App's live platformsvc client to
// webapi.PttDeviceSource. It reads a.platformClient on each call (not at
// construction) so a future late SetPlatformClient or reconnect-swap is
// reflected immediately — the unified-PTT-tab SPA polls
// /api/ptt/available at modal-open time and during the "Auto-detect"
// flow, so a freshly-reconnected client must show up without a server
// restart. Mirrors the platformBtSource pattern in btsource_android.go.
type platformPttSource struct{ app *App }

// ListPttDevices forwards to pttdevice.EnumerateFromPlatformsvc with the
// live platformsvc client. When the client isn't connected yet
// (a.platformClient == nil), returns nil — the webapi handler ships []
// rather than 500 so the SPA's empty-state renders cleanly during the
// brief startup window before the UDS handshake completes.
func (p platformPttSource) ListPttDevices(ctx context.Context) []pttdevice.AvailableDevice {
	c := p.app.platformClient
	if c == nil {
		return nil
	}
	return pttdevice.EnumerateFromPlatformsvc(ctx, c)
}

// pttSourceForWebapi returns the webapi.PttDeviceSource adapter backed
// by the App's platformsvc client. Returned unconditionally on Android —
// the adapter itself handles a nil client gracefully — so the handler
// answers 200 (with [] when nothing is plugged in or the client hasn't
// connected yet) rather than falling through to pttdevice.Enumerate()
// which would scan host /dev paths that don't exist on Android.
func (a *App) pttSourceForWebapi() webapi.PttDeviceSource {
	return platformPttSource{app: a}
}
