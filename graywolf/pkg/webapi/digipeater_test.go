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

// TestDigipeaterRule_AcceptsTxCapableChannels is the positive path.
func TestDigipeaterRule_AcceptsTxCapableChannels(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"from_channel":1,"to_channel":1,"alias":"WIDE","alias_type":"widen","max_hops":1,"action":"repeat","priority":100,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/digipeater/rules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDigipeaterRule_RejectsNonTxCapableFromChannel covers the
// from_channel TX gate on POST /api/digipeater/rules.
func TestDigipeaterRule_RejectsNonTxCapableFromChannel(t *testing.T) {
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

	body := `{"from_channel":` + strconv.FormatUint(uint64(rxCh.ID), 10) +
		`,"to_channel":1,"alias":"WIDE","alias_type":"widen","max_hops":1,"action":"repeat","priority":100,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/digipeater/rules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "from_channel") || !strings.Contains(rec.Body.String(), "not TX-capable") {
		t.Errorf("expected from_channel TX-capable error, got %s", rec.Body.String())
	}
}

// TestDigipeaterRule_RejectsNonTxCapableToChannel covers the to_channel
// branch — a separate validation call that must surface its field name
// in the error body.
func TestDigipeaterRule_RejectsNonTxCapableToChannel(t *testing.T) {
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

	body := `{"from_channel":1,"to_channel":` + strconv.FormatUint(uint64(rxCh.ID), 10) +
		`,"alias":"WIDE","alias_type":"widen","max_hops":1,"action":"repeat","priority":100,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/digipeater/rules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "to_channel") || !strings.Contains(rec.Body.String(), "not TX-capable") {
		t.Errorf("expected to_channel TX-capable error, got %s", rec.Body.String())
	}
}

// TestDigipeaterRule_AllowsNonTxCapableWhenDisabled covers the D3
// escape hatch: a disabled rule can point at a non-TX-capable channel
// so operators can stage broken rules while reshaping their config.
func TestDigipeaterRule_AllowsNonTxCapableWhenDisabled(t *testing.T) {
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

	body := `{"from_channel":1,"to_channel":` + strconv.FormatUint(uint64(rxCh.ID), 10) +
		`,"alias":"WIDE","alias_type":"widen","max_hops":1,"action":"repeat","priority":100,"enabled":false}`
	req := httptest.NewRequest(http.MethodPost, "/api/digipeater/rules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 (disabled escape hatch), got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDigipeaterRule_UnknownChannelStillReportsMissing preserves the
// existing "channel N does not exist" error path — the new gate must
// not shadow the typo-detection message.
func TestDigipeaterRule_UnknownChannelStillReportsMissing(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"from_channel":9999,"to_channel":1,"alias":"WIDE","alias_type":"widen","max_hops":1,"action":"repeat","priority":100,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/digipeater/rules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "does not exist") {
		t.Errorf("expected 'does not exist' error, got %s", rec.Body.String())
	}
}
