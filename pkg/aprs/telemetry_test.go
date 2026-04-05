package aprs

import "testing"

func TestParseTelemetryUncompressed(t *testing.T) {
	info := []byte("T#123,100,200,300,400,500,10110000Hello")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketTelemetry || pkt.Telemetry == nil {
		t.Fatalf("type %q", pkt.Type)
	}
	if pkt.Telemetry.Seq != 123 {
		t.Errorf("seq %d", pkt.Telemetry.Seq)
	}
	if pkt.Telemetry.Analog[0] != 100 || pkt.Telemetry.Analog[4] != 500 {
		t.Errorf("analog %+v", pkt.Telemetry.Analog)
	}
	// bits: 10110000 → 0xB0
	if pkt.Telemetry.Digital != 0xB0 {
		t.Errorf("digital %#x", pkt.Telemetry.Digital)
	}
	if pkt.Telemetry.Comment != "Hello" {
		t.Errorf("comment %q", pkt.Telemetry.Comment)
	}
}

func TestParseTelemetrySeqZero(t *testing.T) {
	// "T#000,..." must parse Seq as 0 (not -1).
	info := []byte("T#000,1,2,3,4,5,00000000")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Telemetry == nil || pkt.Telemetry.Seq != 0 {
		t.Errorf("seq %+v", pkt.Telemetry)
	}
}

func TestEncodeTelemetryRoundTrip(t *testing.T) {
	tm := Telemetry{
		Seq:     42,
		Analog:  [5]float64{10, 20, 30, 40, 50},
		Digital: 0xA5,
	}
	b, err := EncodeTelemetry(tm)
	if err != nil {
		t.Fatal(err)
	}
	pkt, err := ParseInfo(b)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Telemetry.Seq != 42 || pkt.Telemetry.Digital != 0xA5 {
		t.Errorf("round-trip: %+v", pkt.Telemetry)
	}
}
