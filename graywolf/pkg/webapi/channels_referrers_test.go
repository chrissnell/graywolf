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
)

// seedChannelWithRef creates a fresh channel plus a single referring
// beacon on it. Shared helper for the Phase 5 webapi tests.
func seedChannelWithRef(t *testing.T, srv *Server) (chID uint32) {
	t.Helper()
	ctx := context.Background()
	// A fresh channel — the fixture seeded by newTestServer already has
	// one (id=1) but we want a clean slate without unrelated timing /
	// igate refs interfering with the assertion.
	dev := &configstore.AudioDevice{
		Name: "extra", Direction: "input", SourceType: "flac",
		SourcePath: "/tmp/e.flac", SampleRate: 44100, Channels: 1, Format: "s16le",
	}
	if err := srv.store.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatal(err)
	}
	ch := &configstore.Channel{
		Name: "vhf-extra", InputDeviceID: configstore.U32Ptr(dev.ID),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := srv.store.CreateChannel(ctx, ch); err != nil {
		t.Fatal(err)
	}
	b := &configstore.Beacon{Channel: ch.ID, Callsign: "REF", Type: "position"}
	if err := srv.store.CreateBeacon(ctx, b); err != nil {
		t.Fatal(err)
	}
	return ch.ID
}

func TestGetChannelReferrers_Shape(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	chID := seedChannelWithRef(t, srv)

	req := httptest.NewRequest(http.MethodGet,
		"/api/channels/"+strconv.FormatUint(uint64(chID), 10)+"/referrers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var body ChannelReferrersResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Referrers) == 0 {
		t.Fatal("expected at least one referrer")
	}
	found := false
	for _, r := range body.Referrers {
		if r.Type == configstore.ReferrerTypeBeacon && r.Name != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a beacon referrer with non-empty name, got %+v", body.Referrers)
	}
}

func TestGetChannelReferrers_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels/9999/referrers", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for nonexistent channel, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestDeleteChannel_409WhenReferenced covers the default (non-cascade)
// delete path: a referenced channel yields 409 with the structured
// body, and the channel must still exist after the attempt.
func TestDeleteChannel_409WhenReferenced(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	chID := seedChannelWithRef(t, srv)

	req := httptest.NewRequest(http.MethodDelete,
		"/api/channels/"+strconv.FormatUint(uint64(chID), 10), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", rec.Code, rec.Body.String())
	}
	var body ChannelReferrersResponse
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if body.Error == "" {
		t.Errorf("expected populated error on 409 body, got empty")
	}
	if len(body.Referrers) == 0 {
		t.Errorf("expected referrer list in 409 body, got empty")
	}

	// Channel must still exist — 409 means nothing was deleted.
	if _, err := srv.store.GetChannel(context.Background(), chID); err != nil {
		t.Errorf("channel should still exist after 409: %v", err)
	}
}

func TestDeleteChannel_CascadeHappyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	chID := seedChannelWithRef(t, srv)

	// Also add a kiss interface on the channel so we can verify the
	// null-+-flag policy post-cascade.
	ki := &configstore.KissInterface{
		Name: "kiss-cas", InterfaceType: configstore.KissTypeTCP,
		ListenAddr: "0.0.0.0:1", Channel: chID, Enabled: true,
	}
	if err := srv.store.CreateKissInterface(context.Background(), ki); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodDelete,
		"/api/channels/"+strconv.FormatUint(uint64(chID), 10)+"?cascade=true", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	// Channel gone.
	if _, err := srv.store.GetChannel(context.Background(), chID); err == nil {
		t.Errorf("channel should be gone after cascade")
	}
	// Kiss interface survived with Channel=0 + NeedsReconfig=true.
	post, err := srv.store.GetKissInterface(context.Background(), ki.ID)
	if err != nil {
		t.Fatalf("kiss interface row should survive cascade: %v", err)
	}
	if post.Channel != 0 || !post.NeedsReconfig {
		t.Errorf("post-cascade kiss interface = %+v, want Channel=0 NeedsReconfig=true", post)
	}
}

func TestDeleteChannel_404Unknown(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodDelete, "/api/channels/9999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// TestBeaconCreate_RejectsOrphanChannel asserts that the DTO-layer
// ValidateChannelRef helper lands at the handler and surfaces a 400
// with a clear error body when the posted channel doesn't exist.
func TestBeaconCreate_RejectsOrphanChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"type":"position","channel":9999,"callsign":"N0CALL","latitude":1,"longitude":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/beacons", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "channel 9999 does not exist") {
		t.Errorf("expected orphan-ref error, got %s", rec.Body.String())
	}
}

// TestDigipeaterRule_RejectsOrphanToChannel catches the cross-channel
// rule DTO branch — ToChannel is a separate validation call.
func TestDigipeaterRule_RejectsOrphanToChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Fixture has channel id=1; use a valid From and an orphan To so
	// the test isolates the to_channel branch.
	body := `{"from_channel":1,"to_channel":9999,"alias":"WIDE","alias_type":"widen","max_hops":1,"action":"repeat","priority":100,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/digipeater/rules", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "to_channel") {
		t.Errorf("expected error mentioning to_channel, got %s", rec.Body.String())
	}
}

// TestIGateConfig_RejectsOrphanChannel covers the singleton write path
// (PUT /api/igate/config), which doesn't use the generic handler
// wrappers — the check runs directly via badRequest.
func TestIGateConfig_RejectsOrphanChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"enabled":false,"server":"rotate.aprs2.net","port":14580,"callsign":"N0CALL","passcode":"-1","rf_channel":9999,"tx_channel":1,"max_msg_hops":2,"software_name":"graywolf","software_version":"0.1"}`
	req := httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "rf_channel") {
		t.Errorf("expected rf_channel error, got %s", rec.Body.String())
	}
}

// TestTxTiming_RejectsOrphanChannel covers the PUT path where the
// channel id is in the URL, not the body.
func TestTxTiming_RejectsOrphanChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"tx_delay_ms":300,"tx_tail_ms":100,"slot_ms":100,"persist":63}`
	req := httptest.NewRequest(http.MethodPut, "/api/tx-timing/9999", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestKissInterface_RejectsOrphanChannel covers POST /api/kiss for the
// KissRequest.Channel branch. We use a tcp-server shape since
// tcp-client has additional required fields.
func TestKissInterface_RejectsOrphanChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"type":"tcp","tcp_port":12345,"channel":9999,"mode":"modem"}`
	req := httptest.NewRequest(http.MethodPost, "/api/kiss", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "channel 9999") {
		t.Errorf("expected channel error, got %s", rec.Body.String())
	}
}

// TestIGateFilter_RejectsOrphanChannel covers the POST /api/igate/filters
// handler path.
func TestIGateFilter_RejectsOrphanChannel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"channel":9999,"type":"callsign","pattern":"N0CALL","action":"allow","priority":100,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/igate/filters", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "channel 9999") {
		t.Errorf("expected channel error, got %s", rec.Body.String())
	}
}

// TestKissInterface_PUTClearsNeedsReconfig verifies the UX contract
// that saving a valid channel on a previously-orphaned KISS interface
// drops the NeedsReconfig flag.
func TestKissInterface_PUTClearsNeedsReconfig(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	// Seed a kiss interface whose Channel was nulled by a cascade
	// (fake the NeedsReconfig=true state directly).
	ki := &configstore.KissInterface{
		Name: "kiss-needs-reconfig", InterfaceType: configstore.KissTypeTCP,
		ListenAddr: "0.0.0.0:1", Channel: 0, Enabled: true, NeedsReconfig: true,
	}
	// Direct raw insert — the normalizer would object to Channel=0,
	// but the real post-cascade path sets it via an Update, not
	// CreateKissInterface.
	if err := srv.store.DB().Create(ki).Error; err != nil {
		t.Fatal(err)
	}

	// Operator fixes it up via PUT with a valid channel.
	body := `{"type":"tcp","tcp_port":1,"channel":1,"mode":"modem"}`
	req := httptest.NewRequest(http.MethodPut,
		"/api/kiss/"+strconv.FormatUint(uint64(ki.ID), 10), strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	post, err := srv.store.GetKissInterface(ctx, ki.ID)
	if err != nil {
		t.Fatal(err)
	}
	if post.NeedsReconfig {
		t.Errorf("NeedsReconfig should clear on valid PUT, got true")
	}
	if post.Channel != 1 {
		t.Errorf("Channel should update to 1, got %d", post.Channel)
	}
}
