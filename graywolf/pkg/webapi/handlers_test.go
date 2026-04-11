package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
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

// --- handleCreate wiring tests ---

type fakeReq struct {
	Name     string `json:"name"`
	BadInput bool   `json:"bad_input"`
}

func (r fakeReq) Validate() error {
	if r.Name == "" {
		return errors.New("name is required")
	}
	return nil
}

type fakeModel struct {
	ID   uint32
	Name string
}

type fakeResp struct {
	ID   uint32 `json:"id"`
	Name string `json:"name"`
}

func fakeToResp(m fakeModel) fakeResp {
	return fakeResp{ID: m.ID, Name: m.Name}
}

func newTestSrv() *Server {
	return &Server{logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
}

func TestHandleCreate_HappyPath(t *testing.T) {
	srv := newTestSrv()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(`{"name":"foo"}`))

	var seen fakeReq
	create := func(_ context.Context, r fakeReq) (fakeModel, error) {
		seen = r
		return fakeModel{ID: 42, Name: r.Name}, nil
	}

	handleCreate[fakeReq](srv, rec, req, "create", create, fakeToResp)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if seen.Name != "foo" {
		t.Errorf("create not invoked with decoded request, got %+v", seen)
	}
	var body fakeResp
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.ID != 42 || body.Name != "foo" {
		t.Errorf("response did not use toResp mapping: %+v", body)
	}
}

func TestHandleCreate_ValidationFailureReturns400(t *testing.T) {
	srv := newTestSrv()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(`{"name":""}`))

	called := false
	create := func(_ context.Context, r fakeReq) (fakeModel, error) {
		called = true
		return fakeModel{}, nil
	}

	handleCreate[fakeReq](srv, rec, req, "create", create, fakeToResp)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("create should not be invoked when validation fails")
	}
}

func TestHandleCreate_UnknownFieldReturns400(t *testing.T) {
	srv := newTestSrv()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(`{"name":"foo","who_knows":1}`))

	create := func(_ context.Context, _ fakeReq) (fakeModel, error) {
		t.Error("create should not be invoked when decode fails")
		return fakeModel{}, nil
	}

	handleCreate[fakeReq](srv, rec, req, "create", create, fakeToResp)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandleCreate_StoreErrorReturns500(t *testing.T) {
	var logBuf bytes.Buffer
	srv := &Server{logger: slog.New(slog.NewTextHandler(&logBuf, nil))}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/x",
		strings.NewReader(`{"name":"foo"}`))

	const secret = "UNIQUE constraint failed: channels.name"
	create := func(_ context.Context, _ fakeReq) (fakeModel, error) {
		return fakeModel{}, errors.New(secret)
	}

	handleCreate[fakeReq](srv, rec, req, "create channel", create, fakeToResp)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d", rec.Code)
	}
	var body map[string]string
	_ = json.NewDecoder(rec.Body).Decode(&body)
	if body["error"] != "internal error" {
		t.Errorf("response leaked store error: %q", body["error"])
	}
	if !strings.Contains(logBuf.String(), secret) {
		t.Error("logger did not receive real error")
	}
}

// --- handleList/Get/Update/Delete smoke tests ---

func TestHandleList_MapsEveryElement(t *testing.T) {
	srv := newTestSrv()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	list := func(_ context.Context) ([]fakeModel, error) {
		return []fakeModel{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}}, nil
	}

	handleList[fakeModel](srv, rec, req, "list", list, fakeToResp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var body []fakeResp
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if len(body) != 2 || body[0].Name != "a" || body[1].ID != 2 {
		t.Errorf("unexpected body: %+v", body)
	}
}

func TestHandleGet_NotFoundMaps404(t *testing.T) {
	srv := newTestSrv()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/x", nil)

	get := func(_ context.Context, _ uint32) (fakeModel, error) {
		return fakeModel{}, errors.New("record not found")
	}

	handleGet[fakeModel](srv, rec, req, 1, get, fakeToResp)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestHandleUpdate_PinsURLID(t *testing.T) {
	srv := newTestSrv()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/x/7",
		strings.NewReader(`{"name":"renamed"}`))

	var gotID uint32
	update := func(_ context.Context, id uint32, r fakeReq) (fakeModel, error) {
		gotID = id
		return fakeModel{ID: id, Name: r.Name}, nil
	}

	handleUpdate[fakeReq](srv, rec, req, "update", 7, update, fakeToResp)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if gotID != 7 {
		t.Errorf("expected id=7 from URL, got %d", gotID)
	}
}

func TestHandleDelete_Writes204(t *testing.T) {
	srv := newTestSrv()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodDelete, "/x/1", nil)

	var sawID uint32
	del := func(_ context.Context, id uint32) error {
		sawID = id
		return nil
	}

	handleDelete(srv, rec, req, "delete", 1, del)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	if sawID != 1 {
		t.Errorf("expected id=1, got %d", sawID)
	}
}

