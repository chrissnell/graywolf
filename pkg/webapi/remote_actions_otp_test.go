package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/remoteactions"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestGenerateRemoteOTPCode(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	cred := &remoteactions.RemoteOTPCredential{Name: "NW5W OTP", SecretB32: "JBSWY3DPEHPK3PXP"}
	if err := srv.remoteActions.Creds().Create(t.Context(), cred); err != nil {
		t.Fatalf("seed: %v", err)
	}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/remote-actions/otp/1", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
	var got dto.RemoteOTPCode
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Code) != 6 {
		t.Fatalf("code = %q", got.Code)
	}
	if got.ExpiresAt == "" {
		t.Fatalf("expires_at empty")
	}

	// last_used_at must have been bumped
	refresh, _ := srv.remoteActions.Creds().Get(t.Context(), 1)
	if refresh.LastUsedAt == nil {
		t.Fatalf("LastUsedAt not stamped")
	}
}

func TestGenerateRemoteOTPCodeNotFound(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/api/remote-actions/otp/999", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status %d", rr.Code)
	}
}
