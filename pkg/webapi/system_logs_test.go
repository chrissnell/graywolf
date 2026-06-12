package webapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/logbuffer"
)

// fakeLogSource is an in-memory SystemLogSource for handler tests.
type fakeLogSource struct {
	recs []logbuffer.Record
	last logbuffer.QueryOptions
}

func (f *fakeLogSource) Query(opts logbuffer.QueryOptions) ([]logbuffer.Record, error) {
	f.last = opts
	return f.recs, nil
}

func TestSystemLogsReturnsEntries(t *testing.T) {
	src := &fakeLogSource{recs: []logbuffer.Record{
		{Time: time.Unix(100, 0).UTC(), Level: "INFO", Component: "webapi", Message: "hi", Attrs: map[string]any{"k": "v"}},
	}}
	mux := http.NewServeMux()
	RegisterSystemLogs(nil, mux, src)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/system-logs?limit=50", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp SystemLogsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !resp.Available || len(resp.Logs) != 1 {
		t.Fatalf("resp = %+v", resp)
	}
	if resp.Logs[0].Message != "hi" || resp.Logs[0].Level != "INFO" {
		t.Fatalf("entry = %+v", resp.Logs[0])
	}
	if resp.Logs[0].Timestamp != time.Unix(100, 0).UTC().Format(time.RFC3339) {
		t.Fatalf("timestamp = %q", resp.Logs[0].Timestamp)
	}
	if src.last.Limit != 50 {
		t.Fatalf("limit not parsed: %d", src.last.Limit)
	}
}

func TestSystemLogsDefaultExcludesDebug(t *testing.T) {
	src := &fakeLogSource{}
	mux := http.NewServeMux()
	RegisterSystemLogs(nil, mux, src)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/system-logs", nil))
	// Default level filter is INFO+; DEBUG must not be requested.
	for _, lv := range src.last.Levels {
		if lv == "DEBUG" {
			t.Fatalf("default query should not include DEBUG: %v", src.last.Levels)
		}
	}
	if len(src.last.Levels) == 0 {
		t.Fatalf("default query should constrain levels to INFO+")
	}
}

func TestSystemLogsLevelDebugIncludesAll(t *testing.T) {
	src := &fakeLogSource{}
	mux := http.NewServeMux()
	RegisterSystemLogs(nil, mux, src)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/system-logs?level=debug", nil))
	if len(src.last.Levels) != 0 {
		t.Fatalf("level=debug should request all levels (empty filter), got %v", src.last.Levels)
	}
}

func TestSystemLogsNilSourceReportsUnavailable(t *testing.T) {
	mux := http.NewServeMux()
	RegisterSystemLogs(nil, mux, nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/system-logs", nil))
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp SystemLogsResponse
	_ = json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp.Available {
		t.Fatalf("expected available=false when source is nil")
	}
	if resp.Logs == nil {
		t.Fatalf("logs should be empty array, not null")
	}
}

func TestSystemLogsBadLimit(t *testing.T) {
	src := &fakeLogSource{}
	mux := http.NewServeMux()
	RegisterSystemLogs(nil, mux, src)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/api/system-logs?limit=-1", nil))
	if rec.Code != 400 {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
