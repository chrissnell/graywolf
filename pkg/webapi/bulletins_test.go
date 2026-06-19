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
	"testing"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/bulletins"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// ---------------------------------------------------------------------------
// Fake BulletinService
// ---------------------------------------------------------------------------

type fakeBulletinSvc struct {
	sendFn       func(ctx context.Context, req bulletins.SendRequest) (*configstore.Bulletin, error)
	listFn       func(ctx context.Context, f bulletins.Filter) ([]configstore.Bulletin, error)
	deleteFn     func(ctx context.Context, id uint64) error
	markReadFn   func(ctx context.Context, id uint64) error
	markAllReadFn func(ctx context.Context) error
}

func (f *fakeBulletinSvc) Send(ctx context.Context, req bulletins.SendRequest) (*configstore.Bulletin, error) {
	if f.sendFn != nil {
		return f.sendFn(ctx, req)
	}
	return &configstore.Bulletin{ID: 1, Slot: req.Slot, Text: req.Text, Direction: "out"}, nil
}
func (f *fakeBulletinSvc) List(ctx context.Context, flt bulletins.Filter) ([]configstore.Bulletin, error) {
	if f.listFn != nil {
		return f.listFn(ctx, flt)
	}
	return nil, nil
}
func (f *fakeBulletinSvc) Delete(ctx context.Context, id uint64) error {
	if f.deleteFn != nil {
		return f.deleteFn(ctx, id)
	}
	return nil
}
func (f *fakeBulletinSvc) MarkRead(ctx context.Context, id uint64) error {
	if f.markReadFn != nil {
		return f.markReadFn(ctx, id)
	}
	return nil
}
func (f *fakeBulletinSvc) MarkAllRead(ctx context.Context) error {
	if f.markAllReadFn != nil {
		return f.markAllReadFn(ctx)
	}
	return nil
}

// ---------------------------------------------------------------------------
// Test fixture
// ---------------------------------------------------------------------------

func newBulletinTestServer(t *testing.T, svc BulletinService) (*Server, *http.ServeMux) {
	t.Helper()
	cs, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	srv, err := NewServer(Config{
		Store:  cs,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetBulletinService(svc)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux
}

// ---------------------------------------------------------------------------
// GET /api/bulletins
// ---------------------------------------------------------------------------

func TestListBulletins_Empty(t *testing.T) {
	_, mux := newBulletinTestServer(t, &fakeBulletinSvc{})

	req := httptest.NewRequest(http.MethodGet, "/api/bulletins", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []dto.BulletinResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 0 {
		t.Errorf("expected empty list, got %d items", len(resp))
	}
}

func TestListBulletins_WithRows(t *testing.T) {
	svc := &fakeBulletinSvc{
		listFn: func(_ context.Context, _ bulletins.Filter) ([]configstore.Bulletin, error) {
			return []configstore.Bulletin{
				{ID: 1, Direction: "in", Slot: "BLN0", FromCall: "W5X", Text: "hello", Unread: true},
				{ID: 2, Direction: "in", Slot: "BLN1", FromCall: "K9Y", Text: "world", Unread: false},
			}, nil
		},
	}
	_, mux := newBulletinTestServer(t, svc)

	req := httptest.NewRequest(http.MethodGet, "/api/bulletins?direction=in", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []dto.BulletinResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 2 {
		t.Fatalf("expected 2 items, got %d", len(resp))
	}
	if resp[0].Text != "hello" {
		t.Errorf("Text[0]: got %q", resp[0].Text)
	}
}

func TestListBulletins_503WhenNoService(t *testing.T) {
	cs, _ := configstore.OpenMemory()
	t.Cleanup(func() { _ = cs.Close() })
	srv, _ := NewServer(Config{Store: cs, Logger: slog.New(slog.NewTextHandler(io.Discard, nil))})
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodGet, "/api/bulletins", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/bulletins
// ---------------------------------------------------------------------------

func postJSON(t *testing.T, mux *http.ServeMux, path string, body any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestSendBulletin_Valid(t *testing.T) {
	var captured bulletins.SendRequest
	svc := &fakeBulletinSvc{
		sendFn: func(_ context.Context, req bulletins.SendRequest) (*configstore.Bulletin, error) {
			captured = req
			return &configstore.Bulletin{ID: 42, Slot: req.Slot, Text: req.Text, Direction: "out"}, nil
		},
	}
	_, mux := newBulletinTestServer(t, svc)

	rec := postJSON(t, mux, "/api/bulletins", map[string]string{
		"slot": "BLN0",
		"text": "Net tonight at 2000z",
	})

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.BulletinResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 42 {
		t.Errorf("ID: got %d, want 42", resp.ID)
	}
	if captured.Slot != "BLN0" {
		t.Errorf("captured Slot: %q", captured.Slot)
	}
}

func TestSendBulletin_InvalidSlot(t *testing.T) {
	_, mux := newBulletinTestServer(t, &fakeBulletinSvc{})
	rec := postJSON(t, mux, "/api/bulletins", map[string]string{"slot": "BLN", "text": "hi"})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid slot, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSendBulletin_EmptyText(t *testing.T) {
	_, mux := newBulletinTestServer(t, &fakeBulletinSvc{})
	rec := postJSON(t, mux, "/api/bulletins", map[string]string{"slot": "BLN0", "text": ""})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty text, got %d", rec.Code)
	}
}

func TestSendBulletin_TextTooLong(t *testing.T) {
	_, mux := newBulletinTestServer(t, &fakeBulletinSvc{})
	long := ""
	for i := 0; i < 68; i++ {
		long += "x"
	}
	rec := postJSON(t, mux, "/api/bulletins", map[string]string{"slot": "BLN0", "text": long})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for long text, got %d", rec.Code)
	}
}

func TestSendBulletin_IntervalMins_PassedThrough(t *testing.T) {
	var captured bulletins.SendRequest
	svc := &fakeBulletinSvc{
		sendFn: func(_ context.Context, req bulletins.SendRequest) (*configstore.Bulletin, error) {
			captured = req
			return &configstore.Bulletin{ID: 1, Slot: req.Slot, Text: req.Text, Direction: "out", IntervalMins: req.IntervalMins}, nil
		},
	}
	_, mux := newBulletinTestServer(t, svc)

	rec := postJSON(t, mux, "/api/bulletins", map[string]any{
		"slot":          "BLN0",
		"text":          "Net tonight",
		"interval_mins": 10,
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	if captured.IntervalMins != 10 {
		t.Errorf("IntervalMins: got %d, want 10", captured.IntervalMins)
	}
	var resp dto.BulletinResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.IntervalMins != 10 {
		t.Errorf("response IntervalMins: got %d, want 10", resp.IntervalMins)
	}
}

func TestSendBulletin_IntervalMins_OutOfRange(t *testing.T) {
	_, mux := newBulletinTestServer(t, &fakeBulletinSvc{})
	rec := postJSON(t, mux, "/api/bulletins", map[string]any{
		"slot": "BLN0", "text": "hi", "interval_mins": 21,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for interval_mins=21, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// DELETE /api/bulletins/{id}
// ---------------------------------------------------------------------------

func TestDeleteBulletin_OK(t *testing.T) {
	var deletedID uint64
	svc := &fakeBulletinSvc{
		deleteFn: func(_ context.Context, id uint64) error {
			deletedID = id
			return nil
		},
	}
	_, mux := newBulletinTestServer(t, svc)

	req := httptest.NewRequest(http.MethodDelete, "/api/bulletins/7", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if deletedID != 7 {
		t.Errorf("deletedID: got %d, want 7", deletedID)
	}
}

func TestDeleteBulletin_NotFound(t *testing.T) {
	svc := &fakeBulletinSvc{
		deleteFn: func(_ context.Context, _ uint64) error {
			return gorm.ErrRecordNotFound
		},
	}
	_, mux := newBulletinTestServer(t, svc)

	req := httptest.NewRequest(http.MethodDelete, "/api/bulletins/99", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteBulletin_InvalidID(t *testing.T) {
	_, mux := newBulletinTestServer(t, &fakeBulletinSvc{})
	req := httptest.NewRequest(http.MethodDelete, "/api/bulletins/abc", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for non-numeric id, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/bulletins/{id}/read
// ---------------------------------------------------------------------------

func TestMarkBulletinRead_OK(t *testing.T) {
	var readID uint64
	svc := &fakeBulletinSvc{
		markReadFn: func(_ context.Context, id uint64) error {
			readID = id
			return nil
		},
	}
	_, mux := newBulletinTestServer(t, svc)

	req := httptest.NewRequest(http.MethodPost, "/api/bulletins/3/read", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if readID != 3 {
		t.Errorf("readID: got %d, want 3", readID)
	}
}

func TestMarkBulletinRead_Error(t *testing.T) {
	svc := &fakeBulletinSvc{
		markReadFn: func(_ context.Context, _ uint64) error {
			return errors.New("db error")
		},
	}
	_, mux := newBulletinTestServer(t, svc)

	req := httptest.NewRequest(http.MethodPost, "/api/bulletins/3/read", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected 500, got %d", rec.Code)
	}
}

// ---------------------------------------------------------------------------
// POST /api/bulletins/read-all
// ---------------------------------------------------------------------------

func TestMarkAllBulletinsRead_OK(t *testing.T) {
	called := false
	svc := &fakeBulletinSvc{
		markAllReadFn: func(_ context.Context) error {
			called = true
			return nil
		},
	}
	_, mux := newBulletinTestServer(t, svc)

	req := httptest.NewRequest(http.MethodPost, "/api/bulletins/read-all", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	if !called {
		t.Error("MarkAllRead was not called")
	}
}
