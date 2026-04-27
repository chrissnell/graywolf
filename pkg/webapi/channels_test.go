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
	"github.com/chrissnell/graywolf/pkg/kiss"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// TestChannelsCreate_HappyPath creates a channel via the handler and
// asserts the response contains an assigned id.
func TestChannelsCreate_HappyPath(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{
		"name": "new-channel",
		"input_device_id": 1,
		"modem_type": "afsk",
		"bit_rate": 1200,
		"mark_freq": 1200,
		"space_freq": 2200,
		"profile": "A",
		"num_slicers": 1,
		"fix_bits": "none"
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/channels", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.ChannelResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID == 0 {
		t.Errorf("expected non-zero id, got %+v", resp)
	}
	if resp.Name != "new-channel" {
		t.Errorf("unexpected name: %+v", resp)
	}
}

func TestChannelsCreate_MissingNameReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"input_device_id": 1, "modem_type": "afsk"}`
	req := httptest.NewRequest(http.MethodPost, "/api/channels", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "name") {
		t.Errorf("expected error to mention name, got %s", rec.Body.String())
	}
}

func TestChannelsCreate_UnknownFieldReturns400(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"name":"x","input_device_id":1,"modem_type":"afsk","bogus":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/channels", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestChannelsList_ReturnsSeededRow(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp []dto.ChannelResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) == 0 || resp[0].Name != "rx0" {
		t.Errorf("unexpected list: %+v", resp)
	}
}

// TestChannelsList_BackingModem covers the modem-backed branch: a
// channel with an audio input device reports summary=modem. Without a
// live bridge subprocess the health is "down" (bridge.IsRunning() is
// false in the in-memory test harness). This also exercises the
// response structure so the UI sees a non-nil Backing object.
func TestChannelsList_BackingModem(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/channels", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []dto.ChannelResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) == 0 {
		t.Fatalf("expected at least one channel")
	}
	ch := resp[0]
	if ch.Backing == nil {
		t.Fatalf("expected backing object, got nil")
	}
	if ch.Backing.Summary != dto.ChannelBackingSummaryModem {
		t.Errorf("expected summary=modem, got %q", ch.Backing.Summary)
	}
	if ch.Backing.Health != dto.ChannelBackingHealthDown {
		t.Errorf("expected health=down (no live bridge), got %q", ch.Backing.Health)
	}
	if ch.Backing.Modem.Active {
		t.Errorf("expected modem.active=false, got true")
	}
	if len(ch.Backing.KissTnc) != 0 {
		t.Errorf("expected empty kiss_tnc, got %+v", ch.Backing.KissTnc)
	}
}

// TestComputeChannelBacking_Unbound covers the fall-through branch:
// a channel with no input device + no TNC interface reports
// summary=unbound and health=unbound. Phase 2 flipped
// InputDeviceID to *uint32; the nil form is now the canonical
// "KISS-only / no backing" marker.
func TestComputeChannelBacking_Unbound(t *testing.T) {
	ch := configstore.Channel{ID: 99, Name: "unbound-ch", InputDeviceID: nil}
	b := computeChannelBacking(ch, nil, nil, false)
	if b.Summary != dto.ChannelBackingSummaryUnbound {
		t.Errorf("summary=%q", b.Summary)
	}
	if b.Health != dto.ChannelBackingHealthUnbound {
		t.Errorf("health=%q", b.Health)
	}
	if b.Modem.Active {
		t.Errorf("modem.active should be false")
	}
	if b.Modem.Reason == "" {
		t.Errorf("expected reason on unbound modem")
	}
}

// TestChannelsList_BackingKissTnc exercises the kiss-tnc branch
// directly at the pure-function level so the test doesn't need to
// stand up the KISS manager with a real listener. A channel with no
// modem + a TNC-mode KISS interface attached should report
// summary=kiss-tnc; health follows the live-state map.
func TestComputeChannelBacking_KissTnc(t *testing.T) {
	ch := configstore.Channel{ID: 11, Name: "LoRa"}
	ifaces := []configstore.KissInterface{
		{ID: 3, Name: "loramod", Channel: 11, Mode: configstore.KissModeTnc},
	}

	// live case
	statuses := map[uint32]kiss.InterfaceStatus{
		3: {State: kiss.StateListening},
	}
	b := computeChannelBacking(ch, ifaces, statuses, false)
	if b.Summary != dto.ChannelBackingSummaryKissTnc {
		t.Errorf("expected summary=kiss-tnc, got %q", b.Summary)
	}
	if b.Health != dto.ChannelBackingHealthLive {
		t.Errorf("expected health=live, got %q", b.Health)
	}
	if len(b.KissTnc) != 1 || b.KissTnc[0].InterfaceName != "loramod" {
		t.Errorf("unexpected kiss_tnc entries: %+v", b.KissTnc)
	}

	// down case: interface exists in config but not running
	b = computeChannelBacking(ch, ifaces, map[uint32]kiss.InterfaceStatus{}, false)
	if b.Summary != dto.ChannelBackingSummaryKissTnc {
		t.Errorf("expected summary=kiss-tnc, got %q", b.Summary)
	}
	if b.Health != dto.ChannelBackingHealthDown {
		t.Errorf("expected health=down, got %q", b.Health)
	}
	if b.KissTnc[0].State != kiss.StateStopped {
		t.Errorf("expected state=stopped, got %q", b.KissTnc[0].State)
	}

	// modem-mode interface on a channel must NOT be reported under kiss_tnc
	ifaces[0].Mode = configstore.KissModeModem
	b = computeChannelBacking(ch, ifaces, statuses, false)
	if b.Summary != dto.ChannelBackingSummaryUnbound {
		t.Errorf("modem-mode kiss interface should not count as backing, got summary=%q", b.Summary)
	}
	if len(b.KissTnc) != 0 {
		t.Errorf("expected empty kiss_tnc, got %+v", b.KissTnc)
	}
}

// TestComputeChannelBacking_ModemLive covers the happy path where the
// bridge is reported as running: health should be live and modem.active
// true.
func TestComputeChannelBacking_ModemLive(t *testing.T) {
	ch := configstore.Channel{ID: 1, Name: "VHF", InputDeviceID: configstore.U32Ptr(5)}
	b := computeChannelBacking(ch, nil, nil, true)
	if b.Summary != dto.ChannelBackingSummaryModem {
		t.Errorf("summary=%q", b.Summary)
	}
	if b.Health != dto.ChannelBackingHealthLive {
		t.Errorf("health=%q", b.Health)
	}
	if !b.Modem.Active {
		t.Errorf("modem.active=false")
	}
	if b.Modem.Reason != "" {
		t.Errorf("modem.reason=%q, want empty", b.Modem.Reason)
	}
}

func TestChannelsDelete_RemovesRow(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Find the seeded channel id.
	chs, err := srv.store.ListChannels(context.Background())
	if err != nil || len(chs) == 0 {
		t.Fatalf("seed channel missing: %v", err)
	}
	id := chs[0].ID

	req := httptest.NewRequest(http.MethodDelete, "/api/channels/"+strconv.FormatUint(uint64(id), 10), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}

	// GET now should 404.
	req2 := httptest.NewRequest(http.MethodGet, "/api/channels/"+strconv.FormatUint(uint64(id), 10), nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNotFound {
		t.Fatalf("expected 404 after delete, got %d", rec2.Code)
	}
}
