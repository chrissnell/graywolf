package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	pb "github.com/chrissnell/graywolf/pkg/ipcproto"
)

func TestBearerAuthRejectsMissingHeader(t *testing.T) {
	h := bearerAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestBearerAuthRejectsWrongToken(t *testing.T) {
	h := bearerAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer nope")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401, got %d", rr.Code)
	}
}

func TestBearerAuthAcceptsRightToken(t *testing.T) {
	h := bearerAuth("secret", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rr.Code)
	}
}

func TestFrameRingHoldsLastN(t *testing.T) {
	r := newFrameRing(3)
	for i := 0; i < 5; i++ {
		r.push(decodedLine{Stamp: "t", Text: string(rune('A' + i))})
	}
	got := r.snapshot()
	if len(got) != 3 {
		t.Fatalf("want 3, got %d", len(got))
	}
	if got[0].Text != "E" || got[2].Text != "C" {
		t.Fatalf("want newest-first E,D,C; got %+v", got)
	}
}

func TestFrameRingCountIsMonotonic(t *testing.T) {
	r := newFrameRing(2)
	for i := 0; i < 10; i++ {
		r.push(decodedLine{Stamp: "t", Text: "x"})
	}
	if r.count() != 10 {
		t.Fatalf("want count=10, got %d", r.count())
	}
}

func TestFrameRoundTrip(t *testing.T) {
	msg := &pb.IpcMessage{Payload: &pb.IpcMessage_ModemReady{ModemReady: &pb.ModemReady{Version: "v0", Pid: 7}}}
	var buf bytes.Buffer
	if err := writeFrame(&buf, msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	mr := got.GetModemReady()
	if mr == nil || mr.Pid != 7 || mr.Version != "v0" {
		t.Fatalf("want ModemReady{v0,7}, got %+v", got)
	}
}
