//go:build android

package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/webapi"
)

// platformBtSource adapts the App's live platformsvc.Client to
// webapi.BondedBtDevicesSource. The adapter reads a.platformClient on
// each call (not at construction) so a future late SetPlatformClient
// or reconnect-swap is reflected immediately. Today main_android.go
// calls SetPlatformClient before a.Run so the difference is
// theoretical — kissSerialOpenFunc in kiss_openfunc_android.go captures
// the client once at construction and works fine — but the per-call
// form keeps the door open for late-binding without surprising the
// UI's polling loop, at no measurable cost (one pointer load per
// /api/kiss/bonded-bt-devices request).
type platformBtSource struct{ app *App }

// BondedBtDevices forwards to the injected platformsvc client and
// converts platformsvc.BondedBtDevice into the webapi wire type. Returns
// an empty (non-nil) slice when the platform client isn't ready yet so
// the handler ships [] rather than 500.
func (p platformBtSource) BondedBtDevices(ctx context.Context) ([]webapi.BondedBtDevice, error) {
	c := p.app.platformClient
	if c == nil {
		return []webapi.BondedBtDevice{}, nil
	}
	devs, err := c.BondedBtDevices(ctx)
	if err != nil {
		p.app.logger.Warn("kiss-bt: bonded device list failed", "err", err)
		return nil, err
	}
	out := make([]webapi.BondedBtDevice, 0, len(devs))
	names := make([]string, 0, len(devs))
	for _, d := range devs {
		out = append(out, webapi.BondedBtDevice{MAC: d.MAC, Name: d.Name})
		names = append(names, d.Name+" ("+d.MAC+")")
	}
	p.app.logger.Info("kiss-bt: bonded device list", "count", len(out), "devices", names)
	return out, nil
}

// btSourceForWebapi returns the webapi.BondedBtDevicesSource adapter
// backed by the App's platformsvc client. Returned unconditionally on
// Android — the adapter itself handles a nil client gracefully — so the
// handler responds 200 (with [] when the bond set is empty or the client
// isn't connected yet) rather than 501.
func (a *App) btSourceForWebapi() webapi.BondedBtDevicesSource {
	return platformBtSource{app: a}
}
