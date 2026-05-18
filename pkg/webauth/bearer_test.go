package webauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func okHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}

func TestBearerAuth_HeaderMatch(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/anything", nil)
	req.Header.Set("Authorization", "Bearer hex-token-abc")
	res, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	if res.StatusCode != 200 {
		t.Fatalf("want 200 got %d", res.StatusCode)
	}
}

func TestBearerAuth_HeaderMismatch(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/api/anything", nil)
	req.Header.Set("Authorization", "Bearer wrong-token")
	res, _ := srv.Client().Do(req)
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d", res.StatusCode)
	}
}

func TestBearerAuth_HeaderMissing(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	res, _ := srv.Client().Get(srv.URL + "/api/anything")
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d", res.StatusCode)
	}
}

func TestBearerAuth_WSUpgradeAcceptsQueryToken(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/ws/x?token=hex-token-abc", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	res, _ := srv.Client().Do(req)
	if res.StatusCode != 200 {
		t.Fatalf("want 200 got %d", res.StatusCode)
	}
}

func TestBearerAuth_WSUpgradeRejectsQueryTokenMismatch(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	req, _ := http.NewRequest("GET", srv.URL+"/ws/x?token=wrong", nil)
	req.Header.Set("Upgrade", "websocket")
	req.Header.Set("Connection", "Upgrade")
	res, _ := srv.Client().Do(req)
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d", res.StatusCode)
	}
}

func TestBearerAuth_NonWSRejectsQueryToken(t *testing.T) {
	mw := BearerAuthMiddleware("hex-token-abc")
	srv := httptest.NewServer(mw(okHandler()))
	defer srv.Close()

	res, _ := srv.Client().Get(srv.URL + "/api/x?token=hex-token-abc")
	if res.StatusCode != 401 {
		t.Fatalf("want 401 got %d (query token must not bypass for non-WS)", res.StatusCode)
	}
}

func TestBearerAuth_EmptyTokenPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("want panic on empty token")
		} else if !strings.Contains(strings.ToLower(toString(r)), "bearer") {
			t.Fatalf("panic message should mention bearer; got %v", r)
		}
	}()
	_ = BearerAuthMiddleware("")
}

func toString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	if e, ok := v.(error); ok {
		return e.Error()
	}
	return ""
}
