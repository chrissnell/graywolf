package webapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// newTacticalsTestServer wires a Server with the real messages Store,
// a fake MessagesService (so we can observe ReloadTacticalCallsigns
// + EventHub), and the tacticals route registered.
func newTacticalsTestServer(t *testing.T) (*Server, *http.ServeMux, *messages.Store, *fakeMessagesSvc) {
	t.Helper()
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
	// station-callsign work (see pkg/callsign, pkg/configstore.StationConfig).
	if err := store.UpsertStationConfig(ctx, configstore.StationConfig{Callsign: "N0CALL"}); err != nil {
		t.Fatal(err)
	}
	msgStore := messages.NewStore(store.DB())

	svc := &fakeMessagesSvc{}
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
	return srv, mux, msgStore, svc
}

// TestAcceptTacticalInvite_CreatesNewSubscription asserts that POST
// /api/tacticals against an unknown tactical creates a new enabled
// row and returns AlreadyMember=false.
func TestAcceptTacticalInvite_CreatesNewSubscription(t *testing.T) {
	srv, mux, _, svc := newTacticalsTestServer(t)

	reloaded := make(chan struct{}, 1)
	svc.reloadTacticalFn = func(ctx context.Context) error {
		select {
		case reloaded <- struct{}{}:
		default:
		}
		return nil
	}

	body := `{"callsign":"TAC-NET"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.AcceptInviteResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Tactical.Callsign != "TAC-NET" {
		t.Errorf("Tactical.Callsign = %q, want TAC-NET", resp.Tactical.Callsign)
	}
	if !resp.Tactical.Enabled {
		t.Error("Tactical.Enabled must be true on fresh accept")
	}
	if resp.AlreadyMember {
		t.Error("AlreadyMember must be false on first accept")
	}

	// Verify persisted.
	row, err := srv.store.GetTacticalCallsignByCallsign(context.Background(), "TAC-NET")
	if err != nil {
		t.Fatalf("lookup after accept: %v", err)
	}
	if row == nil || !row.Enabled {
		t.Fatalf("expected persisted enabled row; got %+v", row)
	}

	select {
	case <-reloaded:
	case <-time.After(time.Second):
		t.Error("ReloadTacticalCallsigns was not called")
	}
}

// TestAcceptTacticalInvite_IdempotentOnAlreadyEnabled verifies that a
// second POST against an already-enabled subscription returns 200
// with AlreadyMember=true (NOT 409 — the plan forbids 409 on this
// path).
func TestAcceptTacticalInvite_IdempotentOnAlreadyEnabled(t *testing.T) {
	_, mux, _, _ := newTacticalsTestServer(t)

	body := `{"callsign":"TAC-NET"}`
	// First accept.
	req1 := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	mux.ServeHTTP(httptest.NewRecorder(), req1)

	// Second accept.
	req2 := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on second accept, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var resp dto.AcceptInviteResponse
	if err := json.NewDecoder(rec2.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if !resp.AlreadyMember {
		t.Error("AlreadyMember must be true on second accept of the same tactical")
	}
}

// TestAcceptTacticalInvite_EnablesDisabledPreservesAlias verifies that
// accepting for a row that exists but is Enabled=false flips it to
// Enabled=true without clobbering the existing Alias.
func TestAcceptTacticalInvite_EnablesDisabledPreservesAlias(t *testing.T) {
	srv, mux, _, _ := newTacticalsTestServer(t)

	ctx := context.Background()
	pre := &configstore.TacticalCallsign{
		Callsign: "TAC-NET",
		Alias:    "Old Name",
		Enabled:  false,
	}
	if err := srv.store.CreateTacticalCallsign(ctx, pre); err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := `{"callsign":"TAC-NET"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.AcceptInviteResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if !resp.Tactical.Enabled {
		t.Error("Enabled must be true post-accept")
	}
	if resp.Tactical.Alias != "Old Name" {
		t.Errorf("Alias = %q, want preserved %q", resp.Tactical.Alias, "Old Name")
	}
	if resp.AlreadyMember {
		t.Error("AlreadyMember must be false — row existed but was disabled")
	}

	// Verify persisted state.
	row, _ := srv.store.GetTacticalCallsignByCallsign(ctx, "TAC-NET")
	if row == nil || !row.Enabled || row.Alias != "Old Name" {
		t.Errorf("unexpected persisted row: %+v", row)
	}
}

// TestAcceptTacticalInvite_MalformedCallsign returns 400 for inputs
// that don't match the 1-9 [A-Z0-9-] grammar.
func TestAcceptTacticalInvite_MalformedCallsign(t *testing.T) {
	_, mux, _, _ := newTacticalsTestServer(t)

	cases := []struct {
		name string
		call string
	}{
		{"empty", ""},
		{"too long", "TOOLONGTAC"},
		{"underscore", "TAC_NET"},
		{"space", "TAC NET"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := fmt.Sprintf(`{"callsign":%q}`, tc.call)
			req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("%s: expected 400, got %d: %s", tc.name, rec.Code, rec.Body.String())
			}
		})
	}
}

// TestAcceptTacticalInvite_LowercaseNormalizedToUpper verifies that a
// lowercase callsign in the request is uppercased before validation
// + persist. The wire regex is uppercase-only but the handler is
// tolerant on input.
func TestAcceptTacticalInvite_LowercaseNormalizedToUpper(t *testing.T) {
	srv, mux, _, _ := newTacticalsTestServer(t)

	body := `{"callsign":"tac-net"}`
	req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	row, err := srv.store.GetTacticalCallsignByCallsign(context.Background(), "TAC-NET")
	if err != nil || row == nil {
		t.Fatalf("expected persisted row under uppercase key; err=%v row=%+v", err, row)
	}
}

// TestAcceptTacticalInvite_NoSourceMessageStillSubscribes verifies
// that omitting SourceMessageID (or sending 0) still results in a
// successful subscribe — the message link is optional audit info.
func TestAcceptTacticalInvite_NoSourceMessageStillSubscribes(t *testing.T) {
	srv, mux, _, _ := newTacticalsTestServer(t)

	body := `{"callsign":"TAC-NET","source_message_id":0}`
	req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	row, _ := srv.store.GetTacticalCallsignByCallsign(context.Background(), "TAC-NET")
	if row == nil || !row.Enabled {
		t.Errorf("expected subscription even without source_message_id; got %+v", row)
	}
}

// TestAcceptTacticalInvite_StampsSourceMessageAuditFields verifies
// that when SourceMessageID points to a valid inbound invite for the
// matching tactical, the handler sets InviteAcceptedAt for audit.
func TestAcceptTacticalInvite_StampsSourceMessageAuditFields(t *testing.T) {
	srv, mux, msgStore, svc := newTacticalsTestServer(t)
	// EventHub must exist for the handler to publish on.
	_ = svc.EventHub()

	// Seed an inbound invite row for TAC-NET.
	inv := &configstore.Message{
		Direction:      "in",
		OurCall:        "N0CALL",
		FromCall:       "W1ABC",
		ToCall:         "N0CALL",
		Text:           "!GW1 INVITE TAC-NET",
		ThreadKind:     messages.ThreadKindDM,
		ThreadKey:      "W1ABC",
		Kind:           messages.MessageKindInvite,
		InviteTactical: "TAC-NET",
		Source:         "rf",
		Unread:         true,
	}
	insertMessage(t, msgStore, inv)

	body := fmt.Sprintf(`{"callsign":"TAC-NET","source_message_id":%d}`, inv.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Re-read and assert stamp.
	got, err := msgStore.GetByID(context.Background(), inv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.InviteAcceptedAt == nil {
		t.Fatal("InviteAcceptedAt must be set after accept")
	}
	firstStamp := *got.InviteAcceptedAt

	// Verify subscription exists.
	row, _ := srv.store.GetTacticalCallsignByCallsign(context.Background(), "TAC-NET")
	if row == nil || !row.Enabled {
		t.Fatalf("subscription missing post-accept: %+v", row)
	}

	// Idempotency: re-posting the same accept must not re-stamp or
	// change state, and must return AlreadyMember=true.
	req2 := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on second accept, got %d: %s", rec2.Code, rec2.Body.String())
	}
	var resp2 dto.AcceptInviteResponse
	_ = json.NewDecoder(rec2.Body).Decode(&resp2)
	if !resp2.AlreadyMember {
		t.Error("AlreadyMember must be true on second accept")
	}
	got2, _ := msgStore.GetByID(context.Background(), inv.ID)
	if got2.InviteAcceptedAt == nil || !got2.InviteAcceptedAt.Equal(firstStamp) {
		t.Errorf("InviteAcceptedAt changed on re-accept: was %v, now %v", firstStamp, got2.InviteAcceptedAt)
	}
}

// TestAcceptTacticalInvite_SilentlySkipsMismatchedSource verifies
// that when SourceMessageID resolves to a row that fails any of the
// invariants (wrong direction, wrong kind, mismatched tactical, etc.)
// the subscription still succeeds and the source row is left
// untouched. The plan says acceptance is tactical-keyed, not
// message-keyed.
func TestAcceptTacticalInvite_SilentlySkipsMismatchedSource(t *testing.T) {
	srv, mux, msgStore, _ := newTacticalsTestServer(t)

	// Seed an OUTBOUND text row — wrong direction and wrong kind.
	bogus := &configstore.Message{
		Direction:  "out",
		OurCall:    "N0CALL",
		FromCall:   "N0CALL",
		ToCall:     "W1ABC",
		Text:       "hello",
		ThreadKind: messages.ThreadKindDM,
		ThreadKey:  "W1ABC",
		Kind:       messages.MessageKindText,
	}
	insertMessage(t, msgStore, bogus)

	body := fmt.Sprintf(`{"callsign":"TAC-NET","source_message_id":%d}`, bogus.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// Subscription exists.
	row, _ := srv.store.GetTacticalCallsignByCallsign(context.Background(), "TAC-NET")
	if row == nil || !row.Enabled {
		t.Fatalf("subscription must succeed even with a mismatched source: %+v", row)
	}

	// Source row unchanged — InviteAcceptedAt still nil.
	got, _ := msgStore.GetByID(context.Background(), bogus.ID)
	if got.InviteAcceptedAt != nil {
		t.Errorf("source row must not be stamped on invariant mismatch; InviteAcceptedAt=%v", got.InviteAcceptedAt)
	}
}

// TestAcceptTacticalInvite_TacticalMismatchDoesNotStamp verifies that
// a source message whose InviteTactical differs from the accept's
// callsign is left unchanged — protects against a malicious client
// stamping unrelated rows.
func TestAcceptTacticalInvite_TacticalMismatchDoesNotStamp(t *testing.T) {
	srv, mux, msgStore, _ := newTacticalsTestServer(t)

	// Invite for TAC-A.
	inv := &configstore.Message{
		Direction:      "in",
		OurCall:        "N0CALL",
		FromCall:       "W1ABC",
		ToCall:         "N0CALL",
		Text:           "!GW1 INVITE TAC-A",
		ThreadKind:     messages.ThreadKindDM,
		Kind:           messages.MessageKindInvite,
		InviteTactical: "TAC-A",
		Source:         "rf",
	}
	insertMessage(t, msgStore, inv)

	// Accept for TAC-B, but cite the TAC-A message.
	body := fmt.Sprintf(`{"callsign":"TAC-B","source_message_id":%d}`, inv.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	// TAC-B subscribed.
	rowB, _ := srv.store.GetTacticalCallsignByCallsign(context.Background(), "TAC-B")
	if rowB == nil || !rowB.Enabled {
		t.Fatal("TAC-B subscription missing")
	}
	// TAC-A invite row untouched.
	got, _ := msgStore.GetByID(context.Background(), inv.ID)
	if got.InviteAcceptedAt != nil {
		t.Error("TAC-A invite must NOT be stamped when accept is for a different tactical")
	}
}

// TestAcceptTacticalInvite_EmitsUpdatedEventOnStamp asserts that when
// the accept stamps InviteAcceptedAt, a message.updated SSE event is
// emitted for the source row (so other tabs / the sender re-render).
func TestAcceptTacticalInvite_EmitsUpdatedEventOnStamp(t *testing.T) {
	_, mux, msgStore, svc := newTacticalsTestServer(t)
	hub := svc.EventHub()
	events, unsub := hub.Subscribe()
	defer unsub()

	inv := &configstore.Message{
		Direction:      "in",
		OurCall:        "N0CALL",
		FromCall:       "W1ABC",
		ToCall:         "N0CALL",
		Text:           "!GW1 INVITE TAC-NET",
		ThreadKind:     messages.ThreadKindDM,
		Kind:           messages.MessageKindInvite,
		InviteTactical: "TAC-NET",
		Source:         "rf",
	}
	insertMessage(t, msgStore, inv)

	body := fmt.Sprintf(`{"callsign":"TAC-NET","source_message_id":%d}`, inv.ID)
	req := httptest.NewRequest(http.MethodPost, "/api/tacticals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	select {
	case e := <-events:
		if e.Type != messages.EventMessageUpdated {
			t.Errorf("event type = %q, want %q", e.Type, messages.EventMessageUpdated)
		}
		if e.MessageID != inv.ID {
			t.Errorf("event MessageID = %d, want %d", e.MessageID, inv.ID)
		}
	case <-time.After(time.Second):
		t.Fatal("expected a message.updated event after accept-with-stamp")
	}
}
