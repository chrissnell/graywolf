package messages

import (
	"context"
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
)

// TestService_SendMessage_StampsConversationSendPath asserts that a
// per-conversation SendPath override is stamped onto the outbound row
// so retry re-attempts route the same way as the initial send.
func TestService_SendMessage_StampsConversationSendPath(t *testing.T) {
	svc, rig, _, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	if err := rig.cs.UpsertConversationPrefs(ctx, &configstore.ConversationPrefs{
		ThreadKind: ThreadKindDM, ThreadKey: "W1ABC", SendPath: FallbackPolicyRFOnly, WaitForAck: true,
	}); err != nil {
		t.Fatalf("seed prefs: %v", err)
	}

	row, err := svc.SendMessage(ctx, SendMessageRequest{OurCall: "N0CALL", To: "W1ABC", Text: "hi"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}
	if row.SendPath != FallbackPolicyRFOnly {
		t.Fatalf("row.SendPath = %q, want %q", row.SendPath, FallbackPolicyRFOnly)
	}
}

// TestService_SendMessage_WaitForAckFalseSkipsRetry asserts that a
// conversation with WaitForAck=false sends once and does NOT enroll the
// row in the retry ladder — the no-ACK-device case (issue #453).
func TestService_SendMessage_WaitForAckFalseSkipsRetry(t *testing.T) {
	svc, rig, _, cleanup := buildService(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := svc.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer svc.Stop()

	if err := rig.cs.UpsertConversationPrefs(ctx, &configstore.ConversationPrefs{
		ThreadKind: ThreadKindDM, ThreadKey: "W1ABC", SendPath: "", WaitForAck: false,
	}); err != nil {
		t.Fatalf("seed prefs: %v", err)
	}

	row, err := svc.SendMessage(ctx, SendMessageRequest{OurCall: "N0CALL", To: "W1ABC", Text: "hi"})
	if err != nil {
		t.Fatalf("SendMessage: %v", err)
	}

	// Wait for the async initial send to hit the RF sink.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if len(rig.sink.list()) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if len(rig.sink.list()) == 0 {
		t.Fatal("no RF submit after SendMessage")
	}
	// Give the (skipped) enrollment path a beat to run if it were going to.
	time.Sleep(20 * time.Millisecond)

	cur, err := rig.store.GetByID(ctx, row.ID)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if cur.NextRetryAt != nil {
		t.Errorf("NextRetryAt = %v, want nil (no retry enrollment for no-ACK contact)", cur.NextRetryAt)
	}
	if cur.Attempts != 0 {
		t.Errorf("Attempts = %d, want 0 (not enrolled)", cur.Attempts)
	}
}
