package configstore

import (
	"context"
	"testing"
)

// TestConversationPrefs_GetMissingReturnsNil asserts the common case:
// a conversation with no override row returns (nil, nil) so callers can
// fall back to the global defaults without special-casing an error.
func TestConversationPrefs_GetMissingReturnsNil(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	got, err := s.GetConversationPrefs(ctx, "dm", "W5XYZ")
	if err != nil {
		t.Fatalf("GetConversationPrefs: %v", err)
	}
	if got != nil {
		t.Fatalf("expected nil for missing row, got %+v", got)
	}
}

// TestConversationPrefs_UpsertRoundTrips writes an override and reads it
// back, then updates it in place (no duplicate row).
func TestConversationPrefs_UpsertRoundTrips(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertConversationPrefs(ctx, &ConversationPrefs{
		ThreadKind: "dm", ThreadKey: "W5XYZ", SendPath: "rf_only", WaitForAck: true,
	}); err != nil {
		t.Fatalf("UpsertConversationPrefs: %v", err)
	}
	got, err := s.GetConversationPrefs(ctx, "dm", "W5XYZ")
	if err != nil || got == nil {
		t.Fatalf("GetConversationPrefs: got=%+v err=%v", got, err)
	}
	if got.SendPath != "rf_only" || !got.WaitForAck {
		t.Fatalf("round-trip mismatch: %+v", got)
	}

	// Update in place — WaitForAck off (no-ack contact).
	if err := s.UpsertConversationPrefs(ctx, &ConversationPrefs{
		ThreadKind: "dm", ThreadKey: "W5XYZ", SendPath: "rf_only", WaitForAck: false,
	}); err != nil {
		t.Fatalf("update UpsertConversationPrefs: %v", err)
	}
	var count int64
	if err := s.db.WithContext(ctx).Model(&ConversationPrefs{}).
		Where("thread_kind = ? AND thread_key = ?", "dm", "W5XYZ").
		Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row after update, got %d", count)
	}
	got, _ = s.GetConversationPrefs(ctx, "dm", "W5XYZ")
	if got.WaitForAck {
		t.Fatalf("expected WaitForAck=false after update, got %+v", got)
	}
}

// TestConversationPrefs_UpsertDefaultsDeletesRow verifies the
// sparse-table contract: writing the default state (inherit send path +
// resend on) removes any existing override row so the table only holds
// customized conversations.
func TestConversationPrefs_UpsertDefaultsDeletesRow(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)

	if err := s.UpsertConversationPrefs(ctx, &ConversationPrefs{
		ThreadKind: "dm", ThreadKey: "W5XYZ", SendPath: "is_only", WaitForAck: true,
	}); err != nil {
		t.Fatalf("seed override: %v", err)
	}
	// Reset to defaults.
	if err := s.UpsertConversationPrefs(ctx, &ConversationPrefs{
		ThreadKind: "dm", ThreadKey: "W5XYZ", SendPath: "", WaitForAck: true,
	}); err != nil {
		t.Fatalf("reset to defaults: %v", err)
	}
	got, err := s.GetConversationPrefs(ctx, "dm", "W5XYZ")
	if err != nil {
		t.Fatalf("GetConversationPrefs: %v", err)
	}
	if got != nil {
		t.Fatalf("expected row deleted on reset-to-defaults, got %+v", got)
	}
	// Resetting a non-existent conversation is a no-op, not an error.
	if err := s.UpsertConversationPrefs(ctx, &ConversationPrefs{
		ThreadKind: "dm", ThreadKey: "NOPE", SendPath: "", WaitForAck: true,
	}); err != nil {
		t.Fatalf("reset of missing row should be a no-op: %v", err)
	}
}
