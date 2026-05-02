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

// Regression: partial PUT must NOT zero-out unrelated columns. The
// front-end macros store and RawPacketView each PUT only the field
// they own; before C1 the handler always wrote every column from a
// default-zero struct and silently destroyed everything else.
func TestPutAX25TerminalConfig_PartialPUTPreservesOtherColumns(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Seed full config first.
	seed := `{
		"scrollback_rows": 4096,
		"cursor_blink": true,
		"default_modulo": 128,
		"default_paclen": 256,
		"macros": [{"label": "login", "payload": "TQ=="}],
		"raw_tail_filter": "icall=W6XYZ"
	}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/ax25/terminal-config", strings.NewReader(seed)))
	if rec.Code != http.StatusOK {
		t.Fatalf("seed status=%d body=%s", rec.Code, rec.Body.String())
	}

	// Patch only macros. Every other column must survive.
	macroPatch := `{"macros":[{"label":"list","payload":"TA=="}]}`
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodPut, "/api/ax25/terminal-config", strings.NewReader(macroPatch)))
	if rec2.Code != http.StatusOK {
		t.Fatalf("patch status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var afterMacros dto.AX25TerminalConfig
	if err := json.NewDecoder(rec2.Body).Decode(&afterMacros); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if afterMacros.ScrollbackRows != 4096 {
		t.Fatalf("scrollback wiped: %d (want 4096)", afterMacros.ScrollbackRows)
	}
	if !afterMacros.CursorBlink {
		t.Fatal("cursor_blink wiped")
	}
	if afterMacros.DefaultModulo != 128 {
		t.Fatalf("default_modulo wiped: %d", afterMacros.DefaultModulo)
	}
	if afterMacros.DefaultPaclen != 256 {
		t.Fatalf("default_paclen wiped: %d", afterMacros.DefaultPaclen)
	}
	if afterMacros.RawTailFilter != "icall=W6XYZ" {
		t.Fatalf("raw_tail_filter wiped: %q", afterMacros.RawTailFilter)
	}
	if len(afterMacros.Macros) != 1 || afterMacros.Macros[0].Label != "list" {
		t.Fatalf("macros not applied: %+v", afterMacros.Macros)
	}

	// Now patch only raw_tail_filter. Every other column must survive.
	filterPatch := `{"raw_tail_filter":"src=KE7XYZ"}`
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, httptest.NewRequest(http.MethodPut, "/api/ax25/terminal-config", strings.NewReader(filterPatch)))
	if rec3.Code != http.StatusOK {
		t.Fatalf("filter patch status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	var afterFilter dto.AX25TerminalConfig
	if err := json.NewDecoder(rec3.Body).Decode(&afterFilter); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if afterFilter.RawTailFilter != "src=KE7XYZ" {
		t.Fatalf("filter not applied: %q", afterFilter.RawTailFilter)
	}
	if len(afterFilter.Macros) != 1 || afterFilter.Macros[0].Label != "list" {
		t.Fatalf("macros wiped by filter patch: %+v", afterFilter.Macros)
	}
	if afterFilter.ScrollbackRows != 4096 || afterFilter.DefaultModulo != 128 {
		t.Fatalf("numeric columns wiped: %+v", afterFilter)
	}
}

// I1: zero-valued explicit fields must fail validation.
func TestPutAX25TerminalConfig_RejectsExplicitZeros(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	cases := map[string]string{
		"modulo zero":     `{"default_modulo": 0}`,
		"paclen zero":     `{"default_paclen": 0}`,
		"scrollback zero": `{"scrollback_rows": 0}`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/ax25/terminal-config", strings.NewReader(body)))
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: status=%d body=%s, want 400", name, rec.Code, rec.Body.String())
			}
		})
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
