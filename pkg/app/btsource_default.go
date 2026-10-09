//go:build !android && !darwin

package app

import "github.com/chrissnell/graywolf/pkg/webapi"

// btSourceForWebapi returns nil on non-Android builds so the webapi
// handler responds 501 Not Implemented to GET /api/kiss/bonded-bt-devices.
// Desktop platforms have no platformsvc client to enumerate bonded
// Bluetooth devices through.
func (a *App) btSourceForWebapi() webapi.BondedBtDevicesSource { return nil }
