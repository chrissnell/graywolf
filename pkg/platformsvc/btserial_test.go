//go:build android

package platformsvc

import (
	"context"
	"testing"
	"time"

	pb "github.com/chrissnell/graywolf/pkg/platformproto"
)

func TestBondedBtDevices_returnsDevices(t *testing.T) {
	respond := func(req *pb.PlatformMessage) *pb.PlatformMessage {
		if req.GetBondedBtDevicesRequest() == nil {
			return nil
		}
		return &pb.PlatformMessage{Body: &pb.PlatformMessage_BondedBtDevicesResponse{
			BondedBtDevicesResponse: &pb.BondedBtDevicesResponse{
				Devices: []*pb.BondedBtDevicesResponse_Device{
					{Mac: "AA:BB:CC:DD:EE:FF", Name: "Mobilinkd TNC4"},
					{Mac: "11:22:33:44:55:66", Name: ""},
				},
			},
		}}
	}
	withFakeClient(t, respond, func(c *clientImpl) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		got, err := c.BondedBtDevices(ctx)
		if err != nil {
			t.Fatalf("BondedBtDevices: %v", err)
		}
		if len(got) != 2 {
			t.Fatalf("expected 2 devices, got %d", len(got))
		}
		if got[0].MAC != "AA:BB:CC:DD:EE:FF" || got[0].Name != "Mobilinkd TNC4" {
			t.Errorf("device[0] = %+v", got[0])
		}
		if got[1].MAC != "11:22:33:44:55:66" || got[1].Name != "" {
			t.Errorf("device[1] = %+v", got[1])
		}
	})
}
