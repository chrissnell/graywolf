package configstore

import (
	"context"
	"testing"
)

func TestGetLogBufferConfigDefault(t *testing.T) {
	s := newTestStore(t)
	_, exists, err := s.GetLogBufferConfig(context.Background())
	if err != nil {
		t.Fatalf("GetLogBufferConfig: %v", err)
	}
	if exists {
		t.Fatalf("exists = true on fresh DB, want false")
	}
}

func TestUpsertLogBufferConfigRoundTrip(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.UpsertLogBufferConfig(ctx, LogBufferConfig{MaxRows: 12345}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	got, exists, err := s.GetLogBufferConfig(ctx)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if !exists {
		t.Fatalf("exists = false after upsert, want true")
	}
	if got.MaxRows != 12345 {
		t.Fatalf("MaxRows = %d, want 12345", got.MaxRows)
	}

	// Update in place to zero — the disable signal. exists must stay
	// true so the consumer can distinguish "operator opted out" from
	// "no override stored".
	if err := s.UpsertLogBufferConfig(ctx, LogBufferConfig{MaxRows: 0}); err != nil {
		t.Fatalf("upsert disable: %v", err)
	}
	got, exists, _ = s.GetLogBufferConfig(ctx)
	if !exists {
		t.Fatalf("exists = false after disable upsert, want true")
	}
	if got.MaxRows != 0 {
		t.Fatalf("MaxRows after disable = %d, want 0", got.MaxRows)
	}
}

func TestUpsertLogBufferConfigRejectsUnknownID(t *testing.T) {
	s := newTestStore(t)
	// Production callers leave c.ID == 0 so the upsert finds-or-creates.
	// If a refactor ever passes a stale ID, GORM's UpdateColumns silently
	// no-ops; that's a footgun. Surface the no-op as an error.
	err := s.UpsertLogBufferConfig(context.Background(), LogBufferConfig{ID: 9999, MaxRows: 42})
	if err == nil {
		t.Fatal("upsert with unknown ID should error")
	}
}
