package webapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestInternalErrorSanitizesResponse verifies that internalError writes a
// generic 500 body to the client but still surfaces the real error and op
// label to the logger, so we catch regressions that leak DB messages.
func TestInternalErrorSanitizesResponse(t *testing.T) {
	var logBuf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logBuf, &slog.HandlerOptions{Level: slog.LevelDebug}))
	srv := &Server{logger: logger}

	const secret = "UNIQUE constraint failed: users.username"
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)

	srv.internalError(rec, req, "test op", errors.New(secret))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("expected application/json, got %q", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if body["error"] != "internal error" {
		t.Errorf("expected generic message, got %q", body["error"])
	}
	if strings.Contains(body["error"], secret) {
		t.Errorf("response body leaked real error: %q", body["error"])
	}

	logs := logBuf.String()
	if !strings.Contains(logs, secret) {
		t.Errorf("logger did not receive real error, got: %s", logs)
	}
	if !strings.Contains(logs, "test op") {
		t.Errorf("logger did not receive op label, got: %s", logs)
	}
}
