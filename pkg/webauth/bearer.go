package webauth

import (
	"crypto/subtle"
	"net/http"
	"strings"
)

// BearerAuthMiddleware requires Authorization: Bearer <token> on every
// request, except WebSocket upgrade requests which may also pass
// ?token=<hex>. Mismatch responds 401 with a JSON error body.
//
// Used only by the Android entry (cmd/graywolf/main_android.go) where
// the local HTTP listener is reachable by every other app on the
// device. Empty token is a programmer error and panics; the Android
// Service must always inject one.
func BearerAuthMiddleware(token string) func(http.Handler) http.Handler {
	if token == "" {
		panic("webauth: BearerAuthMiddleware requires non-empty token")
	}
	want := []byte(token)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if matchHeader(r, want) {
				next.ServeHTTP(w, r.WithContext(WithBearerAuthed(r.Context())))
				return
			}
			if isWebSocketUpgrade(r) && matchQueryToken(r, want) {
				next.ServeHTTP(w, r.WithContext(WithBearerAuthed(r.Context())))
				return
			}
			jsonError(w, http.StatusUnauthorized, "authentication required")
		})
	}
}

func matchHeader(r *http.Request, want []byte) bool {
	h := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := []byte(h[len(prefix):])
	return subtle.ConstantTimeCompare(got, want) == 1
}

func matchQueryToken(r *http.Request, want []byte) bool {
	got := []byte(r.URL.Query().Get("token"))
	if len(got) == 0 {
		return false
	}
	return subtle.ConstantTimeCompare(got, want) == 1
}

func isWebSocketUpgrade(r *http.Request) bool {
	return strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
		strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade")
}
