package filters

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func pkt(src string) *aprs.DecodedAPRSPacket {
	return &aprs.DecodedAPRSPacket{Source: src}
}

func TestDefaultDenyAll(t *testing.T) {
	e := New(nil)
	if e.Allow(pkt("N0CALL-1")) {
		t.Fatal("empty engine must deny all")
	}
}

func TestCallsignExact(t *testing.T) {
	e := New([]Rule{
		{ID: 1, Priority: 10, Type: TypeCallsign, Pattern: "N0CALL-1", Action: Allow},
	})
	if !e.Allow(pkt("N0CALL-1")) {
		t.Fatal("exact match should allow")
	}
	if e.Allow(pkt("N0CALL-2")) {
		t.Fatal("SSID mismatch should fall through to deny")
	}
	if e.Allow(pkt("N0CALL")) {
		t.Fatal("missing SSID should not match CALL-1")
	}
}

func TestPrefix(t *testing.T) {
	e := New([]Rule{
		{ID: 1, Priority: 10, Type: TypePrefix, Pattern: "W5", Action: Allow},
	})
	if !e.Allow(pkt("W5ABC-7")) {
		t.Fatal("prefix should match")
	}
	if e.Allow(pkt("N0CALL")) {
		t.Fatal("non-prefix should deny")
	}
}

func TestMessageDest(t *testing.T) {
	p := &aprs.DecodedAPRSPacket{Source: "X", Message: &aprs.Message{Addressee: "BLN1"}}
	e := New([]Rule{
		{ID: 1, Priority: 10, Type: TypeMessageDest, Pattern: "BLN1", Action: Allow},
	})
	if !e.Allow(p) {
		t.Fatal("message addressee should match")
	}
	p2 := &aprs.DecodedAPRSPacket{Source: "X"}
	if e.Allow(p2) {
		t.Fatal("non-message packet should not match message rule")
	}
}

func TestObjectAndItem(t *testing.T) {
	e := New([]Rule{
		{ID: 1, Priority: 10, Type: TypeObject, Pattern: "EOC", Action: Allow},
	})
	obj := &aprs.DecodedAPRSPacket{Source: "X", Object: &aprs.Object{Name: "EOC"}}
	item := &aprs.DecodedAPRSPacket{Source: "X", Item: &aprs.Item{Name: "EOC"}}
	if !e.Allow(obj) {
		t.Fatal("object name should match")
	}
	if !e.Allow(item) {
		t.Fatal("item name should match via object rule")
	}
}

func TestPriorityOrderingDenyWins(t *testing.T) {
	// Deny at priority 5 should beat allow at priority 10.
	e := New([]Rule{
		{ID: 2, Priority: 10, Type: TypePrefix, Pattern: "W", Action: Allow},
		{ID: 1, Priority: 5, Type: TypeCallsign, Pattern: "W5BAD-9", Action: Deny},
	})
	if e.Allow(pkt("W5BAD-9")) {
		t.Fatal("higher-priority deny must win")
	}
	if !e.Allow(pkt("W5GOOD-1")) {
		t.Fatal("lower-priority allow should still apply to non-denied sources")
	}
}

func TestPriorityOrderingAllowWins(t *testing.T) {
	// Allow at priority 5 should beat deny at priority 10.
	e := New([]Rule{
		{ID: 2, Priority: 10, Type: TypePrefix, Pattern: "W", Action: Deny},
		{ID: 1, Priority: 5, Type: TypeCallsign, Pattern: "W5VIP-3", Action: Allow},
	})
	if !e.Allow(pkt("W5VIP-3")) {
		t.Fatal("higher-priority allow must win")
	}
	if e.Allow(pkt("W5OTHER-1")) {
		t.Fatal("default deny from deny-rule should apply")
	}
}
