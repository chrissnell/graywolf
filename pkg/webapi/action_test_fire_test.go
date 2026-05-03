package webapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/chrissnell/graywolf/pkg/actions"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

// recordingTestFire captures the action and args last passed to
// TestFire so the test can assert OTP/allowlist were bypassed and
// sanitized args were forwarded.
type recordingTestFire struct {
	fakeActionsService
	gotAction *configstore.Action
	gotArgs   []actions.KeyValue
	result    actions.Result
	invID     uint
}

func (r *recordingTestFire) TestFire(_ context.Context, a *configstore.Action, args []actions.KeyValue) (actions.Result, uint) {
	r.testFires.Add(1)
	r.gotAction = a
	r.gotArgs = args
	if r.result.Status == "" {
		r.result.Status = actions.StatusOK
		r.result.OutputCapture = "ok"
	}
	return r.result, r.invID
}

// makeTestFireAction creates a webhook-shaped Action via the REST API
// (so the schema/etc round-trip through validation) and returns its
// id. Webhook is used so the action survives without a real binary on
// disk.
func makeTestFireAction(t *testing.T, mux *http.ServeMux) uint {
	t.Helper()
	in := dto.Action{
		Name: "tf", Type: "webhook", WebhookMethod: "GET",
		WebhookURL: "https://example.test/", TimeoutSec: 5,
		Enabled:   true,
		ArgSchema: []dto.ArgSpec{{Key: "k", MaxLen: 8}},
	}
	body, _ := json.Marshal(in)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/actions", bytes.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var got dto.Action
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	return got.ID
}

func TestFireAction_Success(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	rec := &recordingTestFire{invID: 42}
	srv.SetActionsService(rec)
	id := makeTestFireAction(t, mux)

	body := strings.NewReader(`{"args":{"k":"v"}}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/actions/"+strconv.FormatUint(uint64(id), 10)+"/test-fire", body))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got dto.TestFireResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "ok" {
		t.Fatalf("status=%q, want ok", got.Status)
	}
	if got.InvocationID != 42 {
		t.Fatalf("invocation_id=%d, want 42", got.InvocationID)
	}
	if rec.gotAction == nil || rec.gotAction.ID != id {
		t.Fatalf("action not forwarded: %+v", rec.gotAction)
	}
	if len(rec.gotArgs) != 1 || rec.gotArgs[0].Key != "k" || rec.gotArgs[0].Value != "v" {
		t.Fatalf("args not sanitized: %+v", rec.gotArgs)
	}
}

func TestFireAction_TruncatedFlagPropagates(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	rec := &recordingTestFire{
		// 80 chars of output → FormatReply truncates to fit the
		// 67-char APRS message cap, so the response Truncated must
		// be true.
		result: actions.Result{
			Status:        actions.StatusOK,
			OutputCapture: strings.Repeat("a", 80),
		},
	}
	srv.SetActionsService(rec)
	id := makeTestFireAction(t, mux)

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost,
		"/api/actions/"+strconv.FormatUint(uint64(id), 10)+"/test-fire",
		strings.NewReader(`{"args":{"k":"v"}}`)))
	if rr.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rr.Code, rr.Body.String())
	}
	var got dto.TestFireResponse
	if err := json.NewDecoder(rr.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Truncated {
		t.Fatalf("expected truncated=true, got %+v", got)
	}
}

func TestFireAction_BadArgRejected(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	rec := &recordingTestFire{}
	srv.SetActionsService(rec)
	id := makeTestFireAction(t, mux)

	// Value too long for declared MaxLen=8.
	body := strings.NewReader(`{"args":{"k":"toolongvalue"}}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/actions/"+strconv.FormatUint(uint64(id), 10)+"/test-fire", body))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s, want 400", rr.Code, rr.Body.String())
	}
	if rec.testFires.Load() != 0 {
		t.Fatalf("TestFire was called despite bad arg: %d", rec.testFires.Load())
	}
}

func TestFireAction_NoActionsService(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	id := uint(1)
	body := strings.NewReader(`{"args":{}}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/actions/"+strconv.FormatUint(uint64(id), 10)+"/test-fire", body))
	if rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", rr.Code)
	}
}

func TestFireAction_NotFound(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	srv.SetActionsService(&recordingTestFire{})
	body := strings.NewReader(`{"args":{}}`)
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/actions/9999/test-fire", body))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want 404", rr.Code)
	}
}
