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
