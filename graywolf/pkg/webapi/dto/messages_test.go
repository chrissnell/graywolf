package dto

import (
	"testing"
	"time"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
)

func TestDeriveMessageStatus(t *testing.T) {
	now := time.Now()
	nowPtr := &now

	tests := []struct {
		name string
		in   configstore.Message
		want string
	}{
		// Inbound
		{
			name: "inbound_received",
			in:   configstore.Message{Direction: "in", ThreadKind: messages.ThreadKindDM},
			want: MessageStatusReceived,
		},
		{
			name: "inbound_tactical_received",
			in:   configstore.Message{Direction: "in", ThreadKind: messages.ThreadKindTactical},
			want: MessageStatusReceived,
		},
		// Outbound DM — queued
		{
			name: "outbound_dm_queued_no_attempts",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindDM,
				AckState: messages.AckStateNone, Attempts: 0, SentAt: nil,
			},
			want: MessageStatusQueued,
		},
		{
			name: "outbound_dm_tx_submitted",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindDM,
				AckState: messages.AckStateNone, Attempts: 1, SentAt: nil,
			},
			want: MessageStatusTxSubmitted,
		},
		// Sent awaiting ack
		{
			name: "outbound_dm_sent_rf_awaiting",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindDM,
				AckState: messages.AckStateNone, Attempts: 1, SentAt: nowPtr,
				Source: "rf",
			},
			want: MessageStatusSentRF,
		},
		{
			name: "outbound_dm_sent_is_awaiting",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindDM,
				AckState: messages.AckStateNone, Attempts: 1, SentAt: nowPtr,
				Source: "is",
			},
			want: MessageStatusSentIS,
		},
		// Acked / rejected / timeout / failed
		{
			name: "outbound_dm_acked",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindDM,
				AckState: messages.AckStateAcked, SentAt: nowPtr, AckedAt: nowPtr,
			},
			want: MessageStatusAcked,
		},
		{
			name: "outbound_dm_rejected_peer_rej",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindDM,
				AckState: messages.AckStateRejected, FailureReason: "",
			},
			want: MessageStatusRejected,
		},
		{
			name: "outbound_dm_timeout",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindDM,
				AckState: messages.AckStateRejected, FailureReason: "retry budget exhausted",
			},
			want: MessageStatusTimeout,
		},
		{
			name: "outbound_dm_failed_permanent",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindDM,
				AckState: messages.AckStateRejected, FailureReason: "send error: invalid path",
			},
			want: MessageStatusFailed,
		},
		// Tactical outbound
		{
			name: "outbound_tactical_queued",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindTactical,
				AckState: messages.AckStateNone, Attempts: 0, SentAt: nil,
			},
			want: MessageStatusQueued,
		},
		{
			name: "outbound_tactical_tx_submitted",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindTactical,
				AckState: messages.AckStateNone, Attempts: 1, SentAt: nil,
			},
			want: MessageStatusTxSubmitted,
		},
		{
			name: "outbound_tactical_broadcast_terminal",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindTactical,
				AckState: messages.AckStateBroadcast, SentAt: nowPtr,
			},
			want: MessageStatusBroadcast,
		},
		{
			name: "outbound_tactical_sent_no_broadcast_state",
			in: configstore.Message{
				Direction: "out", ThreadKind: messages.ThreadKindTactical,
				AckState: messages.AckStateNone, Attempts: 1, SentAt: nowPtr,
			},
			want: MessageStatusBroadcast,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := DeriveMessageStatus(tc.in)
			if got != tc.want {
				t.Errorf("DeriveMessageStatus() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestValidateAddressee(t *testing.T) {
	valid := []string{
		"W1ABC", "N0CALL-9", "N0CALL-15", "NET1", "NET-1", "ABC-12",
		"SHELTER1",
		// 9-char tactical with hyphen inside — matches the tactical
		// alternate `[A-Z0-9-]{1,9}` branch.
		"W1ABC-ABC",
	}
	for _, v := range valid {
		if err := ValidateAddressee(v); err != nil {
			t.Errorf("ValidateAddressee(%q) returned error: %v", v, err)
		}
	}
	invalid := []string{
		"", "   ",
		"TOOLONGCALLSIGN", // >9 chars — neither branch matches
		"A B",             // space — no branch accepts whitespace
		"FOO$",            // bad character
	}
	for _, v := range invalid {
		if err := ValidateAddressee(v); err == nil {
			t.Errorf("ValidateAddressee(%q) should have failed", v)
		}
	}
}

func TestValidateMessageText(t *testing.T) {
	if err := ValidateMessageText(""); err == nil {
		t.Error("empty text should fail")
	}
	ok := "hello world"
	if err := ValidateMessageText(ok); err != nil {
		t.Errorf("normal text should pass: %v", err)
	}
	// Exactly 67 chars — boundary.
	at := ""
	for i := 0; i < 67; i++ {
		at += "x"
	}
	if err := ValidateMessageText(at); err != nil {
		t.Errorf("67 chars should pass: %v", err)
	}
	if err := ValidateMessageText(at + "y"); err == nil {
		t.Error("68 chars should fail")
	}
}

func TestSendMessageRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     SendMessageRequest
		wantErr bool
	}{
		{"ok", SendMessageRequest{To: "W1ABC", Text: "hi"}, false},
		{"bad_addr", SendMessageRequest{To: "", Text: "hi"}, true},
		{"empty_text", SendMessageRequest{To: "W1ABC", Text: ""}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}

func TestMessageFromModelRoundtrip(t *testing.T) {
	now := time.Now().UTC()
	m := configstore.Message{
		ID:         42,
		Direction:  "out",
		OurCall:    "N0CALL",
		PeerCall:   "W1ABC",
		FromCall:   "N0CALL",
		ToCall:     "W1ABC",
		Text:       "hi",
		MsgID:      "001",
		CreatedAt:  now,
		SentAt:     &now,
		Source:     "rf",
		Channel:    2,
		ThreadKind: messages.ThreadKindDM,
		ThreadKey:  "W1ABC",
		AckState:   messages.AckStateNone,
		Attempts:   1,
	}
	out := MessageFromModel(m)
	if out.ID != 42 {
		t.Errorf("ID mismatch: got %d", out.ID)
	}
	if out.Status != MessageStatusSentRF {
		t.Errorf("Status mismatch: got %q", out.Status)
	}
	if out.Channel == nil || *out.Channel != 2 {
		t.Errorf("Channel mismatch: got %v", out.Channel)
	}
}

func TestMessagePreferencesRequestValidate(t *testing.T) {
	tests := []struct {
		name    string
		req     MessagePreferencesRequest
		wantErr bool
	}{
		{"ok", MessagePreferencesRequest{FallbackPolicy: "is_fallback", RetryMaxAttempts: 5}, false},
		{"empty_policy_allowed", MessagePreferencesRequest{RetryMaxAttempts: 5}, false},
		{"bad_policy", MessagePreferencesRequest{FallbackPolicy: "nope"}, true},
		{"retry_cap", MessagePreferencesRequest{FallbackPolicy: "rf_only", RetryMaxAttempts: 9999}, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.req.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() err=%v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
}
