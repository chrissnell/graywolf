package webauth

import (
	"encoding/json"
	"net/http"
	"time"
)

const sessionMaxAge = 7 * 24 * time.Hour // 7 days

// Handlers groups the auth HTTP endpoints.
type Handlers struct {
	Auth   *AuthStore
	Secure bool // set true when binding to non-loopback
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
		jsonError(w, http.StatusInternalServerError, "failed to generate session")
		return
	}
	expiry := time.Now().Add(sessionMaxAge)
	if _, err := h.Auth.CreateSession(r.Context(), user.ID, token, expiry); err != nil {
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

// HandleSetup creates the first user when no users exist. Returns 403 if
// users already exist (preventing privilege escalation).
// POST /api/auth/setup
func (h *Handlers) HandleSetup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		jsonError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	count, err := h.Auth.UserCount(r.Context())
	if err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to check users")
		return
	}
	if count > 0 {
		jsonError(w, http.StatusForbidden, "setup already completed")
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
		jsonError(w, http.StatusInternalServerError, "failed to hash password")
		return
	}
	if _, err := h.Auth.CreateUser(r.Context(), req.Username, hash); err != nil {
		jsonError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(map[string]string{"status": "ok", "username": req.Username})
}
