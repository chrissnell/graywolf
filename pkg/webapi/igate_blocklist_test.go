package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIGateSourceExclusionsValidationAndRoundTrip(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	save := func(list string) *httptest.ResponseRecorder {
		t.Helper()
		body, err := json.Marshal(map[string]any{"enabled": false, "rf_to_is_blocklist": list})
		if err != nil {
			t.Fatal(err)
		}
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/igate/config", strings.NewReader(string(body))))
		return rec
	}
	for _, invalid := range []string{"K1ABC-16", "K1ABC-*", "K1ABC/B"} {
		if rec := save(invalid); rec.Code != http.StatusBadRequest {
			t.Fatalf("%q: got %d: %s", invalid, rec.Code, rec.Body.String())
		}
	}
	for _, input := range []string{" k1abc, W5XYZ-07 k1abc ", ""} {
		rec := save(input)
		if rec.Code != http.StatusOK {
			t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
		}
		want := "K1ABC\nW5XYZ-7"
		if input == "" {
			want = ""
		}
		cfg, err := srv.store.GetIGateConfig(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if cfg.RFToISBlocklist != want {
			t.Fatalf("stored %q, want %q", cfg.RFToISBlocklist, want)
		}
		rec = httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/igate/config", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET: %d", rec.Code)
		}
		var got map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
			t.Fatal(err)
		}
		if got["rf_to_is_blocklist"] != want {
			t.Fatalf("GET failed round trip: %s", rec.Body.String())
		}
	}
}
