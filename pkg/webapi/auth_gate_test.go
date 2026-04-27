package webapi

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/modembridge"
	"github.com/chrissnell/graywolf/pkg/webauth"
)

// TestAuthGate_EveryRoute walks every /api/* route the webapi server
// registers and asserts two things:
//
//  1. Unauthenticated requests receive HTTP 401 from the RequireAuth
//     middleware (or, for the intentionally-public routes, whatever
//     the handler's public behavior is).
//
//  2. Authenticated requests do NOT receive 401. The exact success
//     code is intentionally not asserted — this test only cares that
//     the auth layer let the request through. Whether the handler
//     then returns 200, 400, 404, or 405 for a particular
//     method/path combo is outside this test's scope; those are
//     covered by the per-resource handler tests.
//
// The point of this test is to be a bulkhead: if someone adds a new
// route under /api/ and forgets to wrap it with RequireAuth, or bolts
// a route onto the outer mux bypassing apiMux, this table will miss
// it and the new route should be added. If someone *intentionally*
// makes a new route public, the table's publicRoutes section is where
// that exemption is declared — and the reviewer has to notice.
func TestAuthGate_EveryRoute(t *testing.T) {
	srv := newAuthGateServer(t)
	token := seedUserAndSession(t, srv.authStore)

	// Routes protected by RequireAuth. Every /api/* route that is NOT
	// intentionally public must appear here; a missing row means the
	// test would not catch a regression that turned the route public.
	protected := []struct {
		method string
		path   string
		body   string
	}{
		// Channels
		{http.MethodGet, "/api/channels", ""},
		{http.MethodPost, "/api/channels", `{}`},
		{http.MethodGet, "/api/channels/1", ""},
		{http.MethodPut, "/api/channels/1", `{}`},
		{http.MethodDelete, "/api/channels/1", ""},
		{http.MethodGet, "/api/channels/1/stats", ""},

		// Audio devices
		{http.MethodGet, "/api/audio-devices", ""},
		{http.MethodPost, "/api/audio-devices", `{}`},
		{http.MethodGet, "/api/audio-devices/1", ""},
		{http.MethodPut, "/api/audio-devices/1", `{}`},
		{http.MethodDelete, "/api/audio-devices/1", ""},

		// Beacons
		{http.MethodGet, "/api/beacons", ""},
		{http.MethodPost, "/api/beacons", `{}`},
		{http.MethodGet, "/api/beacons/1", ""},
		{http.MethodPut, "/api/beacons/1", `{}`},
		{http.MethodDelete, "/api/beacons/1", ""},
		{http.MethodPost, "/api/beacons/1/send", ""},

		// PTT + TX timing
		{http.MethodGet, "/api/ptt", ""},
		{http.MethodPost, "/api/ptt", `{}`},
		{http.MethodGet, "/api/ptt/1", ""},
		{http.MethodGet, "/api/tx-timing", ""},
		{http.MethodGet, "/api/tx-timing/1", ""},
		{http.MethodPut, "/api/tx-timing/1", `{}`},

		// KISS interfaces
		{http.MethodGet, "/api/kiss", ""},
		{http.MethodPost, "/api/kiss", `{}`},
		{http.MethodGet, "/api/kiss/1", ""},
		{http.MethodPut, "/api/kiss/1", `{}`},
		{http.MethodDelete, "/api/kiss/1", ""},

		// AGW
		{http.MethodGet, "/api/agw", ""},
		{http.MethodPut, "/api/agw", `{}`},

		// iGate config (the /api/igate status endpoint is registered
		// separately; only /api/igate/config and /api/igate/filters
		// belong to the protected set)
		{http.MethodGet, "/api/igate/config", ""},
		{http.MethodPut, "/api/igate/config", `{}`},
		{http.MethodGet, "/api/igate/filters", ""},
		{http.MethodPost, "/api/igate/filters", `{}`},
		{http.MethodDelete, "/api/igate/filters/1", ""},

		// Digipeater
		{http.MethodGet, "/api/digipeater", ""},
		{http.MethodPut, "/api/digipeater", `{}`},
		{http.MethodGet, "/api/digipeater/rules", ""},
		{http.MethodPost, "/api/digipeater/rules", `{}`},
		{http.MethodPut, "/api/digipeater/rules/1", `{}`},
		{http.MethodDelete, "/api/digipeater/rules/1", ""},

		// GPS
		{http.MethodGet, "/api/gps", ""},
		{http.MethodPut, "/api/gps", `{}`},
		{http.MethodGet, "/api/gps/available", ""},

		// Health/status are informational but still behind auth —
		// graywolf does not model untrusted web users, so nothing in
		// /api/ is public except the explicit exceptions below.
		{http.MethodGet, "/api/health", ""},
		{http.MethodGet, "/api/status", ""},

		// Release notes (popup + About page "What's new").
		{http.MethodGet, "/api/release-notes", ""},
		{http.MethodGet, "/api/release-notes/unseen", ""},
		{http.MethodPost, "/api/release-notes/ack", ""},
	}

	// Walk the table twice: once with no cookie (expect 401) and once
	// with the session cookie (expect NOT 401).
	for _, tc := range protected {
		tc := tc
		name := tc.method + " " + tc.path
		t.Run("unauth "+name, func(t *testing.T) {
			rec := doRequest(srv.mux, tc.method, tc.path, tc.body, "")
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%s unauthenticated: got %d, want 401\nbody: %s",
					name, rec.Code, rec.Body.String())
			}
		})
		t.Run("auth "+name, func(t *testing.T) {
			rec := doRequest(srv.mux, tc.method, tc.path, tc.body, token)
			if rec.Code == http.StatusUnauthorized {
				t.Fatalf("%s authenticated: got 401 (auth layer rejected a valid session)\nbody: %s",
					name, rec.Body.String())
			}
		})
	}
}

// TestAuthGate_PublicRoutes documents and exercises every /api/ route
// that is intentionally NOT behind RequireAuth. These are the login,
// logout, setup, and version endpoints: the first three cannot be
// protected (the user is not yet authenticated when calling them) and
// version is small public surface area that the UI reads before login
// to decide which screens to show.
//
// Any new public route MUST be added here and justified in the
// comment above. If you think a new route should be public, first
// check work order 04 in scratch/fix-plan — the project deliberately
// does not model untrusted web users, so "public" means "this
// endpoint can be reached by an attacker on the same LAN".
func TestAuthGate_PublicRoutes(t *testing.T) {
	srv := newAuthGateServer(t)

	// POST /api/auth/setup on an empty DB creates the first user and
	// returns 201. A second call returns 403 (setup already complete).
	// The route itself is public; this test asserts the first call
	// isn't gated by 401.
	rec := doRequest(srv.mux, http.MethodPost, "/api/auth/setup",
		`{"username":"admin","password":"hunter22"}`, "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /api/auth/setup first call: got %d, want 201\nbody: %s",
			rec.Code, rec.Body.String())
	}

	// POST /api/auth/login with the newly-created credentials should
	// return 200 plus a session cookie.
	rec = doRequest(srv.mux, http.MethodPost, "/api/auth/login",
		`{"username":"admin","password":"hunter22"}`, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/login: got %d, want 200\nbody: %s",
			rec.Code, rec.Body.String())
	}
	if !hasSessionCookie(rec) {
		t.Fatalf("POST /api/auth/login: no session cookie in response")
	}

	// POST /api/auth/logout without a cookie still returns 200 (it's
	// idempotent by design: clearing an absent cookie is a no-op).
	rec = doRequest(srv.mux, http.MethodPost, "/api/auth/logout", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("POST /api/auth/logout: got %d, want 200\nbody: %s",
			rec.Code, rec.Body.String())
	}

	// GET /api/auth/setup reports whether setup is needed. After the
	// earlier POST it should return 200 with needs_setup=false.
	rec = doRequest(srv.mux, http.MethodGet, "/api/auth/setup", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/auth/setup: got %d, want 200\nbody: %s",
			rec.Code, rec.Body.String())
	}

	// GET /api/version is public.
	rec = doRequest(srv.mux, http.MethodGet, "/api/version", "", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/version: got %d, want 200\nbody: %s",
			rec.Code, rec.Body.String())
	}
}

// ---------------------------------------------------------------------------
// Fixtures
// ---------------------------------------------------------------------------

// authGateServer bundles the mounted mux, the auth store used to mint
// sessions, and the configstore we need to keep alive for the test's
// lifetime so its t.Cleanup hook closes it after the subtests finish.
type authGateServer struct {
	mux       http.Handler
	authStore *webauth.AuthStore
}

// newAuthGateServer builds the same mux shape the production wiring
// produces in pkg/app/wiring.go wireHTTP, but without the extra
// plumbing (packets log, gps cache, igate, metrics) that isn't needed
// to exercise the auth layer. Specifically, it mounts:
//
//	/api/version        → public handler
//	/api/auth/{login,logout,setup} → public auth handlers
//	/api/ (everything else) → webauth.RequireAuth(apiMux)
//
// where apiMux has every registerX route the production Server
// installs. That is the exact topology the auth gate must protect.
func newAuthGateServer(t *testing.T) *authGateServer {
	t.Helper()

	store := seedStoreForAuthGate(t)
	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}

	silent := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Minimal webapi.Server — a nil Bridge is fine because the table
	// above only hits routes up to the point where the auth layer
	// either rejects or admits the request. Handler bodies that
	// dereference bridge/bridge state are allowed to 500; that's
	// still "not 401" as far as the auth gate test is concerned.
	// Supply a real bridge anyway so handlers that probe nil-ness
	// don't panic.
	bridge := modembridge.New(modembridge.Config{
		Store:  store,
		Logger: silent,
	})
	apiSrv, err := NewServer(Config{
		Store:   store,
		Bridge:  bridge,
		KissCtx: context.Background(),
		Logger:  silent,
		Version: "test",
	})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	// beacon/send uses this callback — make it a no-op so the handler
	// can advance past its nil-check without the full beacon.Scheduler.
	apiSrv.SetBeaconSendNow(func(context.Context, uint32) error { return nil })
	// Reload channels are drained in production by the wiring layer;
	// buffered(1) channels keep the signal-on-write handlers from
	// blocking if a test triggers a write path.
	apiSrv.SetGPSReload(make(chan struct{}, 1))
	apiSrv.SetBeaconReload(make(chan struct{}, 1))
	apiSrv.SetDigipeaterReload(make(chan struct{}, 1))

	outer := http.NewServeMux()

	// Public routes mirror the wiring order.
	RegisterVersion(apiSrv, outer)
	authHandlers := &webauth.Handlers{Auth: authStore, Logger: silent}
	outer.HandleFunc("POST /api/auth/login", authHandlers.HandleLogin)
	outer.HandleFunc("POST /api/auth/logout", authHandlers.HandleLogout)
	outer.HandleFunc("GET /api/auth/setup", authHandlers.GetSetupStatus)
	outer.HandleFunc("POST /api/auth/setup", authHandlers.CreateFirstUser)

	// Protected routes live on an inner mux wrapped with RequireAuth.
	apiMux := http.NewServeMux()
	apiSrv.RegisterRoutes(apiMux)
	RegisterReleaseNotes(apiSrv, apiMux, "test", authStore)

	outer.Handle("/api/", webauth.RequireAuth(authStore)(apiMux))

	return &authGateServer{mux: outer, authStore: authStore}
}

// seedStoreForAuthGate opens a fresh in-memory configstore and populates
// it with one audio device and one channel so handlers that try to list
// or look things up produce realistic responses instead of empty sets.
// The exact data shape is not asserted by this test — it's just here
// so nothing panics on a nil row.
func seedStoreForAuthGate(t *testing.T) *configstore.Store {
	t.Helper()
	store, err := configstore.OpenMemory()
	if err != nil {
		t.Fatalf("OpenMemory: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	ctx := context.Background()
	dev := &configstore.AudioDevice{
		Name: "test", Direction: "input", SourceType: "flac",
		SourcePath: "/tmp/x.flac", SampleRate: 44100, Channels: 1, Format: "s16le",
	}
	if err := store.CreateAudioDevice(ctx, dev); err != nil {
		t.Fatalf("CreateAudioDevice: %v", err)
	}
	ch := &configstore.Channel{
		Name: "rx0", InputDeviceID: configstore.U32Ptr(dev.ID),
		ModemType: "afsk", BitRate: 1200, MarkFreq: 1200, SpaceFreq: 2200,
		Profile: "A", NumSlicers: 1, FixBits: "none",
	}
	if err := store.CreateChannel(ctx, ch); err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}

	// Pre-populate rows that a handler's GET-by-id path would otherwise
	// dereference as a nil pointer. These are not part of what this
	// test is asserting — they only exist to keep the auth gate walk
	// from tripping over pre-existing handler bugs in which a
	// not-found store lookup returns (nil, nil) and the response
	// mapper crashes. Those bugs should be fixed separately; see
	// follow-ups in scratch/fix-plan/13-testing-and-ci-hardening.md.
	if err := store.UpsertTxTiming(ctx, &configstore.TxTiming{Channel: 1}); err != nil {
		t.Fatalf("UpsertTxTiming: %v", err)
	}
	return store
}

// seedUserAndSession pre-creates one user and one non-expired session
// token directly via AuthStore, bypassing the login handler. That
// keeps the test independent of the HTTP login path (which has its own
// tests) and short-circuits a password hash round-trip per request.
func seedUserAndSession(t *testing.T, auth *webauth.AuthStore) string {
	t.Helper()
	ctx := context.Background()
	hash, err := webauth.HashPassword("hunter22")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	user, err := auth.CreateFirstUser(ctx, "admin", hash, "")
	if err != nil {
		t.Fatalf("CreateFirstUser: %v", err)
	}
	token, err := webauth.GenerateSessionToken()
	if err != nil {
		t.Fatalf("GenerateSessionToken: %v", err)
	}
	if _, err := auth.CreateSession(ctx, user.ID, token, time.Now().Add(time.Hour)); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}
	return token
}

// doRequest drives one round-trip through the given handler and
// returns the recorder for assertions. An empty token means the
// request carries no cookie; a non-empty token attaches a session
// cookie with the same name the auth middleware reads.
func doRequest(h http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	var reqBody io.Reader
	if body != "" {
		reqBody = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, reqBody)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "graywolf_session", Value: token})
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// hasSessionCookie reports whether the response set a cookie named
// "graywolf_session" — used by the public-routes test to confirm login
// actually issued a credential.
func hasSessionCookie(rec *httptest.ResponseRecorder) bool {
	for _, c := range rec.Result().Cookies() {
		if c.Name == "graywolf_session" && c.Value != "" {
			return true
		}
	}
	// Fallback: some middleware compositions only set the raw
	// Set-Cookie header without going through SetCookie.
	for _, hdr := range rec.Header().Values("Set-Cookie") {
		if strings.HasPrefix(hdr, "graywolf_session=") {
			return true
		}
	}
	return false
}
