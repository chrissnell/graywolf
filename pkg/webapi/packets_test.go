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
// for every transmission type that carries a fix (plain position, Mic-E, object,
// item) regardless of the local station's own GPS, and omits them for
// positionless packets. This is what the web log's click-to-zoom reticle keys on.
func TestListPackets_ExposesCoordinates(t *testing.T) {
	srv, _ := newTestServer(t)

	log := packetlog.New(packetlog.Config{Capacity: 10})
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "POS>APRS:!pos",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketPosition, Source: "POS",
			Position: &aprs.Position{Latitude: 39.5, Longitude: -104.8}},
	})
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "MIC>APRS:mice",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketMicE, Source: "MIC",
			MicE: &aprs.MicE{Position: aprs.Position{Latitude: 35.1, Longitude: -90.0}}},
	})
	log.Record(packetlog.Entry{
		Direction: packetlog.DirRX, Display: "MSG>APRS::dest:hi",
		Decoded: &aprs.DecodedAPRSPacket{Type: aprs.PacketMessage, Source: "MSG"},
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

	if p := bySrc["POS"]; p.Lat == nil || p.Lon == nil || *p.Lat != 39.5 || *p.Lon != -104.8 {
		t.Errorf("position packet: expected (39.5,-104.8), got %v", p)
	}
	if p := bySrc["MIC"]; p.Lat == nil || p.Lon == nil || *p.Lat != 35.1 || *p.Lon != -90.0 {
		t.Errorf("mic-e packet: expected (35.1,-90.0), got %v", p)
	}
	if p := bySrc["MSG"]; p.Lat != nil || p.Lon != nil {
		t.Errorf("message packet: expected no coordinates, got lat=%v lon=%v", p.Lat, p.Lon)
	}
}
