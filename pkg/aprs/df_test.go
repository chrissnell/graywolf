package aprs

import "testing"

func TestParseDirectionFindingAppended(t *testing.T) {
	// "/BRG/NRQ" appendix (APRS101 ch 7) on a position packet.
	info := []byte("!4903.50N/07201.75W\\/088/729DF station")
	pkt, err := ParseInfo(info)
	if err != nil {
		t.Fatal(err)
	}
	if pkt.Type != PacketPosition {
		t.Fatalf("type %q", pkt.Type)
	}
	if pkt.DF == nil {
		t.Fatal("expected DF appendix")
	}
	if pkt.DF.Bearing != 88 {
		t.Errorf("bearing %d", pkt.DF.Bearing)
	}
	if pkt.DF.Number != 7 {
		t.Errorf("number %d", pkt.DF.Number)
	}
	// R=2 → Range = 2^2 = 4 miles.
	if pkt.DF.Range != 4 {
		t.Errorf("range %d", pkt.DF.Range)
	}
	if pkt.DF.Quality != 9 {
		t.Errorf("quality %d", pkt.DF.Quality)
	}
}
