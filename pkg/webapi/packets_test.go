package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
