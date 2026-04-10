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

func TestIsFixedPositionBeacon(t *testing.T) {
	stationary := &aprs.DecodedAPRSPacket{
		Type:     aprs.PacketPosition,
		Position: &aprs.Position{HasCourse: false},
	}
	moving := &aprs.DecodedAPRSPacket{
		Type:     aprs.PacketPosition,
		Position: &aprs.Position{HasCourse: true, Speed: 35},
	}
	if !isFixedPositionBeacon(stationary) {
		t.Error("stationary position must be a fixed beacon")
	}
	if isFixedPositionBeacon(moving) {
		t.Error("moving position (course+speed) is not a fixed beacon")
	}
	msg := &aprs.DecodedAPRSPacket{Type: aprs.PacketMessage}
	if isFixedPositionBeacon(msg) {
		t.Error("message is not a fixed beacon")
	}
}

func TestGateRFToISSkipsThirdParty(t *testing.T) {
	ig, err := New(Config{Server: "127.0.0.1:1", Callsign: "N0CALL"})
	if err != nil {
		t.Fatal(err)
	}
	// Simulate connected so the dropped-offline counter is not what
	// catches third-party packets.
	ig.mu.Lock()
	ig.connected = true
	ig.mu.Unlock()

	raw := buildRawFrame(t, "W5ABC-7", "APRS", nil, "}N0CALL>APRS,TCPIP*:test")
	pkt := &aprs.DecodedAPRSPacket{
		Source:     "W5ABC-7",
		Dest:       "APRS",
		Raw:        raw,
		ThirdParty: &aprs.DecodedAPRSPacket{Source: "N0CALL"},
	}
	ig.gateRFToIS(pkt)
	if ig.Status().Gated != 0 {
		t.Fatalf("third-party traffic must not be gated; Gated=%d", ig.Status().Gated)
	}
}

func TestGateRFToISDroppedWhenDisconnected(t *testing.T) {
	ig, err := New(Config{Server: "127.0.0.1:1", Callsign: "N0CALL"})
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
