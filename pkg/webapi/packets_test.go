package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/packetlog"
)

// TestListPackets_ResolvesChannelName verifies that /api/packets enriches
// each entry with the configured channel's display name, falling back to no
// name (the frontend then shows the raw ID) for IDs that map to no channel.
func TestListPackets_ResolvesChannelName(t *testing.T) {
	srv, _ := newTestServer(t)

	// newTestServer seeds one channel named "rx0"; grab its ID.
	chs, err := srv.store.ListChannels(context.Background())
	if err != nil || len(chs) == 0 {
		t.Fatalf("ListChannels: %v (n=%d)", err, len(chs))
	}
	seeded := chs[0]

	log := packetlog.New(packetlog.Config{Capacity: 10})
	log.Record(packetlog.Entry{Channel: seeded.ID, Direction: packetlog.DirRX, Display: "A>B:hi"})
	log.Record(packetlog.Entry{Channel: 0, Direction: packetlog.DirRX, Source: "igate-is", Display: "C>D:is"})

	mux := http.NewServeMux()
	RegisterPackets(srv, mux, log, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/packets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []packetDTO
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 packets, got %d", len(got))
	}

	byChannel := map[uint32]packetDTO{}
	for _, p := range got {
		byChannel[p.Channel] = p
	}
	if name := byChannel[seeded.ID].ChannelName; name != seeded.Name {
		t.Errorf("channel %d: expected name %q, got %q", seeded.ID, seeded.Name, name)
	}
	if name := byChannel[0].ChannelName; name != "" {
		t.Errorf("channel 0 maps to no channel; expected empty name, got %q", name)
	}
}

// TestListPackets_ExposesCoordinates verifies that /api/packets surfaces Lat/Lon
// for every transmission type that carries a fix -- plain position, Mic-E,
// weather-with-position, object, and item -- regardless of the local station's
// own GPS, and omits them for positionless packets and the 0/0 "null island"
// non-fix. This is what the web log's click-to-zoom reticle keys on.
func TestListPackets_ExposesCoordinates(t *testing.T) {
	srv, _ := newTestServer(t)

	log := packetlog.New(packetlog.Config{Capacity: 10})
	// Plain position report.
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "POS>APRS:!pos",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketPosition, Source: "POS",
			Position: &aprs.Position{Latitude: 39.5, Longitude: -104.8}},
	})
	// Weather report carrying a fix: the position rides on the top-level
	// Position field, exercising the primary (d.Position) branch the same way
	// real decoded Mic-E/weather packets do.
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "WX>APRS:@wx",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketWeather, Source: "WX",
			Position: &aprs.Position{Latitude: 47.6, Longitude: -122.3},
			Weather:  &aprs.Weather{HasTemp: true, Temperature: 55}},
	})
	// Mic-E with only the MicE.Position populated: exercises the defensive
	// fallback branch in packetPosition (real decodes also set d.Position).
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "MIC>APRS:mice",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketMicE, Source: "MIC",
			MicE: &aprs.MicE{Position: aprs.Position{Latitude: 35.1, Longitude: -90.0}}},
	})
	// Object with an embedded position.
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "OBJ>APRS:;obj",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketObject, Source: "OBJ",
			Object: &aprs.Object{Name: "EVENT", Live: true,
				Position: &aprs.Position{Latitude: 30.0, Longitude: -97.0}}},
	})
	// Item with an embedded position.
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "ITM>APRS:)item",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketItem, Source: "ITM",
			Item: &aprs.Item{Name: "AID", Live: true,
				Position: &aprs.Position{Latitude: 41.9, Longitude: -87.6}}},
	})
	// Positionless message: must carry no coordinates.
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "MSG>APRS::dest:hi",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketMessage, Source: "MSG"},
	})
	// Null island (0,0): an unset Position struct must NOT masquerade as a fix.
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "NUL>APRS:!null",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketPosition, Source: "NUL",
			Position: &aprs.Position{Latitude: 0, Longitude: 0}},
	})

	mux := http.NewServeMux()
	// Nil posCache: the local station has no fix, yet coordinates must still
	// be present (only DistanceMi depends on the local position).
	RegisterPackets(srv, mux, log, nil)

	req := httptest.NewRequest(http.MethodGet, "/api/packets", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var got []packetDTO
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	bySrc := map[string]packetDTO{}
	for _, p := range got {
		if p.Decoded != nil {
			bySrc[p.Decoded.Source] = p
		}
	}

	// Positioned types: coordinates present and exact.
	for _, tc := range []struct {
		src              string
		wantLat, wantLon float64
	}{
		{"POS", 39.5, -104.8},
		{"WX", 47.6, -122.3},
		{"MIC", 35.1, -90.0},
		{"OBJ", 30.0, -97.0},
		{"ITM", 41.9, -87.6},
	} {
		p := bySrc[tc.src]
		if p.Lat == nil || p.Lon == nil {
			t.Errorf("%s: expected coordinates (%v,%v), got nil", tc.src, tc.wantLat, tc.wantLon)
			continue
		}
		if *p.Lat != tc.wantLat || *p.Lon != tc.wantLon {
			t.Errorf("%s: expected (%v,%v), got (%v,%v)", tc.src, tc.wantLat, tc.wantLon, *p.Lat, *p.Lon)
		}
	}

	// Positionless / null-island: coordinates omitted.
	for _, src := range []string{"MSG", "NUL"} {
		if p := bySrc[src]; p.Lat != nil || p.Lon != nil {
			t.Errorf("%s: expected no coordinates, got lat=%v lon=%v", src, p.Lat, p.Lon)
		}
	}
}
