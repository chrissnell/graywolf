package stationcache

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/aprs"
)

func TestExtractEntry_NilPacket(t *testing.T) {
	if got := ExtractEntry(nil, "modem", "RX", 0); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestExtractEntry_PositionPacket(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1ABC-9",
		Path:   []string{"WIDE1-1", "N0CALL*", "WIDE2-1"},
		Position: &aprs.Position{
			Latitude:  40.1234,
			Longitude: -105.5678,
			Altitude:  1609,
			HasAlt:    true,
			Speed:     55.0,
			Course:    270,
			HasCourse: true,
			Symbol:    aprs.Symbol{Table: '/', Code: '>'},
		},
		Comment:   "on the road",
		Timestamp: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	entries := ExtractEntry(pkt, "modem", "RX", 1)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	assertEqual(t, "Key", e.Key, "stn:W1ABC-9")
	assertEqual(t, "Callsign", e.Callsign, "W1ABC-9")
	assertBool(t, "IsObject", e.IsObject, false)
	assertBool(t, "HasPos", e.HasPos, true)
	assertFloat(t, "Lat", e.Lat, 40.1234)
	assertFloat(t, "Lon", e.Lon, -105.5678)
	assertFloat(t, "Alt", e.Alt, 1609)
	assertBool(t, "HasAlt", e.HasAlt, true)
	assertFloat(t, "Speed", e.Speed, 55.0)
	assertEqual(t, "Course", e.Course, 270)
	assertBool(t, "HasCourse", e.HasCourse, true)
	assertEqual(t, "Symbol", e.Symbol, [2]byte{'/', '>'})
	assertEqual(t, "Via", e.Via, "rf")
	assertEqual(t, "Hops", e.Hops, 1) // N0CALL* has H-bit
	assertEqual(t, "Direction", e.Direction, "RX")
	assertEqual(t, "Channel", e.Channel, uint32(1))
	assertEqual(t, "Comment", e.Comment, "on the road")
}

func TestExtractEntry_CourseZero(t *testing.T) {
	// Course=0 (due north) with HasCourse=true must not be dropped
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1XYZ",
		Position: &aprs.Position{
			Latitude:  40.0,
			Longitude: -105.0,
			Course:    0,
			HasCourse: true,
			Symbol:    aprs.Symbol{Table: '/', Code: '>'},
		},
	}
	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	assertBool(t, "HasCourse", entries[0].HasCourse, true)
	assertEqual(t, "Course", entries[0].Course, 0)
}

func TestExtractEntry_NoCourse(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1XYZ",
		Position: &aprs.Position{
			Latitude:  40.0,
			Longitude: -105.0,
			HasCourse: false,
			Symbol:    aprs.Symbol{Table: '/', Code: '>'},
		},
	}
	entries := ExtractEntry(pkt, "modem", "RX", 0)
	assertBool(t, "HasCourse", entries[0].HasCourse, false)
}

func TestExtractEntry_Object(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1ABC",
		Object: &aprs.Object{
			Name: "SHELTER1",
			Live: true,
			Position: &aprs.Position{
				Latitude:  40.0,
				Longitude: -105.0,
				Symbol:    aprs.Symbol{Table: '\\', Code: 'S'},
			},
			Comment: "Emergency shelter",
		},
		Timestamp: time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry (object only, no originator position), got %d", len(entries))
	}

	e := entries[0]
	assertEqual(t, "Key", e.Key, "obj:SHELTER1")
	assertEqual(t, "Callsign", e.Callsign, "SHELTER1")
	assertBool(t, "IsObject", e.IsObject, true)
	assertBool(t, "Killed", e.Killed, false)
	assertFloat(t, "Lat", e.Lat, 40.0)
	assertEqual(t, "Symbol", e.Symbol, [2]byte{'\\', 'S'})
}

func TestExtractEntry_ObjectKeyNotCollidesWithStation(t *testing.T) {
	// Object named "W1ABC-9" must not collide with station "W1ABC-9"
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1XYZ",
		Object: &aprs.Object{
			Name: "W1ABC-9",
			Live: true,
			Position: &aprs.Position{
				Latitude:  40.0,
				Longitude: -105.0,
				Symbol:    aprs.Symbol{Table: '/', Code: '-'},
			},
		},
	}

	entries := ExtractEntry(pkt, "modem", "RX", 0)
	assertEqual(t, "Key", entries[0].Key, "obj:W1ABC-9")
	// Station with same callsign would be "stn:W1ABC-9"
}

func TestExtractEntry_KilledObject(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1ABC",
		Object: &aprs.Object{
			Name: "SHELTER1",
			Live: false,
			Position: &aprs.Position{
				Latitude:  40.0,
				Longitude: -105.0,
				Symbol:    aprs.Symbol{Table: '\\', Code: 'S'},
			},
		},
	}

	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	assertBool(t, "Killed", entries[0].Killed, true)
}

func TestExtractEntry_KilledItem(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1ABC",
		Item: &aprs.Item{
			Name: "ITEM1",
			Live: false,
			Position: &aprs.Position{
				Latitude:  40.0,
				Longitude: -105.0,
				Symbol:    aprs.Symbol{Table: '/', Code: '-'},
			},
		},
	}

	entries := ExtractEntry(pkt, "modem", "RX", 0)
	assertBool(t, "Killed", entries[0].Killed, true)
	assertEqual(t, "Key", entries[0].Key, "obj:ITEM1")
}

func TestExtractEntry_ThirdPartyUnwrap(t *testing.T) {
	inner := &aprs.DecodedAPRSPacket{
		Source: "W2INNER",
		Path:   []string{"WIDE1*", "RELAY*"},
		Position: &aprs.Position{
			Latitude:  41.0,
			Longitude: -106.0,
			Symbol:    aprs.Symbol{Table: '/', Code: '>'},
		},
		Comment: "inner comment",
	}

	pkt := &aprs.DecodedAPRSPacket{
		Source:     "W1OUTER",
		ThirdParty: inner,
		Timestamp:  time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC),
	}

	entries := ExtractEntry(pkt, "igate-is", "IS", 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	e := entries[0]
	assertEqual(t, "Key", e.Key, "stn:W2INNER")
	assertEqual(t, "Callsign", e.Callsign, "W2INNER")
	assertFloat(t, "Lat", e.Lat, 41.0)
	assertEqual(t, "Via", e.Via, "is")
	assertEqual(t, "Hops", e.Hops, 2) // WIDE1* and RELAY*
	assertEqual(t, "Comment", e.Comment, "inner comment")
	// Inner packet has zero timestamp → should fall back to outer packet's timestamp
	outerTS := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)
	if !e.Timestamp.Equal(outerTS) {
		t.Errorf("Timestamp: got %v, want outer packet timestamp %v", e.Timestamp, outerTS)
	}
}

func TestExtractEntry_ThirdPartyTimestampFallback(t *testing.T) {
	// Inner has its own timestamp → use it, not the outer's
	innerTS := time.Date(2025, 5, 15, 8, 0, 0, 0, time.UTC)
	outerTS := time.Date(2025, 6, 1, 12, 0, 0, 0, time.UTC)

	inner := &aprs.DecodedAPRSPacket{
		Source:    "W2INNER",
		Position:  &aprs.Position{Latitude: 41.0, Longitude: -106.0, Symbol: aprs.Symbol{Table: '/', Code: '>'}},
		Timestamp: innerTS,
	}
	pkt := &aprs.DecodedAPRSPacket{
		Source:     "W1OUTER",
		ThirdParty: inner,
		Timestamp:  outerTS,
	}

	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if !entries[0].Timestamp.Equal(innerTS) {
		t.Errorf("should use inner timestamp when present: got %v, want %v", entries[0].Timestamp, innerTS)
	}
}

func TestExtractEntry_ViaDerivation(t *testing.T) {
	tests := []struct {
		source string
		want   string
	}{
		{"modem", "rf"},
		{"kiss", "rf"},
		{"agw", "rf"},
		{"digipeater", "rf"},
		{"beacon", "rf"},
		{"igate-is", "is"},
		{"igate", "rf"},
		{"unknown-source", "rf"},
	}

	for _, tt := range tests {
		pkt := &aprs.DecodedAPRSPacket{
			Source:   "W1TEST",
			Position: &aprs.Position{Latitude: 40.0, Longitude: -105.0, Symbol: aprs.Symbol{Table: '/', Code: '>'}},
		}
		entries := ExtractEntry(pkt, tt.source, "RX", 0)
		if entries[0].Via != tt.want {
			t.Errorf("source=%q: Via=%q, want %q", tt.source, entries[0].Via, tt.want)
		}
	}
}

func TestExtractEntry_HopCounting(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source:   "W1TEST",
		Path:     []string{"WIDE1-1", "N0CALL*", "WIDE2-1", "N1CALL*", "N2CALL*"},
		Position: &aprs.Position{Latitude: 40.0, Longitude: -105.0, Symbol: aprs.Symbol{Table: '/', Code: '>'}},
	}
	entries := ExtractEntry(pkt, "modem", "RX", 0)
	assertEqual(t, "Hops", entries[0].Hops, 3)
}

func TestExtractEntry_Weather(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source: "WX1ABC",
		Position: &aprs.Position{
			Latitude:  40.0,
			Longitude: -105.0,
			Symbol:    aprs.Symbol{Table: '/', Code: '_'},
		},
		Weather: &aprs.Weather{
			Temperature:   72.5,
			HasTemp:       true,
			WindSpeed:     15.0,
			HasWindSpeed:  true,
			WindDirection:  180,
			HasWindDir:    true,
			WindGust:      25.0,
			HasWindGust:   true,
			Humidity:      65,
			HasHumidity:   true,
			Pressure:      10132, // tenths of mbar
			HasPressure:   true,
			Rain1Hour:     5,
			HasRain1h:     true,
			Rain24Hour:    20,
			HasRain24h:    true,
			Snowfall24h:   0.5,
			HasSnow:       true,
			Luminosity:    800,
			HasLuminosity: true,
		},
	}

	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}

	wx := entries[0].Weather
	if wx == nil {
		t.Fatal("expected weather data")
	}
	assertFloat(t, "Temp", wx.Temp, 72.5)
	assertFloat(t, "WindSpeed", wx.WindSpeed, 15.0)
	assertEqual(t, "WindDir", wx.WindDir, 180)
	assertFloat(t, "Pressure", wx.Pressure, 1013.2)
	assertFloat(t, "Rain1h", wx.Rain1h, 5)
	assertFloat(t, "Snow24h", wx.Snow24h, 0.5)
	assertEqual(t, "Luminosity", wx.Luminosity, 800)
}

func TestExtractEntry_MessageOnly(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source:  "W1MSG",
		Message: &aprs.Message{Addressee: "W2OTHER", Text: "hello"},
	}
	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if entries != nil {
		t.Fatalf("message-only packet should return nil, got %d entries", len(entries))
	}
}

func TestExtractEntry_WeatherOnlyNoPosition(t *testing.T) {
	// Positionless weather report — still produces entry (for existing stations)
	pkt := &aprs.DecodedAPRSPacket{
		Source: "WX1ABC",
		Weather: &aprs.Weather{
			Temperature: 72.5,
			HasTemp:     true,
		},
	}
	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry for weather-only packet, got %d", len(entries))
	}
	assertBool(t, "HasPos", entries[0].HasPos, false)
	if entries[0].Weather == nil {
		t.Fatal("expected weather data")
	}
}

func TestExtractEntry_ObjectWithOriginatorPosition(t *testing.T) {
	// Object packet where originator also has a position → 2 entries
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1ABC",
		Position: &aprs.Position{
			Latitude:  39.0,
			Longitude: -104.0,
			Symbol:    aprs.Symbol{Table: '/', Code: '-'},
		},
		Object: &aprs.Object{
			Name: "SHELTER1",
			Live: true,
			Position: &aprs.Position{
				Latitude:  40.0,
				Longitude: -105.0,
				Symbol:    aprs.Symbol{Table: '\\', Code: 'S'},
			},
			Comment: "shelter",
		},
	}

	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries (object + originator), got %d", len(entries))
	}

	assertEqual(t, "First entry key", entries[0].Key, "obj:SHELTER1")
	assertEqual(t, "Second entry key", entries[1].Key, "stn:W1ABC")
	assertFloat(t, "Object lat", entries[0].Lat, 40.0)
	assertFloat(t, "Station lat", entries[1].Lat, 39.0)
}

func TestExtractEntry_Item(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source: "W1ABC",
		Item: &aprs.Item{
			Name: "ITEM1",
			Live: true,
			Position: &aprs.Position{
				Latitude:  40.0,
				Longitude: -105.0,
				Symbol:    aprs.Symbol{Table: '/', Code: '-'},
			},
			Comment: "item comment",
		},
	}

	entries := ExtractEntry(pkt, "modem", "RX", 0)
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	assertEqual(t, "Key", e.Key, "obj:ITEM1")
	assertBool(t, "IsObject", e.IsObject, true)
	assertEqual(t, "Comment", e.Comment, "item comment")
}

func TestExtractEntry_ZeroTimestampDefaultsToNow(t *testing.T) {
	pkt := &aprs.DecodedAPRSPacket{
		Source:   "W1TEST",
		Position: &aprs.Position{Latitude: 40.0, Longitude: -105.0, Symbol: aprs.Symbol{Table: '/', Code: '>'}},
		// Timestamp is zero
	}
	before := time.Now()
	entries := ExtractEntry(pkt, "modem", "RX", 0)
	after := time.Now()
	ts := entries[0].Timestamp
	if ts.Before(before) || ts.After(after) {
		t.Fatalf("timestamp %v not between %v and %v", ts, before, after)
	}
}

// --- helpers ---

func assertEqual[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertBool(t *testing.T, name string, got, want bool) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}

func assertFloat(t *testing.T, name string, got, want float64) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %f, want %f", name, got, want)
	}
}
