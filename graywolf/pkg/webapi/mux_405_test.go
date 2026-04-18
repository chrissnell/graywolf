package webapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webauth"
)

// TestMux405PassesThroughAllowHeader verifies two invariants that the
// handler-split work (Phases 1–5) depends on:
//
//  1. Go 1.22's http.ServeMux returns 405 Method Not Allowed with an
//     Allow: header listing the registered verbs when a path matches
//     but the method does not.
//
//  2. webauth.RequireAuth — the only middleware in front of apiMux in
//     pkg/app/wiring.go — passes that status code and Allow header
//     through to the client unchanged.
//
// The test constructs a minimal mux that registers exactly one method
// pattern, wraps it in RequireAuth using an in-memory AuthStore with a
// seeded session, and drives a wrong-method request through the full
// middleware + mux stack. If either the middleware strips the Allow
// header or Go's mux stops emitting it, the refactor's 405-based
// routing contract breaks, which is why this is an explicit smoke
// test rather than an implicit assumption.
//
// The plan (pkg/webapi/... Phase 0) permits a focused test here rather
// than the full pkg/app smoke test — building the full wiring stack
// requires compiling a fake modem binary and is impractical for a
// middleware-transparency check.
func TestMux405PassesThroughAllowHeader(t *testing.T) {
	// Build a minimal configstore-backed AuthStore so RequireAuth has a
	// real session lookup to succeed against.
	store := seedStoreForAuthGate(t)
	authStore, err := webauth.NewAuthStore(store.DB())
	if err != nil {
		t.Fatalf("NewAuthStore: %v", err)
	}
	token := seedUserAndSession(t, authStore)

	// Minimal inner mux that uses the Go 1.22 method-scoped pattern.
	// Nothing else about the webapi.Server matters here — we're
	// exercising the mux + middleware, not any handler logic.
	inner := http.NewServeMux()
	inner.HandleFunc("GET /api/probe", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Outer wiring mirrors pkg/app/wiring.go: apiMux is mounted under
	// RequireAuth. We register it at "/api/" so the outer mux delegates
	// every /api/* request into the inner mux — the same shape the
	// production wiring uses.
	outer := http.NewServeMux()
	outer.Handle("/api/", webauth.RequireAuth(authStore)(inner))

	// POST to a GET-only endpoint with a valid session.
	req := httptest.NewRequest(http.MethodPost, "/api/probe", nil)
	req.AddCookie(&http.Cookie{Name: "graywolf_session", Value: token})
	rec := httptest.NewRecorder()
	outer.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST on GET-only route, got %d\nbody: %s",
			rec.Code, rec.Body.String())
	}
	allow := rec.Header().Get("Allow")
	if allow == "" {
		t.Fatalf("expected Allow header on 405 response, got none")
	}
	// Go 1.22+ emits a comma-separated list; GET must be in it.
	if !containsMethod(allow, http.MethodGet) {
		t.Fatalf("Allow header missing GET: %q", allow)
	}
}

// containsMethod reports whether the comma-separated Allow header lists
// the given HTTP verb. Match is whitespace-tolerant because Go's
// serializer format is not contractually frozen.
func containsMethod(allow, method string) bool {
	for _, part := range strings.Split(allow, ",") {
		if strings.EqualFold(strings.TrimSpace(part), method) {
			return true
		}
	}
	return false
}
