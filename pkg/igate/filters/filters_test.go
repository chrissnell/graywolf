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

// msgPkt builds a message packet with the given addressee.
func msgPkt(addressee string) *aprs.DecodedAPRSPacket {
	return &aprs.DecodedAPRSPacket{Source: "X", Message: &aprs.Message{Addressee: addressee}}
}

// objPkt builds an object packet with the given object name.
func objPkt(name string) *aprs.DecodedAPRSPacket {
	return &aprs.DecodedAPRSPacket{Source: "X", Object: &aprs.Object{Name: name}}
}

// itemPkt builds an item packet with the given item name.
func itemPkt(name string) *aprs.DecodedAPRSPacket {
	return &aprs.DecodedAPRSPacket{Source: "X", Item: &aprs.Item{Name: name}}
}

func TestMessageDestWildcardSemantics(t *testing.T) {
	cases := []struct {
		name      string
		pattern   string
		addressee string
		want      bool
	}{
		// Trailing-SSID wildcard: NW5W-* requires an SSID suffix.
		{"NW5W-* matches NW5W-7", "NW5W-*", "NW5W-7", true},
		{"NW5W-* does not match NW5W (no SSID)", "NW5W-*", "NW5W", false},
		{"NW5W-* does not match K5XYZ-7", "NW5W-*", "K5XYZ-7", false},

		// Base wildcard: NW5W* matches base call and SSIDed variants.
		{"NW5W* matches NW5W", "NW5W*", "NW5W", true},
		{"NW5W* matches NW5W-10", "NW5W*", "NW5W-10", true},

		// Plain patterns remain exact-only (regression).
		{"NW5W exact does not match NW5W-7", "NW5W", "NW5W-7", false},
		{"NW5W exact matches NW5W", "NW5W", "NW5W", true},

		// Case-insensitivity for wildcard form.
		{"nw5w-* matches NW5W-7 (case-insensitive)", "nw5w-*", "NW5W-7", true},
		{"NW5W-* matches nw5w-7 (case-insensitive value)", "NW5W-*", "nw5w-7", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New([]Rule{
				{ID: 1, Priority: 10, Type: TypeMessageDest, Pattern: tc.pattern, Action: Allow},
			})
			got := e.Allow(msgPkt(tc.addressee))
			if got != tc.want {
				t.Fatalf("pattern=%q addressee=%q: got %v want %v", tc.pattern, tc.addressee, got, tc.want)
			}
		})
	}
}

func TestObjectWildcardSemantics(t *testing.T) {
	// Mirror the MessageDest cases against TypeObject, covering both
	// Object and Item carriers for the name field.
	cases := []struct {
		name    string
		pattern string
		objName string
		useItem bool
		want    bool
	}{
		{"NW5W-* matches NW5W-7 (object)", "NW5W-*", "NW5W-7", false, true},
		{"NW5W-* does not match NW5W (object)", "NW5W-*", "NW5W", false, false},
		{"NW5W-* does not match K5XYZ-7 (object)", "NW5W-*", "K5XYZ-7", false, false},
		{"NW5W* matches NW5W (object)", "NW5W*", "NW5W", false, true},
		{"NW5W* matches NW5W-10 (object)", "NW5W*", "NW5W-10", false, true},
		{"NW5W exact does not match NW5W-7 (object)", "NW5W", "NW5W-7", false, false},
		{"NW5W exact matches NW5W (object)", "NW5W", "NW5W", false, true},
		{"nw5w-* matches NW5W-7 (object, case-insensitive)", "nw5w-*", "NW5W-7", false, true},

		// Same cases should also work when the carrier is an Item.
		{"NW5W-* matches NW5W-7 (item)", "NW5W-*", "NW5W-7", true, true},
		{"NW5W* matches NW5W-10 (item)", "NW5W*", "NW5W-10", true, true},
		{"NW5W exact does not match NW5W-7 (item)", "NW5W", "NW5W-7", true, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New([]Rule{
				{ID: 1, Priority: 10, Type: TypeObject, Pattern: tc.pattern, Action: Allow},
			})
			var p *aprs.DecodedAPRSPacket
			if tc.useItem {
				p = itemPkt(tc.objName)
			} else {
				p = objPkt(tc.objName)
			}
			got := e.Allow(p)
			if got != tc.want {
				t.Fatalf("pattern=%q name=%q (item=%v): got %v want %v", tc.pattern, tc.objName, tc.useItem, got, tc.want)
			}
		})
	}
}

func TestMessageDestEmptyGuard(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
	}{
		{"empty pattern", ""},
		{"whitespace-only pattern", " "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New([]Rule{
				{ID: 1, Priority: 10, Type: TypeMessageDest, Pattern: tc.pattern, Action: Allow},
			})
			// Real addressees must never be gated by these no-op patterns.
			for _, addr := range []string{"BLN1", "NW5W-7", "ANYTHING", ""} {
				if e.Allow(msgPkt(addr)) {
					t.Fatalf("pattern %q must not match addressee %q", tc.pattern, addr)
				}
			}
		})
	}
}

// A bare `*` message_dest rule means "any addressee". Unlike the empty /
// whitespace guards above it MUST match every message packet — it's the
// one-rule expression of the textbook iGate "deliver to whoever we heard"
// behavior, safe because tier-1's heard-direct check bounds delivery.
func TestMessageDestBareStarMatchesAny(t *testing.T) {
	for _, pattern := range []string{"*", " * "} {
		e := New([]Rule{
			{ID: 1, Priority: 10, Type: TypeMessageDest, Pattern: pattern, Action: Allow},
		})
		for _, addr := range []string{"BLN1", "NW5W-7", "ANYTHING"} {
			if !e.Allow(msgPkt(addr)) {
				t.Fatalf("bare-star pattern %q must match addressee %q", pattern, addr)
			}
		}
	}
}

func TestObjectEmptyAndLoneStarGuard(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
	}{
		{"empty pattern", ""},
		{"whitespace-only pattern", " "},
		{"lone-star pattern", "*"},
		{"whitespace + star trims to lone-star", " *"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := New([]Rule{
				{ID: 1, Priority: 10, Type: TypeObject, Pattern: tc.pattern, Action: Allow},
			})
			for _, name := range []string{"EOC", "WX-001", "ANY", ""} {
				if e.Allow(objPkt(name)) {
					t.Fatalf("pattern %q must not match object %q", tc.pattern, name)
				}
				if e.Allow(itemPkt(name)) {
					t.Fatalf("pattern %q must not match item %q", tc.pattern, name)
				}
			}
		})
	}
}

func TestWildcardPriorityInteraction(t *testing.T) {
	// Allow BLN* at priority 40 wins over Deny B* at priority 50 for
	// addressees that match the narrower rule first. Addressees that
	// only match the broader rule fall through to it.
	e := New([]Rule{
		{ID: 1, Priority: 40, Type: TypeMessageDest, Pattern: "BLN*", Action: Allow},
		{ID: 2, Priority: 50, Type: TypeMessageDest, Pattern: "B*", Action: Deny},
	})
	if !e.Allow(msgPkt("BLN1")) {
		t.Fatal("BLN1: priority-40 Allow BLN* must win over priority-50 Deny B*")
	}
	if e.Allow(msgPkt("BX")) {
		t.Fatal("BX: priority-40 Allow BLN* doesn't match; priority-50 Deny B* must apply")
	}
	if e.Allow(msgPkt("OTHER")) {
		t.Fatal("OTHER: neither rule matches; default deny")
	}
}

func TestWildcardPriorityInteractionInverseOrdering(t *testing.T) {
	// Same logical rules but with Deny B* at priority 40 and
	// Allow BLN* at priority 50. First-match-wins means BLN1 now denies,
	// confirming priority drives order, not rule construction order.
	e := New([]Rule{
		{ID: 1, Priority: 50, Type: TypeMessageDest, Pattern: "BLN*", Action: Allow},
		{ID: 2, Priority: 40, Type: TypeMessageDest, Pattern: "B*", Action: Deny},
	})
	if e.Allow(msgPkt("BLN1")) {
		t.Fatal("BLN1: priority-40 Deny B* must win under this ordering")
	}
	if e.Allow(msgPkt("BX")) {
		t.Fatal("BX: priority-40 Deny B* must apply")
	}
	if e.Allow(msgPkt("OTHER")) {
		t.Fatal("OTHER: default deny")
	}
}

func TestCallsignStarIsLiteralNotWildcard(t *testing.T) {
	// Regression: '*' in a Callsign pattern must be treated literally,
	// not as a trailing wildcard. A literal '*' will never match a real
	// APRS source.
	e := New([]Rule{
		{ID: 1, Priority: 10, Type: TypeCallsign, Pattern: "NW5W*", Action: Allow},
	})
	if e.Allow(pkt("NW5W-7")) {
		t.Fatal("Callsign pattern NW5W* must not match NW5W-7 (no wildcard semantics)")
	}
	if e.Allow(pkt("NW5W")) {
		t.Fatal("Callsign pattern NW5W* must not match NW5W (literal '*' absent from source)")
	}
}

func TestPrefixStarIsLiteralNotWildcard(t *testing.T) {
	// Regression: '*' in a Prefix pattern is a literal. The source is
	// SSID-stripped to NW5W, which has no trailing '*' to match.
	e := New([]Rule{
		{ID: 1, Priority: 10, Type: TypePrefix, Pattern: "NW5W*", Action: Allow},
	})
	if e.Allow(pkt("NW5W-7")) {
		t.Fatal("Prefix pattern NW5W* must not match NW5W-7 (literal '*' on SSID-stripped source)")
	}
	if e.Allow(pkt("NW5W")) {
		t.Fatal("Prefix pattern NW5W* must not match NW5W (source has no literal '*')")
	}
}

// typedPkt builds a decoded packet carrying only a PacketType, which is
// all the packet_type rule inspects.
func typedPkt(t aprs.PacketType) *aprs.DecodedAPRSPacket {
	return &aprs.DecodedAPRSPacket{Source: "N0CALL-1", Type: t}
}

func TestPacketTypeMatch(t *testing.T) {
	// One allow rule per category, evaluated against every packet type.
	// want is true only when the packet's type is covered by the category.
	tests := []struct {
		name     string
		category string
		pktType  aprs.PacketType
		want     bool
	}{
		{"message_matches_message", "message", aprs.PacketMessage, true},
		{"message_rejects_position", "message", aprs.PacketPosition, false},
		{"position_matches_position", "position", aprs.PacketPosition, true},
		{"position_matches_mice", "position", aprs.PacketMicE, true},
		{"position_rejects_weather", "position", aprs.PacketWeather, false},
		{"weather_matches_weather", "weather", aprs.PacketWeather, true},
		{"object_matches_object", "object", aprs.PacketObject, true},
		{"object_rejects_item", "object", aprs.PacketItem, false},
		{"item_matches_item", "item", aprs.PacketItem, true},
		{"telemetry_matches_telemetry", "telemetry", aprs.PacketTelemetry, true},
		{"status_matches_status", "status", aprs.PacketStatus, true},
		{"query_matches_query", "query", aprs.PacketQuery, true},
		{"status_rejects_message", "status", aprs.PacketMessage, false},
		// Fail-safe: packet types with no category must never gate to RF.
		// This locks in default-deny for uncovered types so a future
		// taxonomy change can't silently start keying up the radio.
		{"message_rejects_thirdparty", "message", aprs.PacketThirdParty, false},
		{"message_rejects_capabilities", "message", aprs.PacketCapabilities, false},
		{"message_rejects_df", "message", aprs.PacketDF, false},
		{"message_rejects_unknown", "message", aprs.PacketUnknown, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			e := New([]Rule{
				{ID: 1, Priority: 10, Type: TypePacketType, Pattern: tc.category, Action: Allow},
			})
			if got := e.Allow(typedPkt(tc.pktType)); got != tc.want {
				t.Fatalf("packet_type=%q vs pkt=%q: Allow=%v want %v",
					tc.category, tc.pktType, got, tc.want)
			}
		})
	}
}

func TestPacketTypeCaseAndWhitespaceInsensitive(t *testing.T) {
	e := New([]Rule{
		{ID: 1, Priority: 10, Type: TypePacketType, Pattern: "  Message  ", Action: Allow},
	})
	if !e.Allow(typedPkt(aprs.PacketMessage)) {
		t.Fatal("packet_type category should match case- and whitespace-insensitively")
	}
}

func TestPacketTypeUnknownCategoryNeverMatches(t *testing.T) {
	e := New([]Rule{
		{ID: 1, Priority: 10, Type: TypePacketType, Pattern: "bogus", Action: Allow},
	})
	if e.Allow(typedPkt(aprs.PacketMessage)) {
		t.Fatal("unknown category must never match")
	}
}

// TestPacketTypeComposesWithOtherRules exercises the "messages-only" use
// case (GH #518): an allow rule for message packets, with a lower-priority
// deny catch-all, gates messages while denying everything else. It also
// confirms a higher-priority source deny still wins over the type allow.
func TestPacketTypeComposesWithOtherRules(t *testing.T) {
	e := New([]Rule{
		{ID: 1, Priority: 5, Type: TypeCallsign, Pattern: "BADACTR-1", Action: Deny},
		{ID: 2, Priority: 10, Type: TypePacketType, Pattern: "message", Action: Allow},
	})
	msg := &aprs.DecodedAPRSPacket{Source: "N0CALL-1", Type: aprs.PacketMessage}
	if !e.Allow(msg) {
		t.Fatal("message from ordinary source should be allowed by packet_type rule")
	}
	pos := &aprs.DecodedAPRSPacket{Source: "N0CALL-1", Type: aprs.PacketPosition}
	if e.Allow(pos) {
		t.Fatal("non-message should fall through to default deny")
	}
	badMsg := &aprs.DecodedAPRSPacket{Source: "BADACTR-1", Type: aprs.PacketMessage}
	if e.Allow(badMsg) {
		t.Fatal("higher-priority source deny must win over packet_type allow")
	}
}

func TestPacketTypeCategoryHelper(t *testing.T) {
	for _, ok := range []string{"message", "POSITION", "  weather  ", "query"} {
		if !PacketTypeCategory(ok) {
			t.Errorf("PacketTypeCategory(%q) = false, want true", ok)
		}
	}
	for _, bad := range []string{"", "bogus", "mic-e", "unknown"} {
		if PacketTypeCategory(bad) {
			t.Errorf("PacketTypeCategory(%q) = true, want false", bad)
		}
	}
}
