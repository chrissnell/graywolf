package igate

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

// Source exclusions are configured by the operator; no station is
// implicitly excluded by its callsign in an empty/new installation.
func TestRFToISBlocklistDefaultsEmpty(t *testing.T) {
	ig := mustIgate(t)
	pkt := &aprs.DecodedAPRSPacket{
		Source: "K1ABC",
		Dest:   "APRS",
		Type:   aprs.PacketPosition,
		Raw:    buildRawFrame(t, "K1ABC", "APRS", nil, "!5046.57NS00604.17E#normal"),
	}
	if got := ig.gateRFToIS(pkt); got != GateReasonOffline {
		t.Fatalf("empty list blocked ordinary traffic: %q", got)
	}
	if !ig.heard.HeardWithin("K1ABC", 30*time.Minute) {
		t.Fatal("station not recorded with empty blocklist")
	}
}

func TestRFToISBlocklistLiveReconfiguration(t *testing.T) {
	ig := mustIgate(t)
	ig.SetRFToISBlocklist("K1ABC\nW5XYZ-7")
	check := func(call string, want GateReason) {
		t.Helper()
		pkt := &aprs.DecodedAPRSPacket{
			Source: call,
			Dest:   "APRS",
			Type:   aprs.PacketPosition,
			Raw:    buildRawFrame(t, "K1ABC", "APRS", nil, "!5046.57NS00604.17E#normal"),
		}
		if got := ig.gateRFToIS(pkt); got != want {
			t.Errorf("%s: gate reason = %q, want %q", call, got, want)
		}
	}
	check("K1ABC", GateReasonSourceBlock)
	check("K1ABC-2", GateReasonSourceBlock)
	check("W5XYZ-7", GateReasonSourceBlock)
	check("W5XYZ-9", GateReasonOffline)
	check("ON4MAX-2", GateReasonOffline)

	if !ig.heard.HeardWithin("K1ABC", 30*time.Minute) {
		t.Error("uplink exclusion prevented independent heard-direct tracking")
	}
	if !ig.heard.HeardWithin("ON4MAX-2", 30*time.Minute) {
		t.Error("ordinary station was not tracked")
	}
	ig.SetRFToISBlocklist("")
	check("K1ABC", GateReasonOffline)
}
