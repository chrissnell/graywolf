package configstore

import (
	"context"
	"testing"
	"time"
)

func TestMigrateActionsMaxReplyLines(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()

	a := &Action{
		Name:           "MULTILINE",
		Type:           "command",
		CommandPath:    "/bin/echo",
		ArgSchema:      "[]",
		ArgMode:        "kv",
		WebhookHeaders: "{}",
		TimeoutSec:     5,
		Enabled:        true,
	}
	if err := s.CreateAction(ctx, a); err != nil {
		t.Fatalf("create action: %v", err)
	}

	got, err := s.GetActionByName(ctx, "MULTILINE")
	if err != nil {
		t.Fatalf("get action: %v", err)
	}
	if got.MaxReplyLines != 1 {
		t.Fatalf("default MaxReplyLines: want 1, got %d", got.MaxReplyLines)
	}

	got.MaxReplyLines = 3
	if err := s.UpdateAction(ctx, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	again, err := s.GetActionByName(ctx, "MULTILINE")
	if err != nil {
		t.Fatalf("re-get: %v", err)
	}
	if again.MaxReplyLines != 3 {
		t.Fatalf("MaxReplyLines round-trip: want 3, got %d", again.MaxReplyLines)
	}

	inv := &ActionInvocation{
		ActionID:       &again.ID,
		ActionNameAt:   again.Name,
		SenderCall:     "N0CALL",
		Source:         "rf",
		Status:         "ok",
		ReplyText:      "ok: a\nb\nc",
		ReplyLineCount: 3,
		CreatedAt:      time.Now(),
	}
	if err := s.InsertActionInvocation(ctx, inv); err != nil {
		t.Fatalf("insert invocation: %v", err)
	}

	// Read back via the listing API to confirm gorm round-trips ReplyLineCount.
	rows, err := s.ListActionInvocations(ctx, ActionInvocationFilter{Limit: 10})
	if err != nil {
		t.Fatalf("list invocations: %v", err)
	}
	var found *ActionInvocation
	for i := range rows {
		if rows[i].ID == inv.ID {
			found = &rows[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("invocation row %d not in listing", inv.ID)
	}
	if found.ReplyLineCount != 3 {
		t.Fatalf("ReplyLineCount round-trip: want 3, got %d", found.ReplyLineCount)
	}
}
