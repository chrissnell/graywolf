package aprs

import "testing"

func TestParseMessage(t *testing.T) {
	info := []byte(":W1AW     :Hello, World{42")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketMessage || pkt.Message == nil {
		t.Fatalf("type %q", pkt.Type)
	}
	if pkt.Message.Addressee != "W1AW" {
		t.Errorf("addr %q", pkt.Message.Addressee)
	}
	if pkt.Message.Text != "Hello, World" {
		t.Errorf("text %q", pkt.Message.Text)
	}
	if pkt.Message.MessageID != "42" {
		t.Errorf("id %q", pkt.Message.MessageID)
	}
}

func TestParseMessageAck(t *testing.T) {
	info := []byte(":W1AW     :ack42")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if !pkt.Message.IsAck || pkt.Message.MessageID != "42" {
		t.Errorf("ack parse: %+v", pkt.Message)
	}
}

func TestParseBulletin(t *testing.T) {
	info := []byte(":BLN1     :Net tonight at 2000z")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if !pkt.Message.IsBulletin {
		t.Errorf("bulletin flag not set")
	}
}

func TestParseTelemetryMetaPARM(t *testing.T) {
	info := []byte(":N0CALL   :PARM.Volt,Amp,AirTemp,Hum,Pres")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.TelemetryMeta == nil || pkt.TelemetryMeta.Kind != "parm" {
		t.Fatalf("meta %+v", pkt.TelemetryMeta)
	}
	if pkt.TelemetryMeta.Parm[0] != "Volt" || pkt.TelemetryMeta.Parm[4] != "Pres" {
		t.Errorf("parm %+v", pkt.TelemetryMeta.Parm)
	}
	if pkt.Type != PacketTelemetry {
		t.Errorf("type %q", pkt.Type)
	}
}

func TestParseTelemetryMetaUNIT(t *testing.T) {
	info := []byte(":N0CALL   :UNIT.V,A,F,%,mbar")
	pkt, _ := ParseInfo(info)
	if pkt.TelemetryMeta == nil || pkt.TelemetryMeta.Kind != "unit" {
		t.Fatalf("meta %+v", pkt.TelemetryMeta)
	}
	if pkt.TelemetryMeta.Unit[0] != "V" || pkt.TelemetryMeta.Unit[4] != "mbar" {
		t.Errorf("unit %+v", pkt.TelemetryMeta.Unit)
	}
}

func TestParseTelemetryMetaEQNS(t *testing.T) {
	info := []byte(":N0CALL   :EQNS.0,5.2,0,0,0.1,-30,0,1,0,0,1,0,0,1,0")
	pkt, _ := ParseInfo(info)
	if pkt.TelemetryMeta == nil || pkt.TelemetryMeta.Kind != "eqns" {
		t.Fatalf("meta %+v", pkt.TelemetryMeta)
	}
	if pkt.TelemetryMeta.Eqns[0][1] != 5.2 || pkt.TelemetryMeta.Eqns[1][2] != -30 {
		t.Errorf("eqns %+v", pkt.TelemetryMeta.Eqns)
	}
}

func TestParseTelemetryMetaBITS(t *testing.T) {
	info := []byte(":N0CALL   :BITS.10110001,Balloon Project")
	pkt, _ := ParseInfo(info)
	if pkt.TelemetryMeta == nil || pkt.TelemetryMeta.Kind != "bits" {
		t.Fatalf("meta %+v", pkt.TelemetryMeta)
	}
	if pkt.TelemetryMeta.Bits != 0xB1 {
		t.Errorf("bits %#x", pkt.TelemetryMeta.Bits)
	}
	if pkt.TelemetryMeta.ProjectName != "Balloon Project" {
		t.Errorf("project %q", pkt.TelemetryMeta.ProjectName)
	}
}

// Some iGates (aprx, older javAPRSSrvr) strip trailing whitespace from
// the APRS-IS line, leaving an addressee field shorter than the 9 chars
// APRS101 §14.1 requires. Accept these so the Messages inbox isn't
// starved of legitimate traffic that made it through such a relay.
func TestParseMessageShortAddressee(t *testing.T) {
	// ":NW5W-5 :testing 123{004" — seven bytes of addressee padding
	// (six call chars + one space) as observed on a real gated packet.
	info := []byte(":NW5W-5 :testing 123{004")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if pkt.Message == nil {
		t.Fatalf("nil Message; type=%q", pkt.Type)
	}
	if pkt.Message.Addressee != "NW5W-5" {
		t.Errorf("addressee %q, want %q", pkt.Message.Addressee, "NW5W-5")
	}
	if pkt.Message.Text != "testing 123" {
		t.Errorf("text %q", pkt.Message.Text)
	}
	if pkt.Message.MessageID != "004" {
		t.Errorf("id %q", pkt.Message.MessageID)
	}
}

func TestParseMessageNoPadding(t *testing.T) {
	// 9-char addressee with zero trailing space (exactly at the width
	// limit): ":NOCALL-15:hi".
	info := []byte(":NOCALL-15:hi")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if pkt.Message == nil || pkt.Message.Addressee != "NOCALL-15" || pkt.Message.Text != "hi" {
		t.Errorf("got %+v", pkt.Message)
	}
}

func TestParseMessageMalformedNoSeparator(t *testing.T) {
	// No closing ':' within the addressee window — must still reject.
	info := []byte(":NOCALL-15XY")
	pkt, err := ParseInfo(info)
	if err == nil {
		t.Fatalf("expected error, got %+v", pkt.Message)
	}
}

func TestEncodeMessageRoundTrip(t *testing.T) {
	b, err := EncodeMessage("W1AW", "hi", "007")
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := ParseInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Message.Addressee != "W1AW" || pkt.Message.Text != "hi" || pkt.Message.MessageID != "007" {
		t.Errorf("round-trip: %+v", pkt.Message)
	}
}
