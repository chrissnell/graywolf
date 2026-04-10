package webauth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

const sessionMaxAge = 7 * 24 * time.Hour // 7 days

// Handlers groups the auth HTTP endpoints.
type Handlers struct {
	Auth   *AuthStore
	Secure bool // set true when binding to non-loopback
	// Logger receives structured error logs. If nil, slog.Default() is used.
	Logger *slog.Logger
}

// logger returns the configured logger or slog.Default() if none was set.
func (h *Handlers) logger() *slog.Logger {
	if h.Logger != nil {
		return h.Logger
	}
	return slog.Default()
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type setupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// HandleLogin validates credentials, creates a session, and sets a cookie.
// POST /api/auth/login
func (h *Handlers) HandleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
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
	expiry := time.Now().Add(sessionMaxAge)
	if _, err := h.Auth.CreateSession(r.Context(), user.ID, token, expiry); err != nil {
		h.logger().ErrorContext(r.Context(), "handler failed", "op", "login.create_session", "err", err)
		jsonError(w, http.StatusInternalServerError, "failed to create session")
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(sessionMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   h.Secure,
		SameSite: http.SameSiteStrictMode,
	})

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleLogout deletes the session and clears the cookie.
// POST /api/auth/logout
func (h *Handlers) HandleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
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
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
}

// HandleSetup handles first-run account creation.
//   GET  /api/auth/setup → {"needs_setup": bool}
//   POST /api/auth/setup → creates the first user (403 if users exist)
func (h *Handlers) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		count, err := h.Auth.UserCount(r.Context())
		if err != nil {
			h.logger().ErrorContext(r.Context(), "handler failed", "op", "setup.user_count", "err", err)
			jsonError(w, http.StatusInternalServerError, "failed to check users")
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]bool{"needs_setup": count == 0})
		return
	}
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
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

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "username": req.Username})
}
