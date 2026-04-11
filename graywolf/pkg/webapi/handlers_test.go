package webapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type testPayload struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func TestDecodeJSON_HappyPath(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(`{"name":"foo","count":3}`))
	got, err := decodeJSON[testPayload](req)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if got.Name != "foo" || got.Count != 3 {
		t.Fatalf("unexpected value: %+v", got)
	}
}

func TestDecodeJSON_RejectsUnknownFields(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(`{"name":"foo","bogus":1}`))
	_, err := decodeJSON[testPayload](req)
	if err == nil {
		t.Fatal("expected error for unknown field")
	}
	if !strings.Contains(err.Error(), "bogus") {
		t.Errorf("expected error to mention the unknown field, got: %v", err)
	}
}

func TestDecodeJSON_MalformedJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(`{`))
	_, err := decodeJSON[testPayload](req)
	if err == nil {
		t.Fatal("expected error for malformed json")
	}
}
