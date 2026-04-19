package igate

import (
	"sync/atomic"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/igate/filters"
)

// TestHandleISLineSetsDirectionIS verifies that packets arriving via
// the APRS-IS read loop are tagged DirectionIS, which the messages
// router (Phase 2) uses to drive the Source badge and fallback policy.
func TestHandleISLineSetsDirectionIS(t *testing.T) {
	var got atomic.Value // aprs.Direction
	ig, err := New(Config{
		Server:   "127.0.0.1:1",
		Callsign: "N0CALL",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		IsRxHook: func(pkt *aprs.DecodedAPRSPacket, _ string) {
			if pkt != nil {
				got.Store(pkt.Direction)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ig.handleISLine("W5ABC>APRS,WIDE1-1:!3725.00N/12158.00W>hi")

	v := got.Load()
	if v == nil {
		t.Fatalf("IsRxHook never fired, cannot observe Direction")
	}
	if d := v.(aprs.Direction); d != aprs.DirectionIS {
		t.Fatalf("Direction = %q, want %q", d, aprs.DirectionIS)
	}
}

// TestHandleISLineSetsDirectionISMessage verifies the Direction tag
// survives a message-packet code path (not just position) because the
// messages router is the primary consumer of Direction and operates on
// PacketMessage.
func TestHandleISLineSetsDirectionISMessage(t *testing.T) {
	var got atomic.Value
	ig, err := New(Config{
		Server:   "127.0.0.1:1",
		Callsign: "N0CALL",
		Rules: []filters.Rule{
			{ID: 1, Type: filters.TypePrefix, Pattern: "W5", Action: filters.Allow},
		},
		IsRxHook: func(pkt *aprs.DecodedAPRSPacket, _ string) {
			if pkt != nil {
				got.Store(pkt.Direction)
			}
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ig.handleISLine("W5ABC>APRS,WIDE1-1::N0CALL   :hello{1")

	v := got.Load()
	if v == nil {
		t.Fatalf("IsRxHook never fired")
	}
	if d := v.(aprs.Direction); d != aprs.DirectionIS {
		t.Fatalf("Direction on message = %q, want %q", d, aprs.DirectionIS)
	}
}
