//go:build darwin

package app

import (
	"context"

	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi"
)

// macBtSource lists the Bluetooth Classic devices paired with this Mac
// (those offering a Serial Port service) for the Bluetooth interface's
// device picker, using the same endpoint and UI as Android's bonded list.
type macBtSource struct{}

func (macBtSource) BondedBtDevices(_ context.Context) ([]webapi.BondedBtDevice, error) {
	devs, err := kiss.PairedClassicBluetooth()
	if err != nil {
		return nil, err
	}
	out := make([]webapi.BondedBtDevice, 0, len(devs))
	for _, d := range devs {
		out = append(out, webapi.BondedBtDevice{MAC: d.MAC, Name: d.Name})
	}
	return out, nil
}

// btSourceForWebapi returns the paired-device source on macOS builds that
// include the native Bluetooth transport, otherwise nil (the handler then
// answers 501 and the UI hides the Bluetooth option).
func (a *App) btSourceForWebapi() webapi.BondedBtDevicesSource {
	if !kiss.ClassicBluetoothSupported() {
		return nil
	}
	return macBtSource{}
}
