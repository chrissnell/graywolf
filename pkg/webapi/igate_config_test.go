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

// TestIGateConfig_AllowsIdempotentOrphanFKs covers the regression
// where an iGate row whose rf_channel / tx_channel pointed at a
// channel that no longer existed (e.g. deleted via a path that
// bypassed the cascade, or migrated in from an older binary) trapped
// the operator: every PUT round-tripped the persisted orphan value
// back through ValidateChannelRef and 400'd, and the rf_channel field
// has no UI surface for the operator to edit. The handler now skips
// the existence check when the request value is unchanged from the
// persisted value, so saves of unrelated fields succeed even when
// the FKs are stale.
func TestIGateConfig_AllowsIdempotentOrphanFKs(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CAL"}); err != nil {
		t.Fatal(err)
	}

	// Seed an iGate row with rf_channel + tx_channel pointing at IDs
	// that do not exist in the channels table. Direct UpsertIGateConfig
	// bypasses the handler-level validation, modeling the
	// migration / out-of-band-delete case.
	if err := srv.store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Enabled: true, Server: "rotate.aprs2.net", Port: 14580,
		RfChannel: 99, TxChannel: 98, MaxMsgHops: 2,
		SoftwareName: "graywolf", SoftwareVersion: "0.1",
	}); err != nil {
		t.Fatal(err)
	}

	// PUT echoing the exact persisted FKs must succeed (idempotent
	// pass-through). The operator might be saving a different field
	// (server, server_filter, gate flags) and should not be blocked
	// by the orphans they cannot see or edit.
	body := `{"enabled":true,"server":"noam.aprs2.net","port":14580,"rf_channel":99,"tx_channel":98,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent orphan PUT: expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Repointing tx_channel to a NEW orphan must still 400. Idempotent
	// pass-through must not become a backdoor for newly-introduced
	// bad refs.
	body2 := `{"enabled":true,"server":"noam.aprs2.net","port":14580,"rf_channel":99,"tx_channel":77,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req2 := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body2))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusBadRequest {
		t.Fatalf("new orphan tx_channel: expected 400, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// TestIGateConfig_AllowsIdempotentOrphanTxChannelWhenEnabled pins the
// requireTxCapableChannel skip on the same idempotent edge: a
// persisted tx_channel that is not (or no longer) TX-capable must not
// block a save of unrelated fields when the operator did not change
// it. resolveTxChannel handles the runtime fallback.
func TestIGateConfig_AllowsIdempotentOrphanTxChannelWhenEnabled(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	if err := srv.store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CAL"}); err != nil {
		t.Fatal(err)
	}

	// Seed: orphan tx_channel persisted with enabled=true.
	if err := srv.store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Enabled: true, Server: "rotate.aprs2.net", Port: 14580,
		RfChannel: 0, TxChannel: 55, MaxMsgHops: 2,
		SoftwareName: "graywolf", SoftwareVersion: "0.1",
	}); err != nil {
		t.Fatal(err)
	}

	// Save with the same tx_channel and a different server — must pass
	// despite enabled=true.
	body := `{"enabled":true,"server":"noam.aprs2.net","port":14580,"rf_channel":0,"tx_channel":55,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("idempotent orphan tx_channel with enabled=true: expected 200, got %d: %s",
			rec.Code, rec.Body.String())
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
