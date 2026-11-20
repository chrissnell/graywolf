package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/telemetry"
)

func TestTelemetryConfigDefaultsAndRoundTrip(t *testing.T) {
	s, _ := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	get := httptest.NewRecorder()
	mux.ServeHTTP(get, httptest.NewRequest("GET", "/api/telemetry/config", nil))
	var initial TelemetryConfigDTO
	if err := json.Unmarshal(get.Body.Bytes(), &initial); err != nil {
		t.Fatal(err)
	}
	if get.Code != 200 || initial.Enabled || initial.Extended || initial.Delivery != "aprsis" {
		t.Fatalf("%d %+v", get.Code, initial)
	}
	for _, enabled := range []bool{true, false} {
		body := `{"enabled":` + map[bool]string{true: "true", false: "false"}[enabled] + `,"callsign":"test1-10","channel":1,"allow_above_255":true,"rf_path":""}`
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/telemetry/config", strings.NewReader(body)))
		if w.Code != 200 {
			t.Fatalf("%d %s", w.Code, w.Body.String())
		}
		c, _ := s.store.GetTelemetryConfig(context.Background())
		if c.Enabled != enabled || c.Callsign != "TEST1-10" || !c.AllowAbove255 {
			t.Fatalf("%+v", c)
		}
	}
}
func TestTelemetryRejectsInvalidAndUnavailableManualSend(t *testing.T) {
	s, _ := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	for _, body := range []string{`{"delivery":"both"}`, `{"callsign":"N0CALL"}`, `{"enabled":true,"delivery":"rf"}`, `{"channel":999}`, `{"rf_path":"INVALID PATH"}`, `{"metadata_is_seconds":10}`} {
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/telemetry/config", strings.NewReader(body)))
		if w.Code != 400 {
			t.Fatalf("accepted %s: %d %s", body, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/telemetry/report", nil))
	if w.Code != 503 {
		t.Fatal(w.Code)
	}
	sends := 0
	r := telemetry.New(telemetry.Options{Load: func(context.Context) (telemetry.Config, telemetry.State, error) {
		return telemetry.Defaults(), telemetry.State{}, nil
	}, Send: func(context.Context, telemetry.Config, string, string, string, string) error { sends++; return nil }})
	s.SetTelemetry(r)
	for _, path := range []string{"/api/telemetry/config", "/api/telemetry/status"} {
		w = httptest.NewRecorder()
		mux.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 {
			t.Fatal(w.Code)
		}
	}
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("POST", "/api/telemetry/report", nil))
	if w.Code != 409 || sends != 0 {
		t.Fatalf("%d sends=%d", w.Code, sends)
	}
}

func TestTelemetryRequiresRFChannelEvenWhileDisabled(t *testing.T) {
	s, _ := newTestServer(t)
	mux := http.NewServeMux()
	s.RegisterRoutes(mux)
	for _, delivery := range []string{"auto", "rf"} {
		for _, enabled := range []bool{false, true} {
			payload, _ := json.Marshal(TelemetryConfigDTO{Enabled: enabled, Callsign: "TEST1-10", Delivery: delivery})
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/telemetry/config", strings.NewReader(string(payload))))
			if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), "RF delivery channel") {
				t.Fatalf("delivery=%s enabled=%t: %d %s", delivery, enabled, w.Code, w.Body.String())
			}
		}
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("PUT", "/api/telemetry/config", strings.NewReader(`{"enabled":true,"callsign":"TEST1-10","delivery":"aprsis","channel":0}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("IS-only without RF: %d %s", w.Code, w.Body.String())
	}
}
