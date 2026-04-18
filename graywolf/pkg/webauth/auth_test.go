package webauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func testDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file::memory:?cache=shared"), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { sqlDB.Close() })
	return db
}

func testAuthStore(t *testing.T) *AuthStore {
	t.Helper()
	s, err := NewAuthStore(testDB(t))
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestHashAndCheckPassword(t *testing.T) {
	hash, err := HashPassword("hunter2")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "" || hash == "hunter2" {
		t.Fatal("hash should be a bcrypt string")
	}
	if err := CheckPassword(hash, "hunter2"); err != nil {
		t.Fatal("correct password should match")
	}
	if err := CheckPassword(hash, "wrong"); err == nil {
		t.Fatal("wrong password should not match")
	}
}

func TestGenerateSessionToken(t *testing.T) {
	tok, err := GenerateSessionToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) != 64 { // 32 bytes hex-encoded
		t.Fatalf("expected 64-char hex token, got %d chars", len(tok))
	}
	// Uniqueness
	tok2, _ := GenerateSessionToken()
	if tok == tok2 {
		t.Fatal("tokens should be unique")
	}
}

func TestUserCRUD(t *testing.T) {
	s := testAuthStore(t)
	ctx := context.Background()

	u, err := s.CreateUser(ctx, "admin", "hash123")
	if err != nil {
		t.Fatal(err)
	}
	if u.ID == 0 {
		t.Fatal("expected auto-id")
	}

	got, err := s.GetUserByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if got.PasswordHash != "hash123" {
		t.Fatalf("unexpected hash: %s", got.PasswordHash)
	}

	users, err := s.ListUsers(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 {
		t.Fatalf("expected 1 user, got %d", len(users))
	}

	if err := s.DeleteUser(ctx, "admin"); err != nil {
		t.Fatal(err)
	}
	count, _ := s.UserCount(ctx)
	if count != 0 {
		t.Fatalf("expected 0 users after delete, got %d", count)
	}
}

func TestSessionLifecycle(t *testing.T) {
	s := testAuthStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, "admin", "hash")
	_, err := s.CreateSession(ctx, u.ID, "tok123", time.Now().Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	sess, err := s.GetSessionByToken(ctx, "tok123")
	if err != nil {
		t.Fatal(err)
	}
	if sess.UserID != u.ID {
		t.Fatalf("wrong user id: %d", sess.UserID)
	}

	// Expired session should not be returned.
	_, err = s.CreateSession(ctx, u.ID, "expired-tok", time.Now().Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.GetSessionByToken(ctx, "expired-tok")
	if err == nil {
		t.Fatal("expected error for expired session")
	}

	if err := s.DeleteSession(ctx, "tok123"); err != nil {
		t.Fatal(err)
	}
	_, err = s.GetSessionByToken(ctx, "tok123")
	if err == nil {
		t.Fatal("expected error for deleted session")
	}
}

func TestDeleteExpiredSessions(t *testing.T) {
	s := testAuthStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, "admin", "hash")
	s.CreateSession(ctx, u.ID, "valid", time.Now().Add(time.Hour))
	s.CreateSession(ctx, u.ID, "exp1", time.Now().Add(-time.Hour))
	s.CreateSession(ctx, u.ID, "exp2", time.Now().Add(-2*time.Hour))

	n, err := s.DeleteExpiredSessions(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 expired deleted, got %d", n)
	}

	// Valid session still works.
	if _, err := s.GetSessionByToken(ctx, "valid"); err != nil {
		t.Fatal(err)
	}
}

func TestMiddleware(t *testing.T) {
	s := testAuthStore(t)
	ctx := context.Background()

	u, _ := s.CreateUser(ctx, "admin", "hash")
	s.CreateSession(ctx, u.ID, "valid-token", time.Now().Add(time.Hour))

	protected := RequireAuth(s)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user := AuthenticatedUser(r)
		if user == nil {
			t.Fatal("expected user in context")
		}
		w.WriteHeader(http.StatusOK)
	}))

	// No cookie → 401
	req := httptest.NewRequest("GET", "/api/test", nil)
	rec := httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Bad cookie → 401
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "bad-token"})
	rec = httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Valid cookie → 200
	req = httptest.NewRequest("GET", "/api/test", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: "valid-token"})
	rec = httptest.NewRecorder()
	protected.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestFirstRunSetup(t *testing.T) {
	s := testAuthStore(t)
	h := &Handlers{Auth: s}

	// First setup should succeed.
	body, _ := json.Marshal(setupRequest{Username: "admin", Password: "secret"})
	req := httptest.NewRequest("POST", "/api/auth/setup", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateFirstUser(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}

	// Second setup should be forbidden.
	body, _ = json.Marshal(setupRequest{Username: "hacker", Password: "evil"})
	req = httptest.NewRequest("POST", "/api/auth/setup", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	h.CreateFirstUser(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
}

func TestLoginLogout(t *testing.T) {
	s := testAuthStore(t)
	ctx := context.Background()
	h := &Handlers{Auth: s}

	hash, _ := HashPassword("correct")
	s.CreateUser(ctx, "admin", hash)

	// Wrong password → 401
	body, _ := json.Marshal(loginRequest{Username: "admin", Password: "wrong"})
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.HandleLogin(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", rec.Code)
	}

	// Correct password → 200 + set-cookie
	body, _ = json.Marshal(loginRequest{Username: "admin", Password: "correct"})
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewReader(body))
	rec = httptest.NewRecorder()
	h.HandleLogin(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	cookies := rec.Result().Cookies()
	var sessionCookieValue string
	for _, c := range cookies {
		if c.Name == sessionCookie {
			sessionCookieValue = c.Value
			if !c.HttpOnly {
				t.Fatal("session cookie should be HttpOnly")
			}
		}
	}
	if sessionCookieValue == "" {
		t.Fatal("expected session cookie")
	}

	// Logout should clear cookie
	req = httptest.NewRequest("POST", "/api/auth/logout", nil)
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: sessionCookieValue})
	rec = httptest.NewRecorder()
	h.HandleLogout(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	// Session should be deleted
	_, err := s.GetSessionByToken(ctx, sessionCookieValue)
	if err == nil {
		t.Fatal("session should have been deleted")
	}
}

// TestStoreHonorsContextCancellation verifies that a cancelled context
// prevents any store work from hitting the database.
func TestStoreHonorsContextCancellation(t *testing.T) {
	s := testAuthStore(t)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := s.CreateUser(ctx, "admin", "hash")
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	// No row should have been inserted.
	count, cerr := s.UserCount(context.Background())
	if cerr != nil {
		t.Fatal(cerr)
	}
	if count != 0 {
		t.Fatalf("expected 0 users after cancelled CreateUser, got %d", count)
	}
}

// TestCreateFirstUserConcurrent spins many goroutines that all try to create
// the first user simultaneously. Exactly one should win; everyone else must
// see ErrSetupAlreadyComplete.
func TestCreateFirstUserConcurrent(t *testing.T) {
	s := testAuthStore(t)

	const n = 8
	start := make(chan struct{})
	var wg sync.WaitGroup
	var successes atomic.Int32
	var alreadyComplete atomic.Int32
	var other atomic.Int32

	for i := 0; i < n; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			username := "admin" + string(rune('0'+i))
			_, err := s.CreateFirstUser(context.Background(), username, "hash")
			switch {
			case err == nil:
				successes.Add(1)
			case errors.Is(err, ErrSetupAlreadyComplete):
				alreadyComplete.Add(1)
			default:
				other.Add(1)
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	close(start)
	wg.Wait()

	if successes.Load() != 1 {
		t.Fatalf("expected exactly 1 success, got %d", successes.Load())
	}
	if alreadyComplete.Load() != n-1 {
		t.Fatalf("expected %d ErrSetupAlreadyComplete, got %d", n-1, alreadyComplete.Load())
	}
	if other.Load() != 0 {
		t.Fatalf("expected 0 unexpected errors, got %d", other.Load())
	}

	// DB should hold exactly one user.
	count, err := s.UserCount(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 user in db, got %d", count)
	}
}

// TestCreateFirstUserErrorSanitization confirms that a second setup attempt
// returns a clean "setup already completed" error body — not a leaked DB
// message like a UNIQUE constraint violation.
func TestCreateFirstUserErrorSanitization(t *testing.T) {
	s := testAuthStore(t)
	h := &Handlers{Auth: s}

	// Seed one user directly so the setup endpoint sees a populated DB.
	if _, err := s.CreateUser(context.Background(), "existing", "hash"); err != nil {
		t.Fatal(err)
	}

	body, _ := json.Marshal(setupRequest{Username: "newbie", Password: "secret"})
	req := httptest.NewRequest("POST", "/api/auth/setup", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.CreateFirstUser(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode response: %v (body=%s)", err, rec.Body.String())
	}
	if got := resp["error"]; got != "setup already completed" {
		t.Fatalf("expected sanitized error, got %q", got)
	}
}
