package webauth

import (
	"context"
	"net/http"

	"github.com/chrissnell/graywolf/pkg/webtypes"
)

type contextKey int

const userContextKey contextKey = iota

// AuthenticatedUser returns the WebUser from the request context, or nil.
func AuthenticatedUser(r *http.Request) *WebUser {
	u, _ := r.Context().Value(userContextKey).(*WebUser)
	return u
}

// RequireAuth returns middleware that validates the session cookie and
// populates the request context with the authenticated user. Unauthenticated
// requests receive a 401 JSON response.
func RequireAuth(auth *AuthStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
