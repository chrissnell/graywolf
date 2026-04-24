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
	// The DTO now gates only on the hard upper ceiling — the effective
	// 67-char default cap is enforced by the preference-aware sender
	// path, not here, so long-mode operators aren't blocked at this
	// layer. 200 chars must pass; 201 must not.
	at200 := ""
	for range MaxMessageTextUnsafe {
		at200 += "x"
	}
	if err := ValidateMessageText(at200); err != nil {
		t.Errorf("%d chars (at ceiling) should pass: %v", MaxMessageTextUnsafe, err)
	}
	if err := ValidateMessageText(at200 + "y"); err == nil {
		t.Errorf("%d chars (over ceiling) should fail", MaxMessageTextUnsafe+1)
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
		// max_message_text_override: 0 is fine (default); below-default
		// and above-ceiling are rejected; in-range is accepted.
		{"override_zero_ok", MessagePreferencesRequest{MaxMessageTextOverride: 0}, false},
		{"override_at_default_rejected", MessagePreferencesRequest{MaxMessageTextOverride: 67}, true},
		{"override_just_above_default_ok", MessagePreferencesRequest{MaxMessageTextOverride: 68}, false},
		{"override_typical_ok", MessagePreferencesRequest{MaxMessageTextOverride: 150}, false},
		{"override_at_ceiling_ok", MessagePreferencesRequest{MaxMessageTextOverride: 200}, false},
		{"override_above_ceiling_rejected", MessagePreferencesRequest{MaxMessageTextOverride: 201}, true},
		{"override_one_rejected", MessagePreferencesRequest{MaxMessageTextOverride: 1}, true},
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

// TestMessagePreferencesFromModel_NormalizesOverride asserts a stored
// out-of-range override (hand-edited DB or forward-incompatible row)
// surfaces to the UI as 0 (default), not as the corrupt value.
func TestMessagePreferencesFromModel_NormalizesOverride(t *testing.T) {
	cases := []struct {
		name     string
		stored   uint32
		wantResp uint32
	}{
		{"zero", 0, 0},
		{"valid_mid", 150, 150},
		{"valid_ceiling", 200, 200},
		{"below_default_normalized", 10, 0},
		{"at_default_normalized", 67, 0},
		{"above_ceiling_normalized", 500, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			resp := MessagePreferencesFromModel(configstore.MessagePreferences{
				MaxMessageTextOverride: c.stored,
			})
			if resp.MaxMessageTextOverride != c.wantResp {
				t.Errorf("stored=%d → response=%d, want %d",
					c.stored, resp.MaxMessageTextOverride, c.wantResp)
			}
		})
	}
}

// TestMaxMessageTextConstants_AgreeWithSender guards the dual
// declarations in dto (MaxMessageText / MaxMessageTextUnsafe) and
// messages (DefaultMaxMessageText / MaxMessageTextCeiling). The two
// layers can't share a constant without pkg/messages depending on the
// webapi layer (or vice versa), so the next best thing is an
// assertion that nothing has drifted.
func TestMaxMessageTextConstants_AgreeWithSender(t *testing.T) {
	if MaxMessageText != messages.DefaultMaxMessageText {
		t.Errorf("dto.MaxMessageText=%d != messages.DefaultMaxMessageText=%d",
			MaxMessageText, messages.DefaultMaxMessageText)
	}
	if MaxMessageTextUnsafe != messages.MaxMessageTextCeiling {
		t.Errorf("dto.MaxMessageTextUnsafe=%d != messages.MaxMessageTextCeiling=%d",
			MaxMessageTextUnsafe, messages.MaxMessageTextCeiling)
	}
}

// TestMessageFromModel_ExtendedFlag confirms the badge signal fires
// iff the stored body length exceeds the default 67-char cap.
func TestMessageFromModel_ExtendedFlag(t *testing.T) {
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"short_body", "hi", false},
		{"exactly_default", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz01234", false}, // 67
		{"one_over_default", "abcdefghijklmnopqrstuvwxyz0123456789abcdefghijklmnopqrstuvwxyz012345", true}, // 68
		{"long_extended", "this is a deliberately long message that exceeds the APRS 67-char default", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if len(c.text) > 67 != c.want {
				t.Fatalf("test-case self-check failed: len=%d want-extended=%v", len(c.text), c.want)
			}
			resp := MessageFromModel(configstore.Message{
				Direction:  "out",
				ThreadKind: messages.ThreadKindDM,
				Text:       c.text,
				AckState:   messages.AckStateNone,
			})
			if resp.Extended != c.want {
				t.Errorf("Extended for len=%d = %v, want %v", len(c.text), resp.Extended, c.want)
			}
		})
	}
}
