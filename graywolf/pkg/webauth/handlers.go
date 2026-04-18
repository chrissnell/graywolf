package webauth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// defaultSessionMaxAge is the fallback session lifetime used when a
// Handlers value leaves SessionMaxAge at zero.
const defaultSessionMaxAge = 7 * 24 * time.Hour // 7 days

// Handlers groups the auth HTTP endpoints.
type Handlers struct {
	Auth   *AuthStore
	Secure bool // set true when binding to non-loopback
	// Logger receives structured error logs. If nil, slog.Default() is used.
	Logger *slog.Logger
	// SessionMaxAge, when non-zero, overrides the default 7-day session
	// lifetime used for newly-issued session cookies. Zero means use the
	// package default (defaultSessionMaxAge).
	SessionMaxAge time.Duration
}

// logger returns the configured logger or slog.Default() if none was set.
func (h *Handlers) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

// sessionMaxAge returns the configured session lifetime, falling back
// to the package default when SessionMaxAge is zero.
func (h *Handlers) sessionMaxAge() time.Duration {
	if h.SessionMaxAge > 0 {
		return h.SessionMaxAge
	}
	return defaultSessionMaxAge
}

// writeJSON is the shared success-response writer for /api/auth/*.
// Matches the shape of pkg/webapi.writeJSON but takes an optional
// *Handlers so encode failures can be attributed to the configured
// logger when one is available. A nil h falls back to slog.Default().
func writeJSON(h *Handlers, w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		var l *slog.Logger
		if h != nil {
			l = h.logger()
		} else {
			l = slog.Default()
		}
		l.Warn("webauth: json encode failed", "err", err)
	}
}

// LoginRequest is the POST body for /api/auth/login.
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// SetupRequest is the POST body for /api/auth/setup (first-run account
// creation).
type SetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// StatusResponse is the canonical success body for the auth endpoints
// that return only a status sentinel (e.g. login, logout).
type StatusResponse struct {
	Status string `json:"status"`
}

// SetupStatusResponse is returned by GET /api/auth/setup. NeedsSetup is
// true only when no users exist in the auth store.
type SetupStatusResponse struct {
	NeedsSetup bool `json:"needs_setup"`
}

// SetupCreatedResponse is returned by a successful POST /api/auth/setup.
type SetupCreatedResponse struct {
	Status   string `json:"status"`
	Username string `json:"username"`
}

// The JSON error envelope used by /api/auth/* lives in pkg/webtypes
// (webtypes.ErrorResponse). Referencing the shared type here is what
// lets swag emit a single ErrorResponse schema for every /api/*
// endpoint instead of per-package duplicates.

// internal type aliases preserve the existing unexported decode types
// while exposing the request shapes to the OpenAPI spec.
type (
	loginRequest = LoginRequest
	setupRequest = SetupRequest
)

// HandleLogin validates credentials, creates a session, and sets a cookie.
//
// Method dispatch is delegated to the calling mux: this handler is
// registered with a Go 1.22 method-scoped pattern ("POST /api/auth/login")
// by wiring.go, so the mux produces 405 with an Allow header
// automatically if a wrong verb arrives.
//
// @Summary  Log in
// @Tags     auth
// @ID       login
// @Accept   json
// @Produce  json
// @Param    body body     webauth.LoginRequest true "Credentials"
// @Success  200  {object} webauth.StatusResponse
// @Header   200  {string} Set-Cookie "Session cookie"
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  401  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Router   /auth/login [post]
func (h *Handlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		jsonError(w, http.StatusBadRequest, "username and password required")
		return
	}

	user, err := h.Auth.GetUserByUsername(r.Context(), req.Username)
	if err != nil {
		jsonError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}
	if err := CheckPassword(user.PasswordHash, req.Password); err != nil {
		jsonError(w, http.StatusUnauthorized, "invalid credentials")
		return
	}

	token, err := GenerateSessionToken()
	if err != nil {
		h.logger().ErrorContext(r.Context(), "handler failed", "op", "login.generate_token", "err", err)
		jsonError(w, http.StatusInternalServerError, "failed to create session")
		return
	}
	maxAge := h.sessionMaxAge()
	expiry := time.Now().Add(maxAge)
	if _, err := h.Auth.CreateSession(r.Context(), user.ID, token, expiry); err != nil {
		h.logger().ErrorContext(r.Context(), "handler failed", "op", "login.create_session", "err", err)
		jsonError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(maxAge.Seconds()),
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteStrictMode,
	})

	writeJSON(h, w, http.StatusOK, StatusResponse{Status: "ok"})
}

// HandleLogout deletes the session and clears the cookie.
//
// Method dispatch is delegated to the calling mux: this handler is
// registered with "POST /api/auth/logout", so the mux produces 405
// automatically for wrong verbs.
//
// @Summary  Log out
// @Tags     auth
// @ID       logout
// @Produce  json
// @Success  200 {object} webauth.StatusResponse
// @Header   200 {string} Set-Cookie "Cleared session cookie"
// @Router   /auth/logout [post]
func (h *Handlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie(sessionCookie)
	if err == nil && c.Value != "" {
		_ = h.Auth.DeleteSession(r.Context(), c.Value)
	}
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteStrictMode,
	})
	writeJSON(h, w, http.StatusOK, StatusResponse{Status: "ok"})
}

// GetSetupStatus reports whether first-run account creation is still
// required (i.e. whether the user table is empty).
//
// @Summary  Get first-run setup status
// @Tags     auth
// @ID       getSetupStatus
// @Produce  json
// @Success  200 {object} webauth.SetupStatusResponse
// @Failure  500 {object} webtypes.ErrorResponse
// @Router   /auth/setup [get]
func (h *Handlers) GetSetupStatus(w http.ResponseWriter, r *http.Request) {
	count, err := h.Auth.UserCount(r.Context())
	if err != nil {
		h.logger().ErrorContext(r.Context(), "handler failed", "op", "setup.user_count", "err", err)
		jsonError(w, http.StatusInternalServerError, "failed to check users")
		return
	}
	writeJSON(h, w, http.StatusOK, SetupStatusResponse{NeedsSetup: count == 0})
}

// CreateFirstUser creates the first administrative user during setup.
// Returns 403 if any user already exists.
//
// @Summary  Create first-run user
// @Tags     auth
// @ID       createFirstUser
// @Accept   json
// @Produce  json
// @Param    body body     webauth.SetupRequest true "Credentials for the first administrator"
// @Success  201  {object} webauth.SetupCreatedResponse
// @Failure  400  {object} webtypes.ErrorResponse
// @Failure  403  {object} webtypes.ErrorResponse
// @Failure  500  {object} webtypes.ErrorResponse
// @Router   /auth/setup [post]
func (h *Handlers) CreateFirstUser(w http.ResponseWriter, r *http.Request) {
	var req setupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if req.Username == "" || req.Password == "" {
		jsonError(w, http.StatusBadRequest, "username and password required")
		return
	}

	hash, err := HashPassword(req.Password)
	if err != nil {
		h.logger().ErrorContext(r.Context(), "handler failed", "op", "setup.hash_password", "err", err)
		jsonError(w, http.StatusInternalServerError, "failed to create user")
		return
	}
	if _, err := h.Auth.CreateFirstUser(r.Context(), req.Username, hash); err != nil {
		if errors.Is(err, ErrSetupAlreadyComplete) {
			jsonError(w, http.StatusForbidden, "setup already completed")
			return
		}
		h.logger().ErrorContext(r.Context(), "handler failed", "op", "setup.create_first_user", "err", err)
		jsonError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	writeJSON(h, w, http.StatusCreated, SetupCreatedResponse{Status: "ok", Username: req.Username})
}
