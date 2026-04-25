package mapsauth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRegister_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/register" {
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Fatalf("content-type = %q", ct)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"callsign":"N5XXX","token":"GKHkfi0a51nVZbiu_eJ7AqZ3YFvZY43Pvq4jOFZWDf0"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	out, err := c.Register(context.Background(), "N5XXX")
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	if out.Callsign != "N5XXX" || !strings.HasPrefix(out.Token, "GK") {
		t.Fatalf("unexpected response: %+v", out)
	}
}

func TestRegister_DeviceLimitReached(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"device_limit_reached","message":"Registration failed. Please file an issue at https://github.com/chrissnell/graywolf/issues"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Register(context.Background(), "N5XXX")
	var rerr *Error
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if rerr.Code != "device_limit_reached" || rerr.Status != http.StatusConflict {
		t.Fatalf("unexpected error: %+v", rerr)
	}
	if !strings.Contains(rerr.Message, "github.com/chrissnell/graywolf/issues") {
		t.Fatalf("message missing issue URL: %q", rerr.Message)
	}
}

func TestRegister_RateLimitedEmptyBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Register(context.Background(), "N5XXX")
	var rerr *Error
	if !errors.As(err, &rerr) {
		t.Fatalf("expected *Error, got %T: %v", err, err)
	}
	if rerr.Status != http.StatusTooManyRequests || rerr.Code != "rate_limited" {
		t.Fatalf("unexpected error: %+v", rerr)
	}
}

func TestRegister_BlockedShouldNotRetry(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"blocked","message":"Registration failed. Please file an issue at https://github.com/chrissnell/graywolf/issues"}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.Register(context.Background(), "N5XXX")
	var rerr *Error
	if !errors.As(err, &rerr) || rerr.Code != "blocked" {
		t.Fatalf("expected blocked error, got %v", err)
	}
}
