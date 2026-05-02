package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestGetAX25TerminalConfig_ReturnsSeededDefaults(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ax25/terminal-config", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got dto.AX25TerminalConfig
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.ScrollbackRows != 1000 || got.DefaultModulo != 8 || got.DefaultPaclen != 256 {
		t.Fatalf("defaults drifted: %+v", got)
	}
	if got.Macros == nil {
		t.Fatal("macros must be a non-nil array")
	}
}

func TestPutAX25TerminalConfig_PersistsMacros(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{
		"scrollback_rows": 4000,
		"cursor_blink": true,
		"default_modulo": 128,
		"default_paclen": 256,
		"macros": [
			{"label": "login", "payload": "TQ=="},
			{"label": "list",  "payload": "TA=="}
		],
		"raw_tail_filter": "icall=W6XYZ"
	}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/ax25/terminal-config", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var got dto.AX25TerminalConfig
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.DefaultModulo != 128 || !got.CursorBlink || got.ScrollbackRows != 4000 {
		t.Fatalf("response drifted: %+v", got)
	}
	if len(got.Macros) != 2 || got.Macros[0].Label != "login" || got.Macros[0].Payload != "TQ==" {
		t.Fatalf("macros drifted: %+v", got.Macros)
	}
	if got.RawTailFilter != "icall=W6XYZ" {
		t.Fatalf("raw_tail_filter drifted: %q", got.RawTailFilter)
	}

	// Round-trip: GET should return the same shape.
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, "/api/ax25/terminal-config", nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("re-get status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var got2 dto.AX25TerminalConfig
	if err := json.NewDecoder(rec2.Body).Decode(&got2); err != nil {
		t.Fatalf("re-decode: %v", err)
	}
	if len(got2.Macros) != 2 {
		t.Fatalf("macros not persisted: %+v", got2.Macros)
	}
}

func TestPutAX25TerminalConfig_RejectsBadModulo(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"default_modulo": 64, "default_paclen": 256}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/ax25/terminal-config", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}

func TestPutAX25TerminalConfig_RejectsBlankMacroLabel(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := `{"macros":[{"label":"","payload":"TQ=="}]}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/ax25/terminal-config", strings.NewReader(body)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rec.Code, rec.Body.String())
	}
}
