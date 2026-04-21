package igate

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func TestPathBlocksGating(t *testing.T) {
	cases := []struct {
		path []string
		want bool
	}{
		{[]string{"WIDE1-1", "WIDE2-2"}, false},
		{[]string{"TCPIP*"}, true},
		{[]string{"TCPXX*"}, true},
		{[]string{"WIDE1-1", "NOGATE"}, true},
		{[]string{"RFONLY"}, true},
	}
	for _, c := range cases {
		if got := pathBlocksGating(c.path); got != c.want {
			t.Errorf("pathBlocksGating(%v) = %v, want %v", c.path, got, c.want)
		}
	}
}

func TestGateRFToISSkipsThirdParty(t *testing.T) {
	ig, err := New(Config{Server: "127.0.0.1:1", StationCallsign: "KE7XYZ"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate connected so the dropped-offline counter is not what
	// catches third-party packets.
	ig.mu.Lock()
	ig.connected = true
	ig.mu.Unlock()

	raw := buildRawFrame(t, "W5ABC-7", "APRS", nil, "}KE7XYZ>APRS,TCPIP*:test")
	pkt := &aprs.DecodedAPRSPacket{
		Source:     "W5ABC-7",
		Dest:       "APRS",
		Raw:        raw,
		ThirdParty: &aprs.DecodedAPRSPacket{Source: "KE7XYZ"},
	}
	ig.gateRFToIS(pkt)
	if ig.Status().Gated != 0 {
		t.Fatalf("third-party traffic must not be gated; Gated=%d", ig.Status().Gated)
	}
}

func TestGateRFToISDroppedWhenDisconnected(t *testing.T) {
	ig, err := New(Config{Server: "127.0.0.1:1", StationCallsign: "KE7XYZ"})
	if err != nil {
		t.Fatal(err)
	}
	raw := buildRawFrame(t, "W5ABC-7", "APRS", nil, "!3725.00N/12158.00W>hi")
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W5ABC-7",
		Dest:   "APRS",
		Raw:    raw,
		Type:   aprs.PacketPosition,
	}
	ig.gateRFToIS(pkt)
	if ig.Status().DroppedOffline != 1 {
		t.Fatalf("expected DroppedOffline=1 when disconnected, got %d", ig.Status().DroppedOffline)
	}
}
