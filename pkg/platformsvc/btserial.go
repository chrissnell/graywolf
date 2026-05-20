//go:build android

package platformsvc

import (
	"context"
	"fmt"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// BondedBtDevice is the Go-side view of an Android Bluetooth bond entry
// returned by Client.BondedBtDevices. MAC is the colon-separated uppercase
// address (e.g. "AA:BB:CC:DD:EE:FF"); Name is the user-visible label set
// at bond time and may be empty if the device never advertised one.
type BondedBtDevice struct {
	MAC  string
	Name string
}

// BondedBtDevices issues a one-shot request to the platform service for
// the current bonded-Bluetooth-device set. Order matches the platform's
// view at the moment of the request; subsequent bond/unbond events do not
// stream back here (use the platform-service's bond-state broadcast for
// live updates).
func (c *clientImpl) BondedBtDevices(ctx context.Context) ([]BondedBtDevice, error) {
	req := &pb.PlatformMessage{Body: &pb.PlatformMessage_BondedBtDevicesRequest{
		BondedBtDevicesRequest: &pb.BondedBtDevicesRequest{},
	}}
	resp, err := c.roundTrip(ctx, req)
	if err != nil {
		return nil, err
	}
	body := resp.GetBondedBtDevicesResponse()
	if body == nil {
		return nil, fmt.Errorf("platformsvc: expected BondedBtDevicesResponse, got %T", resp.GetBody())
	}
	out := make([]BondedBtDevice, 0, len(body.GetDevices()))
	for _, d := range body.GetDevices() {
		out = append(out, BondedBtDevice{MAC: d.GetMac(), Name: d.GetName()})
	}
	return out, nil
}
