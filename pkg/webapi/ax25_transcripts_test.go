package webapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/webapi/dto"
)

func TestListGetDeleteAX25Transcripts(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()

	sess := &configstore.AX25TranscriptSession{
		ChannelID: 1, PeerCall: "W1AW", PeerSSID: 0,
	}
	if err := srv.store.CreateAX25TranscriptSession(ctx, sess); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := srv.store.AppendAX25TranscriptEntry(ctx, &configstore.AX25TranscriptEntry{
		SessionID: sess.ID, TS: time.Now().UTC(), Direction: "rx", Kind: "data",
		Payload: []byte("hello"),
	}); err != nil {
		t.Fatalf("append: %v", err)
	}

	// List.
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/ax25/transcripts", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rows []dto.AX25TranscriptSession
	if err := json.NewDecoder(rec.Body).Decode(&rows); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(rows) != 1 || rows[0].PeerCall != "W1AW" {
		t.Fatalf("list drift: %+v", rows)
	}

	// Detail.
	url := "/api/ax25/transcripts/" + itoa(sess.ID)
	rec2 := httptest.NewRecorder()
	mux.ServeHTTP(rec2, httptest.NewRequest(http.MethodGet, url, nil))
	if rec2.Code != http.StatusOK {
		t.Fatalf("detail status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var detail dto.AX25TranscriptDetail
	if err := json.NewDecoder(rec2.Body).Decode(&detail); err != nil {
		t.Fatalf("decode detail: %v", err)
	}
	if len(detail.Entries) != 1 || string(detail.Entries[0].Payload) != "hello" {
		t.Fatalf("detail drift: %+v", detail)
	}

	// Delete one.
	rec3 := httptest.NewRecorder()
	mux.ServeHTTP(rec3, httptest.NewRequest(http.MethodDelete, url, nil))
	if rec3.Code != http.StatusNoContent {
		t.Fatalf("delete status=%d body=%s", rec3.Code, rec3.Body.String())
	}
	rows2, _ := srv.store.ListAX25TranscriptSessions(ctx, 0)
	if len(rows2) != 0 {
		t.Fatalf("delete did not remove row: %+v", rows2)
	}
}

func TestDeleteAllAX25Transcripts(t *testing.T) {
	srv, _ := newTestServer(t)
	mux := http.NewServeMux()
	srv.RegisterRoutes(mux)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = srv.store.CreateAX25TranscriptSession(ctx, &configstore.AX25TranscriptSession{
			ChannelID: 1, PeerCall: "W1AW",
		})
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/ax25/transcripts", nil))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete-all status=%d body=%s", rec.Code, rec.Body.String())
	}
	rows, _ := srv.store.ListAX25TranscriptSessions(ctx, 0)
	if len(rows) != 0 {
		t.Fatalf("delete-all left rows: %+v", rows)
	}
}
