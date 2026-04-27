package webapi

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/stationcache"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// fakeStationCache implements stationcache.StationStore for autocomplete
// tests. QueryBBox returns the full station slice regardless of bbox.
type fakeStationCache struct {
	stations []stationcache.Station
}

func (f *fakeStationCache) QueryBBox(_ stationcache.BBox, _ time.Duration) []stationcache.Station {
	out := make([]stationcache.Station, len(f.stations))
	copy(out, f.stations)
	return out
}

func (f *fakeStationCache) Lookup(_ []string) map[string]stationcache.LatLon {
	return nil
}

// newAutocompleteTestServer is a smaller sibling of the messages
// fixture — wires up the minimum for the autocomplete handler.
func newAutocompleteTestServer(t *testing.T, cache stationcache.StationStore, seed []configstore.Message) (*http.ServeMux, *messages.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	msgStore := messages.NewStore(store.DB())
	for i := range seed {
		row := seed[i]
		if err := msgStore.Insert(ctx, &row); err != nil {
			t.Fatal(err)
		}
	}

	srv, err := NewServer(Config{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	RegisterStationsAutocomplete(srv, mux, msgStore, cache)
	return mux, msgStore
}

func TestAutocomplete_EmptyQReturnsBotsAndRecent(t *testing.T) {
	cache := &fakeStationCache{
		stations: []stationcache.Station{
			{Callsign: "W1ABC", LastHeard: time.Now().Add(-5 * time.Minute), Symbol: [2]byte{'/', '>'}},
			{Callsign: "W2XYZ", LastHeard: time.Now().Add(-30 * time.Minute), Symbol: [2]byte{'/', '>'}},
		},
	}
	mux, _ := newAutocompleteTestServer(t, cache, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/stations/autocomplete", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []dto.StationAutocomplete
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if len(resp) == 0 {
		t.Fatal("expected at least bots in response")
	}
	// First entries must all be bots.
	seenBot := false
	seenStation := false
	for _, r := range resp {
		if r.Source == "bot" {
			seenBot = true
			if seenStation {
				t.Errorf("bot %q appeared after a station — bots must lead", r.Callsign)
			}
		} else {
			seenStation = true
		}
	}
	if !seenBot {
		t.Error("expected bot entries in response")
	}
}

func TestAutocomplete_PrefixMatchesBots(t *testing.T) {
	mux, _ := newAutocompleteTestServer(t, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/stations/autocomplete?q=sm", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp []dto.StationAutocomplete
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	hitSMS := false
	for _, r := range resp {
		if r.Callsign == "SMS" && r.Source == "bot" {
			hitSMS = true
		}
	}
	if !hitSMS {
		t.Errorf("expected SMS bot to match prefix 'sm'; got %+v", resp)
	}
}

func TestAutocomplete_DedupCachePlusHistory(t *testing.T) {
	cache := &fakeStationCache{
		stations: []stationcache.Station{
			{Callsign: "W1ABC", LastHeard: time.Now().Add(-1 * time.Hour), Symbol: [2]byte{'/', '>'}},
		},
	}
	seed := []configstore.Message{
		{
			Direction: "out", OurCall: "N0CALL", FromCall: "N0CALL", ToCall: "W1ABC",
			ThreadKind: messages.ThreadKindDM, MsgID: "001", Text: "hi",
		},
	}
	mux, _ := newAutocompleteTestServer(t, cache, seed)
	req := httptest.NewRequest(http.MethodGet, "/api/stations/autocomplete?q=W1", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var resp []dto.StationAutocomplete
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	// Find W1ABC and check source.
	var found *dto.StationAutocomplete
	for i := range resp {
		if resp[i].Callsign == "W1ABC" {
			found = &resp[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected W1ABC in response, got %+v", resp)
	}
	if found.Source != "cache+history" {
		t.Errorf("expected source=cache+history, got %q", found.Source)
	}
}

func TestAutocomplete_HistoryOnlyWhenCacheMisses(t *testing.T) {
	seed := []configstore.Message{
		{
			Direction: "in", OurCall: "N0CALL", FromCall: "ZZZ99", ToCall: "N0CALL",
			ThreadKind: messages.ThreadKindDM, Text: "hi",
		},
	}
	mux, _ := newAutocompleteTestServer(t, nil, seed)
	req := httptest.NewRequest(http.MethodGet, "/api/stations/autocomplete?q=ZZZ", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var resp []dto.StationAutocomplete
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	for _, r := range resp {
		if r.Callsign == "ZZZ99" {
			if r.Source != "history" {
				t.Errorf("expected source=history, got %q", r.Source)
			}
			return
		}
	}
	t.Fatalf("expected ZZZ99 in response, got %+v", resp)
}

func TestAutocomplete_BotCollisionHidesStation(t *testing.T) {
	// If a station somehow also matches a bot name (a real W1ABC would
	// never be "SMS", but defensive), the bot entry wins.
	cache := &fakeStationCache{
		stations: []stationcache.Station{
			{Callsign: "SMS", LastHeard: time.Now().Add(-1 * time.Hour), Symbol: [2]byte{'/', '>'}},
		},
	}
	mux, _ := newAutocompleteTestServer(t, cache, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/stations/autocomplete?q=sms", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var resp []dto.StationAutocomplete
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	smsCount := 0
	for _, r := range resp {
		if r.Callsign == "SMS" {
			smsCount++
			if r.Source != "bot" {
				t.Errorf("expected bot-source for SMS, got %q", r.Source)
			}
		}
	}
	if smsCount != 1 {
		t.Errorf("expected exactly one SMS entry, got %d", smsCount)
	}
}

func TestAutocomplete_BadLimit(t *testing.T) {
	mux, _ := newAutocompleteTestServer(t, nil, nil)
	req := httptest.NewRequest(http.MethodGet, "/api/stations/autocomplete?limit=999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
