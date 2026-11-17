package filters

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func TestExplicitSourceAllow(t *testing.T) {
	position := func(source string) *aprs.DecodedAPRSPacket {
		return &aprs.DecodedAPRSPacket{Source: source, Type: aprs.PacketPosition}
	}
	cases := []struct {
		name string
		rules []Rule
		source string
		want bool
	}{
		{"empty", nil, "N6110L", false},
		{"exact allow", []Rule{{ID: 1, Type: TypeCallsign, Pattern: "N6110L", Action: Allow}}, "N6110L", true},
		{"case insensitive", []Rule{{ID: 1, Type: TypeCallsign, Pattern: "n6110l", Action: Allow}}, "N6110L", true},
		{"other source", []Rule{{ID: 1, Type: TypeCallsign, Pattern: "N6110L", Action: Allow}}, "OTHER", false},
		{"exact deny", []Rule{{ID: 1, Type: TypeCallsign, Pattern: "N6110L", Action: Deny}}, "N6110L", false},
		{"prefix cannot opt in", []Rule{{ID: 1, Type: TypePrefix, Pattern: "N6110L", Action: Allow}}, "N6110L", false},
		{"packet type cannot opt in", []Rule{{ID: 1, Type: TypePacketType, Pattern: "position", Action: Allow}}, "N6110L", false},
		{"deny before exact allow", []Rule{
			{ID: 1, Priority: 1, Type: TypePrefix, Pattern: "N", Action: Deny},
			{ID: 2, Priority: 2, Type: TypeCallsign, Pattern: "N6110L", Action: Allow},
		}, "N6110L", false},
		{"broad allow before exact allow", []Rule{
			{ID: 1, Priority: 1, Type: TypePrefix, Pattern: "N", Action: Allow},
			{ID: 2, Priority: 2, Type: TypeCallsign, Pattern: "N6110L", Action: Allow},
		}, "N6110L", false},
		{"exact allow before broad deny", []Rule{
			{ID: 1, Priority: 1, Type: TypeCallsign, Pattern: "N6110L", Action: Allow},
			{ID: 2, Priority: 2, Type: TypePrefix, Pattern: "N", Action: Deny},
		}, "N6110L", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := New(tc.rules).ExplicitSourceAllow(position(tc.source)); got != tc.want {
				t.Fatalf("ExplicitSourceAllow(%q) = %v, want %v", tc.source, got, tc.want)
			}
		})
	}
	if New([]Rule{{ID: 1, Type: TypeCallsign, Pattern: "N6110L", Action: Allow}}).ExplicitSourceAllow(nil) {
		t.Fatal("nil packet must deny")
	}
}
