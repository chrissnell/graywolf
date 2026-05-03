package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/chrissnell/graywolf/pkg/remoteactions"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestCreateAndListRemoteOTPCredential(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(dto.RemoteOTPCredentialRequest{
		Name:      "NW5W OTP",
		SecretB32: "JBSWY3DPEHPK3PXP",
	})
	req := httptest.NewRequest("POST", "/api/remote-actions/credentials", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusCreated {
		t.Fatalf("create status %d body=%s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/api/remote-actions/credentials", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list status %d", rr.Code)
	}
	var got []dto.RemoteOTPCredential
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0].Name != "NW5W OTP" {
		t.Fatalf("unexpected list: %+v", got)
	}
}

func TestCreateRemoteOTPCredentialRejectsBadSecret(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(dto.RemoteOTPCredentialRequest{Name: "x", SecretB32: "!!!"})
	req := httptest.NewRequest("POST", "/api/remote-actions/credentials", bytes.NewReader(body))
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status %d body=%s", rr.Code, rr.Body.String())
	}
}

// TestUpdateCredEmptySecretLeavesSecretAlone: PUT with SecretB32=""
// must NOT clear the stored secret. The DTO doc says empty leaves it
// untouched; this test pins that contract.
func TestUpdateCredEmptySecretLeavesSecretAlone(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body, _ := json.Marshal(dto.RemoteOTPCredentialRequest{
		Name: "NW5W OTP", SecretB32: "JBSWY3DPEHPK3PXP",
	})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/remote-actions/credentials", bytes.NewReader(body)))
	if rr.Code != http.StatusCreated {
		t.Fatalf("seed: %d", rr.Code)
	}

	// PUT with new name only; secret_b32 omitted/empty.
	body, _ = json.Marshal(dto.RemoteOTPCredentialRequest{
		Name: "renamed",
	})
	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("PUT", "/api/remote-actions/credentials/1", bytes.NewReader(body)))
	if rr.Code != http.StatusOK {
		t.Fatalf("update: %d body=%s", rr.Code, rr.Body.String())
	}

	got, err := srv.remoteActions.Creds().Get(t.Context(), 1)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.SecretB32 != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("secret was rewritten: %q", got.SecretB32)
	}
	if got.Name != "renamed" {
		t.Fatalf("name not applied: %q", got.Name)
	}
}

// TestUpdateCredDuplicateNameReturns409: PUT that collides with
// another credential's UNIQUE name must return 409.
func TestUpdateCredDuplicateNameReturns409(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	for _, name := range []string{"first", "second"} {
		body, _ := json.Marshal(dto.RemoteOTPCredentialRequest{
			Name: name, SecretB32: "JBSWY3DPEHPK3PXP",
		})
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest("POST", "/api/remote-actions/credentials", bytes.NewReader(body)))
		if rr.Code != http.StatusCreated {
			t.Fatalf("seed %s: %d", name, rr.Code)
		}
	}

	body, _ := json.Marshal(dto.RemoteOTPCredentialRequest{Name: "first"})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest("PUT", "/api/remote-actions/credentials/2", bytes.NewReader(body)))
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}
}

func TestDeleteRemoteOTPCredentialBlockedWhenInUse(t *testing.T) {
	srv, cleanup := newTestServerWithRemoteActions(t)
	defer cleanup()
	// seed a credential and bind a macro to it
	cred := &remoteactions.RemoteOTPCredential{Name: "NW5W OTP", SecretB32: "JBSWY3DPEHPK3PXP"}
	if err := srv.remoteActions.Creds().Create(t.Context(), cred); err != nil {
		t.Fatalf("seed cred: %v", err)
	}
	if err := srv.remoteActions.Macros().Create(t.Context(), &remoteactions.RemoteActionMacro{
		TargetCall: "KK7XYZ-9", Label: "x", ActionName: "x",
		RemoteOTPCredentialID: &cred.ID,
	}); err != nil {
		t.Fatalf("seed macro: %v", err)
	}
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("DELETE", "/api/remote-actions/credentials/1", nil)
	mux.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rr.Code, rr.Body.String())
	}
}
