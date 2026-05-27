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
	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/webtypes"
)

// seedNonTxChannel creates a channel with an input device but no output device
// (OutputDeviceID == 0), which makes it non-TX-capable per computeTxCapability.
func seedNonTxChannel(t *testing.T, srv *Server) uint32 {
	t.Helper()
	ctx := context.Background()
	// Re-use the existing input device (id 1, direction=input).
	ch := &configstore.Channel{
		Name:           "rx-only",
		InputDeviceID:  configstore.U32Ptr(1),
		OutputDeviceID: 0, // no output — not TX-capable
		ModemType:      "afsk",
		BitRate:        1200,
		MarkFreq:       1200,
		SpaceFreq:      2200,
		Profile:        "A",
		NumSlicers:     1,
		FixBits:        "none",
	}
	if err := srv.store.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("seedNonTxChannel: %v", err)
	}
	return ch.ID
}

// postTestSignal issues POST /api/channels/{id}/test-tx with the given signal
// string and returns the recorder.
func postTestSignal(t *testing.T, mux *http.ServeMux, channelID uint32, signal string) *httptest.ResponseRecorder {
	t.Helper()
	body := `{"signal":"` + signal + `"}`
	req := httptest.NewRequest(http.MethodPost,
		"/api/channels/"+strconv.FormatUint(uint64(channelID), 10)+"/test-tx",
		strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

// TestSendTestSignal_CWEmptyCallsign verifies that the "cw" signal returns
// 422 when no station callsign has been set (the store returns ErrCallsignEmpty).
// newTestServer seeds no StationConfig row, so GetStationConfig returns a
// zero-value struct with Callsign == "" → ResolveStationCallsign returns
// ErrCallsignEmpty.
func TestSendTestSignal_CWEmptyCallsign(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Seed channel id is 1 (TX-capable) from newTestServer.
	rec := postTestSignal(t, mux, 1, "cw")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var er webtypes.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&er); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(er.Error, "callsign") {
		t.Errorf("error body = %q; want mention of callsign", er.Error)
	}
}

// TestSendTestSignal_CWN0CallCallsign verifies that the "cw" signal returns
// 422 when the station callsign is N0CALL.
func TestSendTestSignal_CWN0CallCallsign(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	if err := srv.store.UpsertStationConfig(context.Background(), configstore.StationConfig{
		Callsign: "N0CALL",
	}); err != nil {
		t.Fatalf("UpsertStationConfig: %v", err)
	}

	rec := postTestSignal(t, mux, 1, "cw")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d: %s", rec.Code, rec.Body.String())
	}
	var er webtypes.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&er); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(er.Error, "N0CALL") {
		t.Errorf("error body = %q; want mention of N0CALL", er.Error)
	}
}

// TestSendTestSignal_UnknownSignal verifies that an unknown signal value
// returns 400 Bad Request.
func TestSendTestSignal_UnknownSignal(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := postTestSignal(t, mux, 1, "bogus")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	var er webtypes.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&er); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(er.Error, "bogus") {
		t.Errorf("error body = %q; want mention of bogus", er.Error)
	}
}

// TestSendTestSignal_NonTxChannelReturns409 verifies that sending any valid
// signal on a channel that is not TX-capable returns 409 Conflict. The
// non-TX channel has an input device but no output device.
func TestSendTestSignal_NonTxChannelReturns409(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	nonTxID := seedNonTxChannel(t, srv)

	rec := postTestSignal(t, mux, nonTxID, "tone1200")
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var er webtypes.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&er); err != nil {
		t.Fatal(err)
	}
	if er.Error == "" {
		t.Errorf("expected non-empty error message in 409 body")
	}
}

// TestBuildTestSignalParams pins the four UI signal ids to their modem TX
// parameters. This guards the hardcoded recipe table against a silent swap
// (e.g. tone1200<->tone2400 frequencies, or a wrong Kind) that the HTTP-level
// guard tests would not catch.
func TestBuildTestSignalParams(t *testing.T) {
	const ch = 7
	tests := []struct {
		signal string
		want   modembridge.TestSignalParams
	}{
		{"cw", modembridge.TestSignalParams{Channel: ch, Kind: 0, CwWpm: cwTestWpm, FreqAHz: cwTestToneHz}},
		{"tone1200", modembridge.TestSignalParams{Channel: ch, Kind: 1, FreqAHz: toneTestLowHz, DurationMs: toneTestDurMs}},
		{"tone2400", modembridge.TestSignalParams{Channel: ch, Kind: 1, FreqAHz: toneTestHighHz, DurationMs: toneTestDurMs}},
		{"alt", modembridge.TestSignalParams{Channel: ch, Kind: 2, FreqAHz: toneTestLowHz, FreqBHz: toneTestHighHz, DurationMs: toneTestDurMs, AltPeriodMs: altTestPeriodMs}},
	}
	for _, tt := range tests {
		got, ok := buildTestSignalParams(ch, tt.signal)
		if !ok {
			t.Errorf("%s: ok = false, want true", tt.signal)
			continue
		}
		if got != tt.want {
			t.Errorf("%s: got %+v, want %+v", tt.signal, got, tt.want)
		}
	}
	// cw must leave Callsign empty for the handler to fill from station config.
	if got, _ := buildTestSignalParams(ch, "cw"); got.Callsign != "" {
		t.Errorf("cw: Callsign = %q, want empty (handler fills it)", got.Callsign)
	}
	if _, ok := buildTestSignalParams(ch, "bogus"); ok {
		t.Errorf("bogus: ok = true, want false")
	}
}

// TestSendTestSignal_Tone1200TxCapableReturns503 verifies that a tone1200
// request on a TX-capable channel clears all guards and reaches the bridge.
// The test bridge is real but not running, so TransmitTestSignal returns
// "not in RUNNING state" → 503. This proves the request passed parseID,
// body decode, signal switch, and requireTxCapableChannel.
func TestSendTestSignal_Tone1200TxCapableReturns503(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Channel 1 from newTestServer is TX-capable (input + output devices).
	rec := postTestSignal(t, mux, 1, "tone1200")
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 (bridge not running), got %d: %s", rec.Code, rec.Body.String())
	}
	var er webtypes.ErrorResponse
	if err := json.NewDecoder(rec.Body).Decode(&er); err != nil {
		t.Fatal(err)
	}
	if er.Error == "" {
		t.Errorf("expected non-empty error message in 503 body")
	}
}
