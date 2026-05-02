package configstore

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"gorm.io/gorm"
)

func TestCreateAndListAX25TranscriptSession(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_transcripts.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		s := &AX25TranscriptSession{
			ChannelID: 1, PeerCall: "W1AW", PeerSSID: uint8(i),
			StartedAt: time.Now().UTC().Add(-time.Duration(3-i) * time.Hour),
		}
		if err := store.CreateAX25TranscriptSession(ctx, s); err != nil {
			t.Fatalf("create: %v", err)
		}
	}
	rows, err := store.ListAX25TranscriptSessions(ctx, 0)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("got %d, want 3", len(rows))
	}
	// Ordered by StartedAt DESC -> i=2 first.
	if rows[0].PeerSSID != 2 {
		t.Fatalf("ordering wrong: %+v", rows[0])
	}
}

func TestAppendAndListAX25TranscriptEntries(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_transcripts_entries.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	sess := &AX25TranscriptSession{ChannelID: 1, PeerCall: "W1AW"}
	if err := store.CreateAX25TranscriptSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	base := time.Now().UTC().Truncate(time.Second)
	for i, body := range []string{"hello", "world"} {
		e := &AX25TranscriptEntry{
			SessionID: sess.ID,
			TS:        base.Add(time.Duration(i) * time.Second),
			Direction: "rx", Kind: "data",
			Payload: []byte(body),
		}
		if err := store.AppendAX25TranscriptEntry(ctx, e); err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
	}
	entries, err := store.ListAX25TranscriptEntries(ctx, sess.ID)
	if err != nil {
		t.Fatalf("list entries: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if string(entries[0].Payload) != "hello" || string(entries[1].Payload) != "world" {
		t.Fatalf("entries out of order: %+v", entries)
	}
}

func TestAppendAX25TranscriptEntry_RejectsBadDirection(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_transcripts_bad_dir.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	err = store.AppendAX25TranscriptEntry(context.Background(), &AX25TranscriptEntry{
		SessionID: 1, Direction: "sideways",
	})
	if err == nil {
		t.Fatal("expected direction-validation error")
	}
}

func TestEndAX25TranscriptSession_StampsCounters(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_transcripts_end.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	sess := &AX25TranscriptSession{ChannelID: 1, PeerCall: "W1AW"}
	if err := store.CreateAX25TranscriptSession(ctx, sess); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := store.EndAX25TranscriptSession(ctx, sess.ID, "operator-disconnect", 1234, 7); err != nil {
		t.Fatalf("end: %v", err)
	}
	got, err := store.GetAX25TranscriptSession(ctx, sess.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.EndReason != "operator-disconnect" || got.ByteCount != 1234 || got.FrameCount != 7 {
		t.Fatalf("end fields wrong: %+v", got)
	}
	if got.EndedAt == nil {
		t.Fatal("EndedAt not stamped")
	}
	// Ending a missing session must surface the typed not-found error.
	if err := store.EndAX25TranscriptSession(ctx, 99_999, "x", 0, 0); !errors.Is(err, gorm.ErrRecordNotFound) {
		t.Fatalf("expected ErrRecordNotFound, got %v", err)
	}
}

func TestDeleteAX25TranscriptSession_CascadesEntries(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_transcripts_delete.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	sess := &AX25TranscriptSession{ChannelID: 1, PeerCall: "W1AW"}
	if err := store.CreateAX25TranscriptSession(ctx, sess); err != nil {
		t.Fatalf("create session: %v", err)
	}
	for i := 0; i < 5; i++ {
		_ = store.AppendAX25TranscriptEntry(ctx, &AX25TranscriptEntry{
			SessionID: sess.ID, Direction: "rx", Payload: []byte("x"),
		})
	}
	if err := store.DeleteAX25TranscriptSession(ctx, sess.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	entries, err := store.ListAX25TranscriptEntries(ctx, sess.ID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("entries not cascaded: %d remain", len(entries))
	}
}

func TestDeleteAllAX25Transcripts_WipesEverything(t *testing.T) {
	t.Parallel()
	dsn := filepath.Join(t.TempDir(), "ax25_transcripts_wipe.db")
	store, err := Open(dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer store.Close()
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		s := &AX25TranscriptSession{ChannelID: 1, PeerCall: "W1AW"}
		_ = store.CreateAX25TranscriptSession(ctx, s)
		_ = store.AppendAX25TranscriptEntry(ctx, &AX25TranscriptEntry{
			SessionID: s.ID, Direction: "rx", Payload: []byte("x"),
		})
	}
	if err := store.DeleteAllAX25Transcripts(ctx); err != nil {
		t.Fatalf("delete all: %v", err)
	}
	rows, _ := store.ListAX25TranscriptSessions(ctx, 0)
	if len(rows) != 0 {
		t.Fatalf("sessions not wiped: %d", len(rows))
	}
}
