package stationcache

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func TestBuildRxEvent(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	tests := []struct {
		name    string
		pkt     *aprs.DecodedAPRSPacket
		wantOK  bool
		wantKey string
		wantPos bool
		wantLat float64
		wantLon float64
		wantHop int
	}{
		{
			name: "direct with position -> origin + coords",
			pkt: &aprs.DecodedAPRSPacket{
				Source: "W1ABC", Path: []string{}, Timestamp: now,
				Position: &aprs.Position{Latitude: 35, Longitude: -95},
			},
			wantOK: true, wantKey: "stn:W1ABC", wantPos: true, wantLat: 35, wantLon: -95, wantHop: 0,
		},
		{
			name: "digipeated -> last real digi, no coords",
			pkt: &aprs.DecodedAPRSPacket{
				Source: "W2DEF", Path: []string{"N0DIGI*"}, Timestamp: now,
				Position: &aprs.Position{Latitude: 40, Longitude: -80},
			},
			wantOK: true, wantKey: "stn:N0DIGI", wantPos: false, wantHop: 1,
		},
		{
			name: "digipeated with trailing generic alias -> last real digi, not the alias",
			pkt: &aprs.DecodedAPRSPacket{
				Source: "W2DEF", Path: []string{"SHEPRD*", "WIDE1*"}, Timestamp: now,
				Position: &aprs.Position{Latitude: 40, Longitude: -80},
			},
			wantOK: true, wantKey: "stn:SHEPRD", wantPos: false, wantHop: 1,
		},
		{
			name: "gated (third-party) -> not counted",
			pkt: &aprs.DecodedAPRSPacket{
				Source: "W3GHI", Timestamp: now,
				ThirdParty: &aprs.DecodedAPRSPacket{Source: "INNER"},
			},
			wantOK: false,
		},
		{
			name:   "positionless direct (message) -> origin, resolved later",
			pkt:    &aprs.DecodedAPRSPacket{Source: "W4JKL", Path: []string{}, Timestamp: now},
			wantOK: true, wantKey: "stn:W4JKL", wantPos: false, wantHop: 0,
		},
		{
			name:   "empty source -> not counted",
			pkt:    &aprs.DecodedAPRSPacket{Source: "", Timestamp: now},
			wantOK: false,
		},
		{
			name:   "nil packet -> not counted",
			pkt:    nil,
			wantOK: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ev, ok := BuildRxEvent(tc.pkt)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if ev.AttrKey != tc.wantKey {
				t.Errorf("AttrKey = %q, want %q", ev.AttrKey, tc.wantKey)
			}
			if ev.HasPos != tc.wantPos {
				t.Errorf("HasPos = %v, want %v", ev.HasPos, tc.wantPos)
			}
			if tc.wantPos && (ev.Lat != tc.wantLat || ev.Lon != tc.wantLon) {
				t.Errorf("coords = (%v,%v), want (%v,%v)", ev.Lat, ev.Lon, tc.wantLat, tc.wantLon)
			}
			if ev.Hops != tc.wantHop {
				t.Errorf("Hops = %d, want %d", ev.Hops, tc.wantHop)
			}
		})
	}
}
