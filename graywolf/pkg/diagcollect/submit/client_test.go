package submit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/flareschema"
)

func sampleBody(t *testing.T) []byte {
	t.Helper()
	f := flareschema.BuildSampleFlare()
	b, err := json.Marshal(f)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestHTTPClient_SubmitSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/submit" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("content-type = %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"flare_id":"id-1","portal_token":"tok","portal_url":"https://x","schema_version":1}`))
	}))
	defer srv.Close()

	c := NewHTTPClient(srv.URL, srv.Client())
	resp, err := c.Submit(sampleBody(t))
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if resp.FlareID != "id-1" || resp.PortalToken != "tok" {
		t.Fatalf("response: %+v", resp)
	}
}

func TestHTTPClient_400SchemaRejected(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.Copy(io.Discard, r.Body)
		_, _ = w.Write([]byte(`{"error":"schema mismatch"}`))
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, srv.Client())
	_, err := c.Submit(sampleBody(t))
	if err == nil {
		t.Fatal("err nil, want ErrSchemaRejected")
	}
	var sr ErrSchemaRejected
	if !errors.As(err, &sr) {
		t.Fatalf("err = %T, want ErrSchemaRejected", err)
	}
	if !strings.Contains(string(sr.Body), "schema mismatch") {
		t.Fatalf("body lost: %q", sr.Body)
	}
}

func TestHTTPClient_429Rate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "120")
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, srv.Client())
	_, err := c.Submit(sampleBody(t))
	var rl ErrRateLimited
	if !errors.As(err, &rl) || rl.RetryAfter != "120" {
		t.Fatalf("err = %v, want ErrRateLimited{RetryAfter:120}", err)
	}
}

func TestHTTPClient_5xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, srv.Client())
	_, err := c.Submit(sampleBody(t))
	var se ErrServerError
	if !errors.As(err, &se) || se.Status != 503 {
		t.Fatalf("err = %v, want ErrServerError{503}", err)
	}
}

func TestHTTPClient_BodyTooLargeShortCircuits(t *testing.T) {
	huge := bytes.Repeat([]byte{'.'}, 5*1024*1024+1)
	c := NewHTTPClient("http://127.0.0.1:0", nil)
	_, err := c.Submit(huge)
	var tl ErrPayloadTooLarge
	if !errors.As(err, &tl) {
		t.Fatalf("err = %v, want ErrPayloadTooLarge", err)
	}
}

func TestHTTPClient_Update(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/submit/abc/update" {
			t.Fatalf("path = %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"flare_id":"abc","portal_token":"t","portal_url":"u","schema_version":1}`))
	}))
	defer srv.Close()
	c := NewHTTPClient(srv.URL, srv.Client())
	if _, err := c.Update("abc", sampleBody(t)); err != nil {
		t.Fatalf("Update: %v", err)
	}
}
