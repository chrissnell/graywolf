package webauth

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webtypes"
)

type contextKey int

const (
	userContextKey contextKey = iota
	// bearerAuthedContextKey is set by BearerAuthMiddleware when an
	// Authorization: Bearer match succeeds. RequireAuth honors it as a
	// pre-authorized signal and skips the session-cookie check (the
	// Android WebView authenticates via the per-launch bearer token and
	// has no cookie store / no /login flow).
	bearerAuthedContextKey
)

// AuthenticatedUser returns the WebUser from the request context, or nil.
func AuthenticatedUser(r *http.Request) *WebUser {
	u, _ := r.Context().Value(userContextKey).(*WebUser)
	return u
}

// WithBearerAuthed marks ctx as already authenticated via Bearer token
// (set by BearerAuthMiddleware). RequireAuth uses this to short-circuit
// the cookie check.
func WithBearerAuthed(ctx context.Context) context.Context {
	return context.WithValue(ctx, bearerAuthedContextKey, true)
}

func isBearerAuthed(r *http.Request) bool {
	v, _ := r.Context().Value(bearerAuthedContextKey).(bool)
	return v
}

// RequireAuth returns middleware that validates the session cookie and
// populates the request context with the authenticated user. Unauthenticated
// requests receive a 401 JSON response.
//
// A request that arrives with the bearerAuthedContextKey marker (set by
// BearerAuthMiddleware) is treated as pre-authenticated; the single
// station user from the AuthStore is loaded into the context and the
// cookie check is skipped. Graywolf is a single-user station (memory
// feedback_single_user_station), so picking the first listed user is
// the correct identity for Android requests.
func RequireAuth(auth *AuthStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isBearerAuthed(r) {
				users, err := auth.ListUsers(r.Context())
				if err == nil && len(users) > 0 {
					u := users[0]
					ctx := context.WithValue(r.Context(), userContextKey, &u)
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				// No station user yet (fresh install): bearer auth still
				// passes the request through with no user attached.
				// Handlers that need a user will surface their own 401.
				next.ServeHTTP(w, r)
				return
			}
			c, err := r.Cookie(sessionCookie)
			if err != nil || c.Value == "" {
				jsonError(w, http.StatusUnauthorized, "authentication required")
				return
			}
			sess, err := auth.GetSessionByToken(r.Context(), c.Value)
			if err != nil {
				jsonError(w, http.StatusUnauthorized, "invalid or expired session")
				return
			}
			user, err := auth.getUserByID(r.Context(), sess.UserID)
			if err != nil {
				jsonError(w, http.StatusUnauthorized, "user not found")
				return
			}
			ctx := context.WithValue(r.Context(), userContextKey, user)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// getUserByID is an internal helper.
func (s *AuthStore) getUserByID(ctx context.Context, id uint32) (*WebUser, error) {
	var u WebUser
	if err := s.db.WithContext(ctx).First(&u, id).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// jsonError writes a typed webtypes.ErrorResponse envelope using the
// package's writeJSON helper. The helper handles Content-Type, status,
// and logs any encode failure via slog.Default (middleware has no
// *Handlers receiver from which to pull a configured logger).
func jsonError(w http.ResponseWriter, code int, message string) {
	writeJSON(nil, w, code, webtypes.ErrorResponse{Error: message})
}
