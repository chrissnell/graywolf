package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// --- test fixtures -------------------------------------------------------

// fakeMessagesSvc is a minimal MessagesService stub for handler tests.
// Every method is individually overridable so each test controls only
// the paths it exercises.
type fakeMessagesSvc struct {
	sendFn           func(ctx context.Context, req messages.SendMessageRequest) (*configstore.Message, error)
	resendFn         func(ctx context.Context, id uint64) (messages.SendResult, error)
	softDeleteFn     func(ctx context.Context, id uint64) error
	markReadFn       func(ctx context.Context, id uint64) error
	markUnreadFn     func(ctx context.Context, id uint64) error
	reloadTacticalFn func(ctx context.Context) error
	reloadPrefsFn    func(ctx context.Context) error
	hub              *messages.EventHub
}

func (f *fakeMessagesSvc) SendMessage(ctx context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
	if f.sendFn != nil {
		return f.sendFn(ctx, req)
	}
	return nil, errors.New("sendFn not set")
}
func (f *fakeMessagesSvc) Resend(ctx context.Context, id uint64) (messages.SendResult, error) {
	if f.resendFn != nil {
		return f.resendFn(ctx, id)
	}
	return messages.SendResult{}, errors.New("resendFn not set")
}
func (f *fakeMessagesSvc) SoftDelete(ctx context.Context, id uint64) error {
	if f.softDeleteFn != nil {
		return f.softDeleteFn(ctx, id)
	}
	return nil
}
func (f *fakeMessagesSvc) MarkRead(ctx context.Context, id uint64) error {
	if f.markReadFn != nil {
		return f.markReadFn(ctx, id)
	}
	return nil
}
func (f *fakeMessagesSvc) MarkUnread(ctx context.Context, id uint64) error {
	if f.markUnreadFn != nil {
		return f.markUnreadFn(ctx, id)
	}
	return nil
}
func (f *fakeMessagesSvc) ReloadTacticalCallsigns(ctx context.Context) error {
	if f.reloadTacticalFn != nil {
		return f.reloadTacticalFn(ctx)
	}
	return nil
}
func (f *fakeMessagesSvc) ReloadPreferences(ctx context.Context) error {
	if f.reloadPrefsFn != nil {
		return f.reloadPrefsFn(ctx)
	}
	return nil
}
func (f *fakeMessagesSvc) EventHub() *messages.EventHub {
	if f.hub == nil {
		f.hub = messages.NewEventHub(16)
	}
	return f.hub
}

// newMessagesTestServer constructs a webapi Server + mux + concrete
// messages store backed by an in-memory DB + the fake service, wired
// together. Every messages test starts from the same fixture.
func newMessagesTestServer(t *testing.T, svc MessagesService) (*Server, *http.ServeMux, *messages.Store) {
	t.Helper()
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	// Seed an iGate config.
	if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Server:     "rotate.aprs2.net",
		Port:       14580,
		TxChannel:  1,
		RfChannel:  1,
		MaxMsgHops: 2,
		GateRfToIs: true,
	}); err != nil {
		t.Fatal(err)
	}
	// resolveOurCall now reads StationConfig, not IGateConfig.
	if err := store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CALL"}); err != nil {
		t.Fatal(err)
	}

	msgStore := messages.NewStore(store.DB())

	srv, err := NewServer(Config{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetMessagesService(svc)
	srv.SetMessagesStore(msgStore)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	return srv, mux, msgStore
}

// insertMessage is a test helper to persist a fixture message row.
func insertMessage(t *testing.T, store *messages.Store, m *configstore.Message) {
	t.Helper()
	if err := store.Insert(context.Background(), m); err != nil {
		t.Fatal(err)
	}
}

// --- GET /api/messages ---------------------------------------------------

func TestListMessages_HappyPath(t *testing.T) {
	svc := &fakeMessagesSvc{}
	_, mux, store := newMessagesTestServer(t, svc)

	insertMessage(t, store, &configstore.Message{
		Direction: "in", OurCall: "N0CALL", FromCall: "W1ABC", ToCall: "N0CALL",
		ThreadKind: messages.ThreadKindDM, Text: "hi", Source: "rf",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.MessageListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Changes) != 1 || resp.Changes[0].Message.Text != "hi" {
		t.Errorf("unexpected response: %+v", resp)
	}
}

func TestListMessages_BadSince(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/messages?since=not-a-timestamp", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListMessages_BadLimit(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/messages?limit=0", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestListMessages_CursorRoundTrip(t *testing.T) {
	_, mux, store := newMessagesTestServer(t, &fakeMessagesSvc{})
	// Insert 3 rows.
	for i := 0; i < 3; i++ {
		insertMessage(t, store, &configstore.Message{
			Direction: "in", OurCall: "N0CALL", FromCall: "W1ABC", ToCall: "N0CALL",
			ThreadKind: messages.ThreadKindDM, Text: fmt.Sprintf("hi-%d", i), Source: "rf",
		})
	}
	req := httptest.NewRequest(http.MethodGet, "/api/messages?limit=2", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var resp dto.MessageListResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Cursor == "" {
		t.Fatal("expected cursor on partial page")
	}
	// Reuse the cursor.
	req2 := httptest.NewRequest(http.MethodGet, "/api/messages?cursor="+resp.Cursor, nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on paged GET, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// --- GET /api/messages/{id} ----------------------------------------------

func TestGetMessage_NotFound(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/messages/999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestGetMessage_BadID(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/messages/notanumber", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetMessage_Success(t *testing.T) {
	_, mux, store := newMessagesTestServer(t, &fakeMessagesSvc{})
	m := &configstore.Message{
		Direction: "in", OurCall: "N0CALL", FromCall: "W1ABC", ToCall: "N0CALL",
		ThreadKind: messages.ThreadKindDM, Text: "pong",
	}
	insertMessage(t, store, m)
	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/messages/%d", m.ID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}

// --- POST /api/messages --------------------------------------------------

func TestSendMessage_202(t *testing.T) {
	sentCh := make(chan messages.SendMessageRequest, 1)
	svc := &fakeMessagesSvc{
		sendFn: func(ctx context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
			sentCh <- req
			return &configstore.Message{
				ID:         42,
				Direction:  "out",
				OurCall:    req.OurCall,
				FromCall:   req.OurCall,
				ToCall:     req.To,
				Text:       req.Text,
				ThreadKind: messages.ThreadKindDM,
				ThreadKey:  req.To,
				MsgID:      "001",
				CreatedAt:  time.Now(),
				AckState:   messages.AckStateNone,
			}, nil
		},
	}
	_, mux, _ := newMessagesTestServer(t, svc)

	body := `{"to":"W1ABC","text":"hi","client_id":"abc-123"}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.MessageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.ID != 42 {
		t.Errorf("expected id=42, got %d", resp.ID)
	}
	got := <-sentCh
	if got.To != "W1ABC" || got.Text != "hi" {
		t.Errorf("unexpected send req: %+v", got)
	}
}

func TestSendMessage_BadAddressee(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	body := `{"to":"","text":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSendMessage_TextTooLong(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	text := strings.Repeat("x", 68)
	body := fmt.Sprintf(`{"to":"W1ABC","text":"%s"}`, text)
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestSendMessage_LoopbackGuard(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	// seed's ourcall is "N0CALL"; sending to ourselves should 400.
	body := `{"to":"N0CALL","text":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for loopback, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSendMessage_MissingStationCallsign(t *testing.T) {
	svc := &fakeMessagesSvc{
		sendFn: func(ctx context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
			t.Error("service SendMessage must NOT be called when station callsign is unset")
			return nil, nil
		},
	}
	srv, mux, _ := newMessagesTestServer(t, svc)
	// Clear the seeded callsign so resolveOurCall returns "".
	if err := srv.store.UpsertStationConfig(context.Background(), configstore.StationConfig{Callsign: ""}); err != nil {
		t.Fatal(err)
	}
	body := `{"to":"W1ABC","text":"hi"}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSendMessage_UnknownField(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	body := `{"to":"W1ABC","text":"hi","unknown":"yes"}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for unknown field, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestSendMessage_Unavailable(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, nil)
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(`{"to":"W1ABC","text":"hi"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

// TestSendMessage_InviteRoundTrip asserts that a POST with
// kind=invite + invite_tactical passes the fields through to the
// service layer unchanged. The fake service records the call so we
// can assert on the forwarded struct.
func TestSendMessage_InviteRoundTrip(t *testing.T) {
	sentCh := make(chan messages.SendMessageRequest, 1)
	svc := &fakeMessagesSvc{
		sendFn: func(ctx context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
			sentCh <- req
			return &configstore.Message{
				ID:             99,
				Direction:      "out",
				OurCall:        req.OurCall,
				FromCall:       req.OurCall,
				ToCall:         req.To,
				Text:           "!GW1 INVITE TAC-NET",
				ThreadKind:     messages.ThreadKindDM,
				ThreadKey:      req.To,
				MsgID:          "002",
				CreatedAt:      time.Now(),
				AckState:       messages.AckStateNone,
				Kind:           messages.MessageKindInvite,
				InviteTactical: "TAC-NET",
			}, nil
		},
	}
	_, mux, _ := newMessagesTestServer(t, svc)

	body := `{"to":"W1ABC","text":"","kind":"invite","invite_tactical":"TAC-NET"}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	got := <-sentCh
	if got.Kind != messages.MessageKindInvite {
		t.Errorf("Kind forwarded = %q, want %q", got.Kind, messages.MessageKindInvite)
	}
	if got.InviteTactical != "TAC-NET" {
		t.Errorf("InviteTactical forwarded = %q, want TAC-NET", got.InviteTactical)
	}
	// Response echoes the server-built wire body.
	var resp dto.MessageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Text != "!GW1 INVITE TAC-NET" {
		t.Errorf("response Text = %q, want the server-built wire body", resp.Text)
	}
	if resp.Kind != messages.MessageKindInvite {
		t.Errorf("response Kind = %q, want %q", resp.Kind, messages.MessageKindInvite)
	}
}

// TestSendMessage_InviteMissingTactical asserts that a kind=invite
// request without invite_tactical is rejected at the webapi
// boundary with 400, not forwarded to the service.
func TestSendMessage_InviteMissingTactical(t *testing.T) {
	called := false
	svc := &fakeMessagesSvc{
		sendFn: func(ctx context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
			called = true
			return nil, nil
		},
	}
	_, mux, _ := newMessagesTestServer(t, svc)

	body := `{"to":"W1ABC","text":"","kind":"invite"}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", rec.Code, rec.Body.String())
	}
	if called {
		t.Error("service SendMessage must NOT be called when invite_tactical is missing")
	}
}

// TestSendMessage_InviteEndToEndPersistsRow wires the real messages
// Service + Store and verifies that a kind=invite POST results in a
// persisted row with Kind=invite, InviteTactical=TAC-NET, and the
// server-built wire body — i.e., the full round trip through the
// send path.
func TestSendMessage_InviteEndToEndPersistsRow(t *testing.T) {
	ctx := context.Background()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	if err := store.UpsertIGateConfig(ctx, &configstore.IGateConfig{
		Server: "rotate.aprs2.net", Port: 14580,
		TxChannel: 1, RfChannel: 1, MaxMsgHops: 2, GateRfToIs: true,
	}); err != nil {
		t.Fatal(err)
	}
	// Station callsign lives in its own singleton as of the centralized
	// station-callsign work.
	if err := store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CALL"}); err != nil {
		t.Fatal(err)
	}
	msgStore := messages.NewStore(store.DB())
	svc, err := messages.NewService(messages.ServiceConfig{
		Store:       msgStore,
		ConfigStore: store,
		TxSink:      &inviteFakeSink{},
		TxHookReg:   &inviteNopHookReg{},
		OurCall:     func() string { return "N0CALL" },
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := svc.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer svc.Stop()

	srv, err := NewServer(Config{
		Store:  store,
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	srv.SetMessagesService(svc)
	srv.SetMessagesStore(msgStore)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Client deliberately sends a bogus Text to prove the server
	// overwrites it.
	body := `{"to":"W1ABC","text":"IGNORED","kind":"invite","invite_tactical":"TAC-NET"}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}

	// Pull the row back and assert persisted state.
	rows, _, err := msgStore.List(ctx, messages.Filter{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 persisted row, got %d", len(rows))
	}
	row := rows[0]
	if row.Kind != messages.MessageKindInvite {
		t.Errorf("Kind = %q, want %q", row.Kind, messages.MessageKindInvite)
	}
	if row.InviteTactical != "TAC-NET" {
		t.Errorf("InviteTactical = %q, want TAC-NET", row.InviteTactical)
	}
	if want := "!GW1 INVITE TAC-NET"; row.Text != want {
		t.Errorf("persisted Text = %q, want %q (server must overwrite the client body)", row.Text, want)
	}
}

// inviteFakeSink + inviteNopHookReg are minimal stubs so the Service
// can be constructed without dragging in the full txgovernor wiring.
// They intentionally discard submissions — the invite tests care
// about persistence, not radio I/O.
type inviteFakeSink struct{}

func (inviteFakeSink) Submit(ctx context.Context, ch uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	return nil
}

type inviteNopHookReg struct{}

func (inviteNopHookReg) AddTxHook(h txgovernor.TxHook) (uint64, func()) {
	return 0, func() {}
}

// --- DELETE /api/messages/{id} -------------------------------------------

func TestDeleteMessage_204(t *testing.T) {
	deleted := make(chan uint64, 1)
	svc := &fakeMessagesSvc{
		softDeleteFn: func(ctx context.Context, id uint64) error {
			deleted <- id
			return nil
		},
	}
	_, mux, store := newMessagesTestServer(t, svc)
	m := &configstore.Message{
		Direction: "out", OurCall: "N0CALL", FromCall: "N0CALL", ToCall: "W1ABC",
		ThreadKind: messages.ThreadKindDM, MsgID: "001", Text: "hi",
	}
	insertMessage(t, store, m)
	req := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/messages/%d", m.ID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case id := <-deleted:
		if id != m.ID {
			t.Errorf("expected id=%d, got %d", m.ID, id)
		}
	case <-time.After(time.Second):
		t.Error("softDelete was not called")
	}
}

func TestDeleteMessage_NotFound(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	req := httptest.NewRequest(http.MethodDelete, "/api/messages/999", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

// --- POST /api/messages/{id}/read | /unread -------------------------------

func TestMarkRead_204(t *testing.T) {
	marked := make(chan uint64, 1)
	svc := &fakeMessagesSvc{
		markReadFn: func(ctx context.Context, id uint64) error {
			marked <- id
			return nil
		},
	}
	_, mux, _ := newMessagesTestServer(t, svc)
	req := httptest.NewRequest(http.MethodPost, "/api/messages/7/read", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	select {
	case id := <-marked:
		if id != 7 {
			t.Errorf("expected id=7, got %d", id)
		}
	case <-time.After(time.Second):
		t.Error("markRead was not called")
	}
}

func TestMarkUnread_204(t *testing.T) {
	marked := make(chan uint64, 1)
	svc := &fakeMessagesSvc{
		markUnreadFn: func(ctx context.Context, id uint64) error {
			marked <- id
			return nil
		},
	}
	_, mux, _ := newMessagesTestServer(t, svc)
	req := httptest.NewRequest(http.MethodPost, "/api/messages/8/unread", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", rec.Code)
	}
	select {
	case <-marked:
	case <-time.After(time.Second):
		t.Error("markUnread was not called")
	}
}

// --- POST /api/messages/{id}/resend --------------------------------------

func TestResend_ConflictOnInProgress(t *testing.T) {
	svc := &fakeMessagesSvc{}
	_, mux, store := newMessagesTestServer(t, svc)
	// Fresh outbound row — Attempts==0, no NextRetryAt → pending.
	m := &configstore.Message{
		Direction: "out", OurCall: "N0CALL", FromCall: "N0CALL", ToCall: "W1ABC",
		ThreadKind: messages.ThreadKindDM, MsgID: "001", Text: "hi",
		AckState: messages.AckStateNone,
	}
	insertMessage(t, store, m)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/messages/%d/resend", m.ID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for pending row, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResend_NotFound(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	req := httptest.NewRequest(http.MethodPost, "/api/messages/999/resend", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestResend_InboundConflict(t *testing.T) {
	_, mux, store := newMessagesTestServer(t, &fakeMessagesSvc{})
	m := &configstore.Message{
		Direction: "in", OurCall: "N0CALL", FromCall: "W1ABC", ToCall: "N0CALL",
		ThreadKind: messages.ThreadKindDM, Text: "bye",
	}
	insertMessage(t, store, m)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/messages/%d/resend", m.ID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for inbound, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestResend_HappyPath(t *testing.T) {
	resendCalled := make(chan uint64, 1)
	svc := &fakeMessagesSvc{
		resendFn: func(ctx context.Context, id uint64) (messages.SendResult, error) {
			resendCalled <- id
			return messages.SendResult{Path: messages.SendPathRF, Retryable: true}, nil
		},
	}
	_, mux, store := newMessagesTestServer(t, svc)
	// Terminal-failed row → eligible for resend.
	m := &configstore.Message{
		Direction: "out", OurCall: "N0CALL", FromCall: "N0CALL", ToCall: "W1ABC",
		ThreadKind: messages.ThreadKindDM, MsgID: "001", Text: "hi",
		AckState:      messages.AckStateRejected,
		FailureReason: "retry budget exhausted",
	}
	insertMessage(t, store, m)
	req := httptest.NewRequest(http.MethodPost, fmt.Sprintf("/api/messages/%d/resend", m.ID), nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-resendCalled:
	case <-time.After(time.Second):
		t.Error("resendFn was not called")
	}
}

// --- GET /api/messages/conversations -------------------------------------

func TestListConversations_HappyPath(t *testing.T) {
	_, mux, store := newMessagesTestServer(t, &fakeMessagesSvc{})
	insertMessage(t, store, &configstore.Message{
		Direction: "in", OurCall: "N0CALL", FromCall: "W1ABC", ToCall: "N0CALL",
		ThreadKind: messages.ThreadKindDM, Text: "hi",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/messages/conversations", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp []dto.ConversationSummary
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if len(resp) != 1 || resp[0].ThreadKey != "W1ABC" {
		t.Errorf("unexpected summaries: %+v", resp)
	}
}

// --- Preferences ---------------------------------------------------------

func TestGetPreferences_SeededDefaults(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/messages/preferences", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var resp dto.MessagePreferencesResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.FallbackPolicy == "" {
		t.Errorf("expected default policy, got empty")
	}
}

func TestPutPreferences_Validation(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	body := `{"fallback_policy":"nope","default_path":"WIDE1-1","retry_max_attempts":3,"retention_days":0}`
	req := httptest.NewRequest(http.MethodPut, "/api/messages/preferences", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestPutPreferences_RoundTrip(t *testing.T) {
	reloaded := make(chan struct{}, 1)
	svc := &fakeMessagesSvc{
		reloadPrefsFn: func(ctx context.Context) error {
			select {
			case reloaded <- struct{}{}:
			default:
			}
			return nil
		},
	}
	_, mux, _ := newMessagesTestServer(t, svc)
	body := `{"fallback_policy":"rf_only","default_path":"WIDE2-2","retry_max_attempts":3,"retention_days":30}`
	req := httptest.NewRequest(http.MethodPut, "/api/messages/preferences", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Error("ReloadPreferences was not called")
	}
}

// --- Tactical CRUD -------------------------------------------------------

func TestCreateTactical_201(t *testing.T) {
	reloaded := make(chan struct{}, 1)
	svc := &fakeMessagesSvc{
		reloadTacticalFn: func(ctx context.Context) error {
			select {
			case reloaded <- struct{}{}:
			default:
			}
			return nil
		},
	}
	_, mux, _ := newMessagesTestServer(t, svc)
	body := `{"callsign":"NET1","alias":"Net Control","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages/tactical", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Error("ReloadTacticalCallsigns was not called")
	}
}

func TestCreateTactical_BotCollision(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	body := `{"callsign":"sms","alias":"My SMS","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages/tactical", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bot collision, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "well-known APRS service address") {
		t.Errorf("expected helpful bot-collision message, got: %s", rec.Body.String())
	}
}

func TestCreateTactical_DuplicateConflict(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	body := `{"callsign":"NET1","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/messages/tactical", strings.NewReader(body))
	mux.ServeHTTP(httptest.NewRecorder(), req)

	// Second insert should conflict.
	req2 := httptest.NewRequest(http.MethodPost, "/api/messages/tactical", strings.NewReader(body))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusConflict {
		t.Fatalf("expected 409 on duplicate, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

func TestUpdateTactical_NotFound(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	body := `{"callsign":"NET1","enabled":true}`
	req := httptest.NewRequest(http.MethodPut, "/api/messages/tactical/999", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestDeleteTactical_204(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	// Create first.
	req := httptest.NewRequest(http.MethodPost, "/api/messages/tactical", strings.NewReader(`{"callsign":"NET1","enabled":true}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	var created dto.TacticalCallsignResponse
	_ = json.NewDecoder(rec.Body).Decode(&created)

	req2 := httptest.NewRequest(http.MethodDelete, fmt.Sprintf("/api/messages/tactical/%d", created.ID), nil)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d: %s", rec2.Code, rec2.Body.String())
	}
}

// --- Participants --------------------------------------------------------

func TestGetParticipants_HappyPath(t *testing.T) {
	_, mux, store := newMessagesTestServer(t, &fakeMessagesSvc{})
	insertMessage(t, store, &configstore.Message{
		Direction: "in", OurCall: "N0CALL", FromCall: "W1ABC", ToCall: "NET1",
		ThreadKind: messages.ThreadKindTactical, Text: "check in",
	})
	req := httptest.NewRequest(http.MethodGet, "/api/messages/tactical/NET1/participants?within=7d", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var env dto.ParticipantsEnvelope
	if err := json.NewDecoder(rec.Body).Decode(&env); err != nil {
		t.Fatal(err)
	}
	if env.EffectiveWithinDays != 7 {
		t.Errorf("expected 7-day window, got %d", env.EffectiveWithinDays)
	}
	if len(env.Participants) != 1 || env.Participants[0].Callsign != "W1ABC" {
		t.Errorf("unexpected participants: %+v", env.Participants)
	}
}

func TestGetParticipants_BadWithin(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	req := httptest.NewRequest(http.MethodGet, "/api/messages/tactical/NET1/participants?within=bad", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

// --- SSE -----------------------------------------------------------------

func TestStreamMessageEvents_SendsEvents(t *testing.T) {
	hub := messages.NewEventHub(4)
	svc := &fakeMessagesSvc{hub: hub}
	_, mux, _ := newMessagesTestServer(t, svc)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/api/messages/events", nil)
	rw := newFlushRecorder()

	done := make(chan struct{})
	go func() {
		defer close(done)
		mux.ServeHTTP(rw, req)
	}()

	// Wait for "connected" comment.
	waitForBytes(t, rw, ": connected", 2*time.Second)

	// Publish a fake event; we don't have a corresponding row, so the
	// handler should still emit a frame with the event type and a
	// MessageChange without the message body.
	hub.Publish(messages.Event{
		Type: messages.EventMessageReceived, MessageID: 42,
		ThreadKind: messages.ThreadKindDM, ThreadKey: "W1ABC",
	})
	waitForBytes(t, rw, "event: message.received", 2*time.Second)

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("SSE handler did not return after cancel")
	}
}

// flushRecorder is a tiny httptest.ResponseRecorder analog that supports
// http.Flusher. ResponseRecorder implements Flusher since Go 1.21; kept
// this abstraction so the wait helper has a safe concurrent read surface.
type flushRecorder struct {
	mu     sync.Mutex
	buf    bytes.Buffer
	code   int
	header http.Header
}

func newFlushRecorder() *flushRecorder {
	return &flushRecorder{header: http.Header{}, code: http.StatusOK}
}

func (r *flushRecorder) Header() http.Header  { return r.header }
func (r *flushRecorder) WriteHeader(code int) { r.code = code }
func (r *flushRecorder) Flush()               {}
func (r *flushRecorder) Code() int            { return r.code }
func (r *flushRecorder) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.Write(p)
}
func (r *flushRecorder) body() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.buf.String()
}

// waitForBytes polls the recorder's body for needle until found or
// timeout expires.
func waitForBytes(t *testing.T, r *flushRecorder, needle string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if strings.Contains(r.body(), needle) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("did not see %q in SSE body within %v; body=%q", needle, timeout, r.body())
}

// --- SetMessagesReload plumbing -----------------------------------------

func TestSetMessagesReload_NonBlockingSignal(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	ch := make(chan struct{}, 1)
	// Install via setter — mirrors wiring.
	srv, _, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	srv.SetMessagesReload(ch)
	srv.signalMessagesReload()
	srv.signalMessagesReload() // second send coalesces — must not block
	select {
	case <-ch:
	default:
		t.Fatal("expected one value queued")
	}
	// Channel must not be full now.
	select {
	case ch <- struct{}{}:
	default:
		t.Fatal("channel unexpectedly full")
	}
	_ = mux // mux above is unused for this test
}

// TestSignalTacticalChanged_FansOutBothSignals confirms the dual-signal
// helper populates both the messages and igate reload channels in a
// single call, and that repeated calls coalesce into at most one
// pending signal per channel (best-effort, non-blocking).
func TestSignalTacticalChanged_FansOutBothSignals(t *testing.T) {
	srv, _, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	msgCh := make(chan struct{}, 1)
	igCh := make(chan struct{}, 1)
	srv.SetMessagesReload(msgCh)
	srv.SetIgateReload(igCh)

	srv.signalTacticalChanged()

	select {
	case <-msgCh:
	default:
		t.Fatal("expected messages reload signal")
	}
	select {
	case <-igCh:
	default:
		t.Fatal("expected igate reload signal")
	}

	// Two back-to-back calls with no drain in between must coalesce: at
	// most one pending signal per channel, neither send must block.
	srv.signalTacticalChanged()
	srv.signalTacticalChanged()
	if len(msgCh) != 1 {
		t.Fatalf("messages channel coalescing broken: len=%d, want 1", len(msgCh))
	}
	if len(igCh) != 1 {
		t.Fatalf("igate channel coalescing broken: len=%d, want 1", len(igCh))
	}
	<-msgCh
	<-igCh
	if len(msgCh) != 0 || len(igCh) != 0 {
		t.Fatalf("leftover signals after drain: msg=%d ig=%d", len(msgCh), len(igCh))
	}
}

// --- guardrail — sanity check that the route table is wired -----------

func TestMessagesRoutes_Registered(t *testing.T) {
	_, mux, _ := newMessagesTestServer(t, &fakeMessagesSvc{})
	// A missing service endpoint must still produce a 2xx-shaped
	// response from the mux (we're checking the route exists, not the
	// handler logic). Use GET /api/messages which takes no required
	// path params.
	req := httptest.NewRequest(http.MethodGet, "/api/messages", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("GET /api/messages not registered")
	}
}

// Sanity — gorm.ErrRecordNotFound is re-exported so the checker's
// import survives a refactor.
var _ = gorm.ErrRecordNotFound
