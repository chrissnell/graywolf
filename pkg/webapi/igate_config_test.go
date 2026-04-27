package webapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// TestIGateConfig_AcceptsTxCapableChannel is the positive path: saving
// an enabled iGate with tx_channel pointing at the seed (TX-capable)
// channel succeeds. Requires StationConfig.Callsign to be populated so
// the enable-guard passes — use the newTestServerWithStation helper
// locally or set it up here.
func TestIGateConfig_AcceptsTxCapableChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	// Seed a station callsign so the enable-guard doesn't short-circuit.
	if err := srv.store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CAL"}); err != nil {
		t.Fatal(err)
	}

	body := `{"enabled":true,"server":"rotate.aprs2.net","port":14580,"rf_channel":1,"tx_channel":1,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestIGateConfig_RejectsNonTxCapableChannel covers the Phase-1 TX
// gate. Create an RX-only channel + try to save an enabled iGate
// pointing tx_channel at it: rejected with 400.
func TestIGateConfig_RejectsNonTxCapableChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CAL"}); err != nil {
		t.Fatal(err)
	}

	rxCh := &configstore.Channel{
		Name: "rx-only", InputDeviceID: configstore.U32Ptr(1),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := srv.store.CreateChannel(ctx, rxCh); err != nil {
		t.Fatal(err)
	}

	body := `{"enabled":true,"server":"rotate.aprs2.net","port":14580,"rf_channel":1,"tx_channel":` +
		strconv.FormatUint(uint64(rxCh.ID), 10) + `,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "tx_channel") || !strings.Contains(rec.Body.String(), "not TX-capable") {
		t.Errorf("expected tx_channel TX-capable error, got %s", rec.Body.String())
	}
}

// TestIGateConfig_RejectsPipeInServerFilter verifies the DTO-layer
// validator is wired through the handler: a PUT with `|` in the
// server_filter comes back 400 with a message that names the field,
// so the web UI's toast surfaces a useful error instead of the save
// silently persisting a broken filter that APRS-IS will quietly drop.
func TestIGateConfig_RejectsPipeInServerFilter(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CAL"}); err != nil {
		t.Fatal(err)
	}

	body := `{"enabled":false,"server":"rotate.aprs2.net","port":14580,"server_filter":"g/NW5W | b/NW5W-12","rf_channel":1,"tx_channel":1,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "server_filter") || !strings.Contains(rec.Body.String(), "|") {
		t.Errorf("expected server_filter pipe error, got %s", rec.Body.String())
	}
}

// TestIGateConfig_AllowsNonTxCapableChannelWhenDisabled covers the
// escape hatch (plan D3): when the iGate is saved with Enabled=false,
// the TX-capability gate is skipped so operators can reshape channel
// config in any order.
func TestIGateConfig_AllowsNonTxCapableChannelWhenDisabled(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	rxCh := &configstore.Channel{
		Name: "rx-only", InputDeviceID: configstore.U32Ptr(1),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := srv.store.CreateChannel(ctx, rxCh); err != nil {
		t.Fatal(err)
	}

	body := `{"enabled":false,"server":"rotate.aprs2.net","port":14580,"rf_channel":1,"tx_channel":` +
		strconv.FormatUint(uint64(rxCh.ID), 10) + `,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 (disabled escape hatch), got %d: %s", rec.Code, rec.Body.String())
	}
}
