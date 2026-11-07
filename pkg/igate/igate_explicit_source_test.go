package igate

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
)

func TestNonMessageExplicitSourceGate(t *testing.T) {
	ig := &Igate{stationCallsign: "KS9N-10"}
	ig.filter.Store(filters.New([]filters.Rule{
		{ID: 1, Priority: 10, Type: filters.TypeCallsign, Pattern: "N6110L", Action: filters.Allow},
		{ID: 2, Priority: 20, Type: filters.TypeCallsign, Pattern: "KS9N-7", Action: filters.Allow},
		{ID: 3, Priority: 30, Type: filters.TypeCallsign, Pattern: "KS9N-10", Action: filters.Allow},
	}))
	cases := []struct {
		name string
		source string
		path []string
		want bool
	}{
		{"explicit aircraft", "N6110L", nil, true},
		{"case insensitive aircraft", "n6110l", nil, true},
		{"existing own SSID", "KS9N-7", nil, true},
		{"own base callsign", "KS9N", nil, true},
		{"never transmit self", "KS9N-10", nil, false},
		{"unknown denied", "N12345", nil, false},
		{"loop prevention", "N6110L", []string{"WIDE1-1", "KS9N-2*"}, false},
		{"empty source", "", nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			pkt := &aprs.DecodedAPRSPacket{
				Source: tc.source,
				Type: aprs.PacketPosition,
				Path: tc.path,
			}
			if got := ig.shouldForwardISToRF(pkt); got != tc.want {
				t.Fatalf("shouldForwardISToRF(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
	if ig.shouldForwardISToRF(nil) {
		t.Fatal("nil packet must deny")
	}
}

func TestNonMessageBroadAllowCannotBypassOwnership(t *testing.T) {
	ig := &Igate{stationCallsign: "KS9N-10"}
	ig.filter.Store(filters.New([]filters.Rule{
		{ID: 1, Priority: 1, Type: filters.TypePacketType, Pattern: "position", Action: filters.Allow},
		{ID: 2, Priority: 2, Type: filters.TypeCallsign, Pattern: "N6110L", Action: filters.Allow},
	}))
	pkt := &aprs.DecodedAPRSPacket{Source: "N6110L", Type: aprs.PacketPosition}
	if ig.shouldForwardISToRF(pkt) {
		t.Fatal("earlier broad allow must not bypass non-message ownership")
	}
}
