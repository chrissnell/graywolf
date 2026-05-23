//go:build android

package platformsvc

import (
	"context"
	"fmt"
	"io"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

// BondedBtDevice is the Go-side view of an Android Bluetooth bond entry
// returned by Client.BondedBtDevices. MAC is the colon-separated uppercase
// address (e.g. "AA:BB:CC:DD:EE:FF"); Name is the user-visible label set at
// bond time and may be empty if the device never advertised one.
type BondedBtDevice struct {
	MAC  string
	Name string
}

// BondedBtDevices issues a one-shot request to the platform service for the
// current bonded-Bluetooth-device set.
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

// BtSerialOpen opens an RFCOMM SPP stream to the bonded device at mac and
// returns a multiplexed io.ReadWriteCloser. The handle multiplexes onto the
// shared UDS connection. Close tears down the RFCOMM socket server-side.
func (c *clientImpl) BtSerialOpen(ctx context.Context, mac string) (io.ReadWriteCloser, error) {
	return c.openSerialStream(ctx, func(handle uint32) *pb.SerialOpen {
		return &pb.SerialOpen{
			Handle:  handle,
			Kind:    pb.SerialKind_SERIAL_KIND_BLUETOOTH,
			Address: mac,
		}
	})
}
