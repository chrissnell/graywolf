package weather

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func TestParseNWSMessage_Warning(t *testing.T) {
	// Sample from the spec:
	// PBZFFW>APRS,qAO,AE5PL-WX::NWS-WARN :111530z,FLASH_FLOOD,OHC019,OHC029,OHC157{d20AA
	pkt := &aprs.DecodedAPRSPacket{
		Source: "PBZFFW",
		Type:   aprs.PacketMessage,
		Message: &aprs.Message{
			IsNWS:     true,
			Addressee: "NWS-WARN",
			Text:      "111530z,FLASH_FLOOD,OHC019,OHC029,OHC157",
			MessageID: "d20AA",
		},
	}
	alert, ok := ParseNWSMessage(pkt)
	if !ok {
		t.Fatal("ParseNWSMessage: expected ok=true")
	}
	if alert.AlertStatus != AlertStatusWarning {
		t.Errorf("AlertStatus = %q, want %q", alert.AlertStatus, AlertStatusWarning)
	}
	if alert.AlertType != "FLASH_FLOOD" {
		t.Errorf("AlertType = %q, want %q", alert.AlertType, "FLASH_FLOOD")
	}
	want := []string{"OHC019", "OHC029", "OHC157"}
	if len(alert.NWSCodes) != len(want) {
		t.Fatalf("NWSCodes = %v, want %v", alert.NWSCodes, want)
	}
	for i, w := range want {
		if alert.NWSCodes[i] != w {
			t.Errorf("NWSCodes[%d] = %q, want %q", i, alert.NWSCodes[i], w)
		}
	}
}

func TestParseNWSMessage_Watch(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Type: aprs.PacketMessage,
		Message: &aprs.Message{
			IsNWS:     true,
			Addressee: "NWS-WTCH",
			Text:      "120000z,TORNADO_WTCH,OHC001,OHC002",
		},
	}
	alert, ok := ParseNWSMessage(pkt)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if alert.AlertStatus != AlertStatusWatch {
		t.Errorf("AlertStatus = %q, want %q", alert.AlertStatus, AlertStatusWatch)
	}
}

func TestParseNWSMessage_NilPacket(t *testing.T) {
	_, ok := ParseNWSMessage(nil)
	if ok {
		t.Fatal("expected ok=false for nil packet")
	}
}

func TestParseNWSMessage_NonNWS(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Type: aprs.PacketMessage,
		Message: &aprs.Message{
			IsNWS:     false,
			Addressee: "W1ABC",
			Text:      "hello",
		},
	}
	_, ok := ParseNWSMessage(pkt)
	if ok {
		t.Fatal("expected ok=false for non-NWS message")
	}
}

func TestParseNWSMessage_NoCountyCodes(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Type: aprs.PacketMessage,
		Message: &aprs.Message{
			IsNWS:     true,
			Addressee: "NWS-WARN",
			Text:      "120000z,FLASH_FLOOD",
		},
	}
	_, ok := ParseNWSMessage(pkt)
	if ok {
		t.Fatal("expected ok=false when no county codes present")
	}
}

func TestAlertStatusFromAddressee(t *testing.T) {
	cases := []struct {
		addr string
		want string
	}{
		{"NWS-WARN", AlertStatusWarning},
		{"NWS-WTCH", AlertStatusWatch},
		{"NWS-OTHER", AlertStatusClear},
		{"", AlertStatusClear},
		{"nws-warn", AlertStatusWarning}, // case-insensitive
	}
	for _, tc := range cases {
		got := AlertStatusFromAddressee(tc.addr)
		if got != tc.want {
			t.Errorf("AlertStatusFromAddressee(%q) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}

func TestNWSCodeFromFIPS(t *testing.T) {
	cases := []struct {
		state, fips, want string
	}{
		{"OH", "39019", "OHC019"},
		{"CA", "06001", "CAC001"},
		{"", "39019", ""},     // empty state
		{"OH", "123", ""},     // short FIPS
	}
	for _, tc := range cases {
		got := NWSCodeFromFIPS(tc.state, tc.fips)
		if got != tc.want {
			t.Errorf("NWSCodeFromFIPS(%q, %q) = %q, want %q", tc.state, tc.fips, got, tc.want)
		}
	}
}

func TestIsNWSWFOCallsign(t *testing.T) {
	cases := []struct {
		src  string
		want bool
	}{
		{"PBZSVR", true},   // PBZ = Pittsburgh WFO
		{"LOT123", true},   // LOT = Chicago WFO
		{"W1ABC", false},
		{"PBZSVR1", true},  // still matches PBZ prefix
		{"XYZ", false},     // not a known WFO
		{"", false},
	}
	for _, tc := range cases {
		got := IsNWSWFOCallsign(tc.src)
		if got != tc.want {
			t.Errorf("IsNWSWFOCallsign(%q) = %v, want %v", tc.src, got, tc.want)
		}
	}
}
