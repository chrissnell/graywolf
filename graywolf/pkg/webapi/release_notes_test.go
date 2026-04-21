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
	"time"

	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/releasenotes"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
	"github.com/chrissnell/graywolf/pkg/webauth"
)

// releaseNotesHarness wires the minimal pieces needed to drive the
// three /api/release-notes handlers end-to-end: an auth store, a
// pre-seeded user with a session cookie, and a mux with the
// RequireAuth middleware in front.
type releaseNotesHarness struct {
	mux       http.Handler
	authStore *webauth.AuthStore
	userID    uint32
	token     string
}

func newReleaseNotesHarness(t *testing.T, buildVersion string) *releaseNotesHarness {
	t.Helper()
	store := seedStoreForAuthGate(t)
	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}

	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	bridge := modembridge.New(modembridge.Config{Store: store, Logger: silent})
	apiSrv, err := NewServer(Config{
		Store:   store,
		Bridge:  bridge,
		KissCtx: context.Background(),
		Logger:  silent,
		Version: buildVersion,
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}

	apiMux := http.NewServeMux()
	apiSrv.RegisterRoutes(apiMux)
	RegisterReleaseNotes(apiSrv, apiMux, buildVersion, authStore)

	outer := http.NewServeMux()
	outer.Handle("/api/", webauth.RequireAuth(authStore)(apiMux))

	ctx := context.Background()
	hash, _ := webauth.HashPassword("hunter22")
	user, err := authStore.CreateFirstUser(ctx, "admin", hash, "0.10.11")
	if err != nil {
		t.Fatalf("CreateFirstUser: %v", err)
	}
	tok, _ := webauth.GenerateSessionToken()
	if _, err := authStore.CreateSession(ctx, user.ID, tok, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return &releaseNotesHarness{
		mux:       outer,
		authStore: authStore,
		userID:    user.ID,
		token:     tok,
	}
}

func (h *releaseNotesHarness) do(method, path, body string, withAuth bool) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, r)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if withAuth {
		req.AddCookie(&http.Cookie{Name: "graywolf_session", Value: h.token})
	}
	rec := httptest.NewRecorder()
	h.mux.ServeHTTP(rec, req)
	return rec
}

// withLoader temporarily swaps the package's defaultLoader for a
// test-specific one. Returns a restore closure.
func withLoader(l releaseNotesLoader) func() {
	prev := setLoader(l)
	return func() { setLoader(prev) }
}

// seedLoader returns a loader backed by a fixed note slice.
func seedLoader(notes []releasenotes.Note) releaseNotesLoader {
	return releaseNotesLoader{
		all: func() ([]releasenotes.Note, error) {
			out := make([]releasenotes.Note, len(notes))
			copy(out, notes)
			return out, nil
		},
		unseen: func(lastSeen string) ([]releasenotes.Note, error) {
			out := make([]releasenotes.Note, 0, len(notes))
			for _, n := range notes {
				if releasenotes.Compare(n.Version, lastSeen) > 0 {
					out = append(out, n)
				}
			}
			return out, nil
		},
	}
}

func TestListReleaseNotes_AuthRequired(t *testing.T) {
	h := newReleaseNotesHarness(t, "0.11.0")
	rec := h.do(http.MethodGet, "/api/release-notes", "", false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without session, got %d", rec.Code)
	}
	rec = h.do(http.MethodGet, "/api/release-notes/unseen", "", false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unseen want 401 without session, got %d", rec.Code)
	}
	rec = h.do(http.MethodPost, "/api/release-notes/ack", "", false)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("ack want 401 without session, got %d", rec.Code)
	}
}

func TestListReleaseNotes_Envelope(t *testing.T) {
	h := newReleaseNotesHarness(t, "0.11.0")
	restore := withLoader(seedLoader([]releasenotes.Note{
		{SchemaVersion: 1, Version: "0.11.0", Date: "2026-04-21", Style: "cta", Title: "T", Body: "<p>body</p>"},
		{SchemaVersion: 1, Version: "0.10.11", Date: "2026-04-18", Style: "info", Title: "U", Body: "<p>body</p>"},
	}))
	defer restore()

	rec := h.do(http.MethodGet, "/api/release-notes", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.ReleaseNotesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	if resp.SchemaVersion != 1 {
		t.Fatalf("envelope schema: want 1, got %d", resp.SchemaVersion)
	}
	if resp.Current != "0.11.0" {
		t.Fatalf("current: want 0.11.0, got %q", resp.Current)
	}
	if len(resp.Notes) != 2 {
		t.Fatalf("want 2 notes, got %d", len(resp.Notes))
	}
	if resp.Notes[0].Version != "0.11.0" || resp.Notes[0].Style != "cta" {
		t.Fatalf("want CTA 0.11.0 first, got %+v", resp.Notes[0])
	}
	if resp.Notes[0].SchemaVersion != 1 {
		t.Fatalf("note schema: want 1, got %d", resp.Notes[0].SchemaVersion)
	}
}

func TestUnseenReleaseNotes_FiltersByLastSeen(t *testing.T) {
	h := newReleaseNotesHarness(t, "0.11.0")
	restore := withLoader(seedLoader([]releasenotes.Note{
		{SchemaVersion: 1, Version: "0.11.0", Date: "2026-04-21", Style: "cta", Title: "T", Body: "<p>b</p>"},
		{SchemaVersion: 1, Version: "0.10.11", Date: "2026-04-18", Style: "info", Title: "U", Body: "<p>b</p>"},
	}))
	defer restore()

	// Test fixture user is seeded with LastSeenReleaseVersion=0.10.11,
	// so unseen should include only 0.11.0.
	rec := h.do(http.MethodGet, "/api/release-notes/unseen", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp dto.ReleaseNotesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Current != "0.11.0" {
		t.Fatalf("current: want 0.11.0, got %q", resp.Current)
	}
	if len(resp.Notes) != 1 || resp.Notes[0].Version != "0.11.0" {
		t.Fatalf("want only 0.11.0, got %+v", resp.Notes)
	}
}

func TestAckReleaseNotes_WritesServerVersion(t *testing.T) {
	h := newReleaseNotesHarness(t, "0.11.0")

	rec := h.do(http.MethodPost, "/api/release-notes/ack", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	got, err := h.authStore.GetLastSeenReleaseVersion(context.Background(), h.userID)
	if err != nil {
		t.Fatal(err)
	}
	if got != "0.11.0" {
		t.Fatalf("want 0.11.0 persisted, got %q", got)
	}
}

func TestAckReleaseNotes_IgnoresRequestBody(t *testing.T) {
	h := newReleaseNotesHarness(t, "0.11.0")

	// Client tries to spoof a higher version. Handler must ignore
	// the body and write the server's 0.11.0.
	body := `{"last_seen_release_version":"99.99.99"}`
	rec := h.do(http.MethodPost, "/api/release-notes/ack", body, true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("want 204, got %d: %s", rec.Code, rec.Body.String())
	}
	got, _ := h.authStore.GetLastSeenReleaseVersion(context.Background(), h.userID)
	if got != "0.11.0" {
		t.Fatalf("want server version to win, got %q", got)
	}
}

func TestAckReleaseNotes_Idempotent(t *testing.T) {
	h := newReleaseNotesHarness(t, "0.11.0")

	for i := 0; i < 3; i++ {
		rec := h.do(http.MethodPost, "/api/release-notes/ack", "", true)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("iter %d: want 204, got %d", i, rec.Code)
		}
	}
	got, _ := h.authStore.GetLastSeenReleaseVersion(context.Background(), h.userID)
	if got != "0.11.0" {
		t.Fatalf("after 3 acks want 0.11.0, got %q", got)
	}
}

func TestReleaseNotes_ParseErrorSurfacesAs500(t *testing.T) {
	h := newReleaseNotesHarness(t, "0.11.0")
	restore := withLoader(releaseNotesLoader{
		all:    func() ([]releasenotes.Note, error) { return nil, errors.New("forced parse failure") },
		unseen: func(string) ([]releasenotes.Note, error) { return nil, errors.New("forced parse failure") },
	})
	defer restore()

	// All three read endpoints return 500 when the loader is broken.
	// Ack does not depend on the loader (it only writes the DB), so
	// it continues to return 204.
	for _, p := range []string{"/api/release-notes", "/api/release-notes/unseen"} {
		rec := h.do(http.MethodGet, p, "", true)
		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("%s: want 500 on parse failure, got %d", p, rec.Code)
		}
	}

	rec := h.do(http.MethodPost, "/api/release-notes/ack", "", true)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ack should still succeed when notes parse fails, got %d", rec.Code)
	}
}
