package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

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
