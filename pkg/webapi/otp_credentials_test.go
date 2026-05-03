package webapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestOTPCredentials_CreateRevealsSecretOnce(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := strings.NewReader(`{"name":"primary"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/otp-credentials", body))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var created dto.OTPCredentialCreated
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.SecretB32 == "" || created.OtpAuthURI == "" {
		t.Fatalf("expected secret + uri on create response: %+v", created)
	}
	if !strings.HasPrefix(created.OtpAuthURI, "otpauth://totp/") {
		t.Fatalf("unexpected otpauth uri: %q", created.OtpAuthURI)
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/otp-credentials/"+strconv.FormatUint(uint64(created.ID), 10), nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("get: status=%d body=%s", rec.Code, rec.Body.String())
	}
	bodyStr := rec.Body.String()
	if strings.Contains(bodyStr, "secret_b32") || strings.Contains(bodyStr, "otpauth_uri") {
		t.Fatalf("get response leaked secret/uri: %s", bodyStr)
	}
}

func TestOTPCredentials_ListOmitsSecret(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/otp-credentials", strings.NewReader(`{"name":"primary"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/otp-credentials", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "secret_b32") {
		t.Fatalf("list leaked secret: %s", rec.Body.String())
	}
}

func TestOTPCredentials_DuplicateNameConflict(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	body := []byte(`{"name":"dup"}`)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/otp-credentials", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("first: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/otp-credentials", bytes.NewReader(body)))
	if rec.Code != http.StatusConflict {
		t.Fatalf("second: %d, want 409", rec.Code)
	}
}

func TestOTPCredentials_Delete(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/otp-credentials", strings.NewReader(`{"name":"x"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created dto.OTPCredentialCreated
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	id := strconv.FormatUint(uint64(created.ID), 10)

	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/otp-credentials/"+id, nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/otp-credentials/"+id, nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("post-delete get: %d, want 404", rec.Code)
	}
}

func TestOTPCredentials_UsedByPopulated(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)

	// Create a credential.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/otp-credentials", strings.NewReader(`{"name":"shared"}`)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create cred: %d %s", rec.Code, rec.Body.String())
	}
	var cred dto.OTPCredentialCreated
	if err := json.NewDecoder(rec.Body).Decode(&cred); err != nil {
		t.Fatal(err)
	}
	credID := cred.ID

	// Create an Action that references it.
	cmd := writeExecScript(t)
	in := newActionRequest("ref", cmd)
	in.OTPRequired = true
	in.OTPCredentialID = &credID
	body, _ := json.Marshal(in)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create action: %d %s", rec.Code, rec.Body.String())
	}

	// List credentials and verify used_by includes the action name.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/otp-credentials", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
	}
	var got []dto.OTPCredential
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || len(got[0].UsedBy) != 1 || got[0].UsedBy[0] != "ref" {
		t.Fatalf("used_by mismatch: %+v", got)
	}
}
