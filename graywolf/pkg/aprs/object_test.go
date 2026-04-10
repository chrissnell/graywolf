package aprs

import "testing"

func TestParseObject(t *testing.T) {
	// ;NAME     *092345z4903.50N/07201.75W>Test
	info := []byte(";LEADER   *092345z4903.50N/07201.75W>Test object")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketObject || pkt.Object == nil {
		t.Fatalf("type %q", pkt.Type)
	}
	if pkt.Object.Name != "LEADER" {
		t.Errorf("name %q", pkt.Object.Name)
	}
	if !pkt.Object.Live {
		t.Errorf("expected live")
	}
	if pkt.Object.Position == nil {
		t.Fatal("missing position")
	}
}

func TestParseItem(t *testing.T) {
	info := []byte(")AID#2!4903.50N/07201.75W-aid station")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketItem || pkt.Item == nil {
		t.Fatalf("type %q", pkt.Type)
	}
	if pkt.Item.Name != "AID#2" {
		t.Errorf("name %q", pkt.Item.Name)
	}
	if !pkt.Item.Live {
		t.Errorf("expected live")
	}
}

func TestParseItemRejectsShortName(t *testing.T) {
	// APRS101 ch 14 specifies item names as 3..9 chars. "AB" (2 chars)
	// must be rejected.
	info := []byte(")AB!4903.50N/07201.75W-short name")
	pkt, _ := ParseInfo(info)
	if pkt.Type == PacketItem && pkt.Item != nil && pkt.Item.Name == "AB" {
		t.Errorf("2-char item name should be rejected, got %+v", pkt.Item)
	}
}

func TestParseThirdPartyRecursive(t *testing.T) {
	info := []byte("}N0CALL>APRS,TCPIP*,qAC,T2USA:=4903.50N/07201.75W-iGate relay")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketThirdParty {
		t.Fatalf("type %q", pkt.Type)
	}
	if pkt.ThirdParty == nil {
		t.Fatal("no nested packet")
	}
	if pkt.ThirdParty.Source != "N0CALL" {
		t.Errorf("inner src %q", pkt.ThirdParty.Source)
	}
	if pkt.ThirdParty.Type != PacketPosition || pkt.ThirdParty.Position == nil {
		t.Errorf("inner type %q", pkt.ThirdParty.Type)
	}
}

func TestParseStatusWithTimestamp(t *testing.T) {
	info := []byte(">092345zNet Control Active")
	pkt, _ := ParseInfo(info)
	if pkt.Type != PacketStatus {
		t.Fatalf("type %q", pkt.Type)
	}
	if pkt.Status != "Net Control Active" {
		t.Errorf("status %q", pkt.Status)
	}
}

func TestParseCapabilities(t *testing.T) {
	info := []byte("<IGATE,MSG_CNT=0,LOC_CNT=12>")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketCapabilities || pkt.Caps == nil {
		t.Fatalf("type %q", pkt.Type)
	}
	if _, ok := pkt.Caps.Entries["IGATE"]; !ok {
		t.Errorf("missing IGATE entry")
	}
	if pkt.Caps.Entries["LOC_CNT"] != "12" {
		t.Errorf("LOC_CNT %q", pkt.Caps.Entries["LOC_CNT"])
	}
}
