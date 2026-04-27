package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// TestBeaconCreate_HappyPath creates a position beacon with fixed coords.
func TestBeaconCreate_HappyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{
		"type":"position",
		"channel":1,
		"callsign":"N0CAL",
		"destination":"APGRWO",
		"path":"WIDE1-1",
		"latitude":37.5,
		"longitude":-122.0,
		"symbol_table":"/",
		"symbol":">",
		"interval":1800,
		"enabled":true
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == 0 {
		t.Errorf("expected id, got %+v", resp)
	}
	if resp.Callsign != "N0CAL" || resp.Latitude != 37.5 {
		t.Errorf("round trip mismatch: %+v", resp)
	}
}

// TestBeaconCreate_PositionWithoutCoordsReturns400 is the one hand-coded
// validation rule the legacy handler had.
func TestBeaconCreate_PositionWithoutCoordsReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"type":"position","callsign":"N0CAL","path":"WIDE1-1","symbol_table":"/","symbol":">","interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "latitude") {
		t.Errorf("expected coordinate error, got %s", rec.Body.String())
	}
}

// Per the centralized station-callsign plan (D2/D3), an empty or
// omitted Callsign on a beacon is permitted and means "inherit from
// StationConfig at transmit time". The old "callsign required" 400
// check was dropped; the runtime guard refuses to transmit a beacon
// whose resolved callsign is empty or N0CALL.
func TestBeaconCreate_MissingCallsignInheritsStation(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// No callsign field; use position to hit the coord-required path so
	// the request has enough non-trivial shape to be rejected for the
	// right reason (missing coords) if it were rejected at all.
	body := `{"type":"status","path":"WIDE1-1","symbol":">","interval":1800}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBeaconCreate_UnknownFieldReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"type":"position","callsign":"N0CAL","latitude":1,"longitude":2,"hmm":"nope"}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBeaconDelete_ThenGet404(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Create one.
	body := `{"type":"position","callsign":"N0CAL","latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// Delete.
	url := "/api/beacons/" + strconv.FormatUint(uint64(created.ID), 10)
	req2 := httptest.NewRequest(http.MethodDelete, url, nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("delete: expected 204, got %d", rec2.Code)
	}

	// Get 404.
	req3 := httptest.NewRequest(http.MethodGet, url, nil)
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, req3)
	if rec3.Code != http.StatusNotFound {
		t.Fatalf("get: expected 404, got %d", rec3.Code)
	}
}

// TestBeaconCreate_RejectsNonTxCapableChannel covers the Phase-1
// referrer-write gate: a beacon pointed at a channel that exists but is
// not TX-capable is rejected at POST time with 400 + reason. The
// channel gets an InputDeviceID but no OutputDeviceID so the backing
// falls through to "no output device configured".
func TestBeaconCreate_RejectsNonTxCapableChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Create an RX-only channel (input set, output zero) — passes
	// validateChannel but fails the TX-capability gate.
	ctx := context.Background()
	ch := &configstore.Channel{
		Name:          "rx-only",
		InputDeviceID: configstore.U32Ptr(1), // seed input device id=1
		ModemType:     "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := srv.store.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}

	body := `{"type":"position","channel":` + strconv.FormatUint(uint64(ch.ID), 10) +
		`,"callsign":"N0CAL","latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "not TX-capable") {
		t.Errorf("expected 'not TX-capable' in error, got %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), dto.TxReasonNoOutputDevice) {
		t.Errorf("expected reason %q in error, got %s", dto.TxReasonNoOutputDevice, rec.Body.String())
	}
}

// TestBeaconUpdate_RejectsNonTxCapableChannel covers the PUT side of
// the gate. Create a beacon on a TX-capable channel, then PUT a new
// body pointing at a non-TX-capable channel. The update is rejected.
func TestBeaconUpdate_RejectsNonTxCapableChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	// Create the beacon on the seed channel (id=1, TX-capable).
	body := `{"type":"position","channel":1,"callsign":"N0CAL","latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("seed create: %d %s", rec.Code, rec.Body.String())
	}
	var created dto.BeaconResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}

	// Add an RX-only channel and try to PUT the beacon onto it.
	rxCh := &configstore.Channel{
		Name: "rx-only", InputDeviceID: configstore.U32Ptr(1),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := srv.store.CreateChannel(ctx, rxCh); err != nil {
		t.Fatal(err)
	}
	put := `{"type":"position","channel":` + strconv.FormatUint(uint64(rxCh.ID), 10) +
		`,"callsign":"N0CAL","latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	url := "/api/beacons/" + strconv.FormatUint(uint64(created.ID), 10)
	req2 := httptest.NewRequest(http.MethodPut, url, strings.NewReader(put))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec2.Code, rec2.Body.String())
	}
	if !strings.Contains(rec2.Body.String(), "not TX-capable") {
		t.Errorf("expected 'not TX-capable', got %s", rec2.Body.String())
	}
}

// TestBeaconCreate_AcceptsTxCapableChannel is the positive counterpart:
// a beacon on the seeded TX-capable channel must still be accepted
// after the Phase-1 gate.
func TestBeaconCreate_AcceptsTxCapableChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"type":"position","channel":1,"callsign":"N0CAL","latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestBeaconList_ReturnsCreated(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"type":"position","callsign":"N0CAL","latitude":1,"longitude":2,"interval":1800,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", rec.Code)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/api/beacons", nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("list: expected 200, got %d", rec2.Code)
	}
	var list []dto.BeaconResponse
	if err := json.NewDecoder(rec2.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Callsign != "N0CAL" {
		t.Errorf("unexpected list: %+v", list)
	}
}
