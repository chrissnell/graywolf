package actions

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

func TestWebhookGetTokenExpansion(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, "thanks")
	}))
	defer srv.Close()
	a := &configstore.Action{
		Type:          "webhook",
		WebhookMethod: "GET",
		WebhookURL:    srv.URL + "/{{action}}?call={{sender-callsign}}&val={{arg.x}}",
		TimeoutSec:    5,
	}
	exe := NewWebhookExecutor()
	res := exe.Execute(context.Background(), ExecRequest{
		Action:     a,
		Invocation: Invocation{ActionName: "TurnOn", SenderCall: "NW5W-7", Args: []KeyValue{{Key: "x", Value: "a b"}}},
		Timeout:    5 * time.Second,
	})
	if res.Status != StatusOK {
		t.Fatalf("status=%v detail=%q", res.Status, res.StatusDetail)
	}
	if got.URL.Path != "/TurnOn" {
		t.Fatalf("path: %q", got.URL.Path)
	}
	if got.URL.Query().Get("val") != "a b" {
		t.Fatalf("url-decode of token: %q", got.URL.Query().Get("val"))
	}
}

func TestWebhookPostDefaultForm(t *testing.T) {
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()
	a := &configstore.Action{
		Type:          "webhook",
		WebhookMethod: "POST",
		WebhookURL:    srv.URL + "/x",
		TimeoutSec:    5,
	}
	exe := NewWebhookExecutor()
	res := exe.Execute(context.Background(), ExecRequest{
		Action:     a,
		Invocation: Invocation{ActionName: "Foo", SenderCall: "NW5W-7", Source: SourceIS, Args: []KeyValue{{Key: "k", Value: "v"}}},
		Timeout:    5 * time.Second,
	})
	if res.Status != StatusOK {
		t.Fatalf("status=%v", res.Status)
	}
	if !strings.Contains(body, "action=Foo") || !strings.Contains(body, "k=v") {
		t.Fatalf("default body: %q", body)
	}
}

func TestWebhookNon2xxReportsStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_, _ = io.WriteString(w, "down")
	}))
	defer srv.Close()
	a := &configstore.Action{Type: "webhook", WebhookMethod: "GET", WebhookURL: srv.URL, TimeoutSec: 5}
	res := NewWebhookExecutor().Execute(context.Background(), ExecRequest{
		Action: a, Invocation: Invocation{}, Timeout: 5 * time.Second,
	})
	if res.Status != StatusError || res.HTTPStatus == nil || *res.HTTPStatus != 503 {
		t.Fatalf("status=%v http=%v", res.Status, res.HTTPStatus)
	}
}

func TestWebhookPostBodyTemplateExpansion(t *testing.T) {
	var body, contentType string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		contentType = r.Header.Get("Content-Type")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	a := &configstore.Action{
		Type:               "webhook",
		WebhookMethod:      "POST",
		WebhookURL:         srv.URL,
		WebhookBodyTemplate: `{"action":"{{action}}","sender":"{{sender-callsign}}","x":"{{arg.x}}"}`,
		TimeoutSec:         5,
	}
	res := NewWebhookExecutor().Execute(context.Background(), ExecRequest{
		Action:     a,
		Invocation: Invocation{ActionName: "Open", SenderCall: "NW5W-7", Args: []KeyValue{{Key: "x", Value: "yes"}}},
		Timeout:    5 * time.Second,
	})
	if res.Status != StatusOK {
		t.Fatalf("status=%v detail=%q", res.Status, res.StatusDetail)
	}
	want := `{"action":"Open","sender":"NW5W-7","x":"yes"}`
	if body != want {
		t.Fatalf("body=%q want=%q", body, want)
	}
	if contentType != "" {
		t.Fatalf("custom-template POST should not force form Content-Type, got %q", contentType)
	}
}

func TestWebhookHeaderTokenExpansion(t *testing.T) {
	var got http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	a := &configstore.Action{
		Type:           "webhook",
		WebhookMethod:  "GET",
		WebhookURL:     srv.URL,
		WebhookHeaders: `{"X-Caller":"{{sender-callsign}}","X-Token":"{{arg.tok}}"}`,
		TimeoutSec:     5,
	}
	res := NewWebhookExecutor().Execute(context.Background(), ExecRequest{
		Action:     a,
		Invocation: Invocation{SenderCall: "NW5W-7", Args: []KeyValue{{Key: "tok", Value: "abc123"}}},
		Timeout:    5 * time.Second,
	})
	if res.Status != StatusOK {
		t.Fatalf("status=%v detail=%q", res.Status, res.StatusDetail)
	}
	if got.Get("X-Caller") != "NW5W-7" {
		t.Fatalf("X-Caller=%q", got.Get("X-Caller"))
	}
	if got.Get("X-Token") != "abc123" {
		t.Fatalf("X-Token=%q", got.Get("X-Token"))
	}
}

func TestWebhookTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	a := &configstore.Action{Type: "webhook", WebhookMethod: "GET", WebhookURL: srv.URL, TimeoutSec: 1}
	start := time.Now()
	res := NewWebhookExecutor().Execute(context.Background(), ExecRequest{
		Action: a, Invocation: Invocation{}, Timeout: 200 * time.Millisecond,
	})
	if res.Status != StatusTimeout {
		t.Fatalf("status=%v detail=%q (elapsed %v)", res.Status, res.StatusDetail, time.Since(start))
	}
	if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
		t.Fatalf("ctx timeout did not fire promptly: %v", elapsed)
	}
}

func TestWebhookResponseBodyCap(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(strings.Repeat("Z", 5*1024)))
	}))
	defer srv.Close()
	a := &configstore.Action{Type: "webhook", WebhookMethod: "GET", WebhookURL: srv.URL, TimeoutSec: 5}
	res := NewWebhookExecutor().Execute(context.Background(), ExecRequest{
		Action: a, Invocation: Invocation{}, Timeout: 5 * time.Second,
	})
	if res.Status != StatusOK {
		t.Fatalf("status=%v", res.Status)
	}
	if got := len(res.OutputCapture); got != webhookOutputCap {
		t.Fatalf("OutputCapture len = %d, want %d (cap)", got, webhookOutputCap)
	}
}

func TestWebhookBadHeadersJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("server should not receive request when headers JSON is malformed")
	}))
	defer srv.Close()
	a := &configstore.Action{
		Type:           "webhook",
		WebhookMethod:  "GET",
		WebhookURL:     srv.URL,
		WebhookHeaders: `{"X-Bad":`,
		TimeoutSec:     5,
	}
	res := NewWebhookExecutor().Execute(context.Background(), ExecRequest{
		Action: a, Invocation: Invocation{}, Timeout: 5 * time.Second,
	})
	if res.Status != StatusError {
		t.Fatalf("status=%v detail=%q", res.Status, res.StatusDetail)
	}
}

func TestWebhookRedirectBlocked(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("redirect target should never be hit")
	}))
	defer target.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer srv.Close()
	a := &configstore.Action{Type: "webhook", WebhookMethod: "GET", WebhookURL: srv.URL, TimeoutSec: 5}
	res := NewWebhookExecutor().Execute(context.Background(), ExecRequest{
		Action: a, Invocation: Invocation{}, Timeout: 5 * time.Second,
	})
	// 302 must surface as error, not be chased — guards against SSRF amp
	// to RFC1918 / link-local / loopback metadata endpoints.
	if res.Status != StatusError {
		t.Fatalf("redirect should classify as error, got status=%v http=%v", res.Status, res.HTTPStatus)
	}
	if res.HTTPStatus == nil || *res.HTTPStatus != http.StatusFound {
		t.Fatalf("expected HTTP 302 surfaced, got %v", res.HTTPStatus)
	}
}

func TestExpandTokenIsDeterministicWithBraceArg(t *testing.T) {
	// If a per-key regex override permits `{` / `}`, an arg value can
	// contain a substring that *looks* like another token. With a
	// single-pass replacer, that substring must appear verbatim in the
	// output — never re-substituted, never reordered. Run many times
	// to defeat any map-iteration randomness regression.
	inv := Invocation{
		ActionName: "Open",
		Args:       []KeyValue{{Key: "x", Value: "{{arg.y}}"}, {Key: "y", Value: "SECRET"}},
	}
	want := "X={{arg.y}} Y=SECRET"
	for i := range 100 {
		got := expandToken("X={{arg.x}} Y={{arg.y}}", inv, identityEncoder)
		if got != want {
			t.Fatalf("iter %d: got %q want %q", i, got, want)
		}
	}
}
