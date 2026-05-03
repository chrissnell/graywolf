package actions

import (
	"context"
	"errors"
	"testing"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
)

type recordingSender struct {
	got messages.SendMessageRequest
	err error
}

func (r *recordingSender) SendMessage(_ context.Context, req messages.SendMessageRequest) (*configstore.Message, error) {
	r.got = req
	if r.err != nil {
		return nil, r.err
	}
	return &configstore.Message{ID: 1, ToCall: req.To, Text: req.Text}, nil
}

func TestMessagesReplySenderRFInboundUsesISFallback(t *testing.T) {
	rec := &recordingSender{}
	a := newMessagesReplySenderForTest(rec, func() string { return "N0CALL-7" })
	if err := a.SendReply(context.Background(), 0, SourceRF, "K1ABC", "ok"); err != nil {
		t.Fatal(err)
	}
	if rec.got.To != "K1ABC" || rec.got.Text != "ok" {
		t.Fatalf("unexpected request: %+v", rec.got)
	}
	if rec.got.OurCall != "N0CALL-7" {
		t.Fatalf("expected OurCall=N0CALL-7, got %q", rec.got.OurCall)
	}
	if rec.got.ThreadKind != messages.ThreadKindDM {
		t.Fatalf("expected DM thread, got %q", rec.got.ThreadKind)
	}
	if rec.got.FallbackPolicyOverride != messages.FallbackPolicyISFallback {
		t.Fatalf("RF inbound should override to is_fallback, got %q", rec.got.FallbackPolicyOverride)
	}
}

func TestMessagesReplySenderISInboundUsesISOnly(t *testing.T) {
	rec := &recordingSender{}
	a := newMessagesReplySenderForTest(rec, func() string { return "N0CALL" })
	if err := a.SendReply(context.Background(), 0, SourceIS, "K1ABC", "ok"); err != nil {
		t.Fatal(err)
	}
	if rec.got.FallbackPolicyOverride != messages.FallbackPolicyISOnly {
		t.Fatalf("IS inbound should override to is_only, got %q", rec.got.FallbackPolicyOverride)
	}
}

func TestMessagesReplySenderPropagatesError(t *testing.T) {
	want := errors.New("boom")
	rec := &recordingSender{err: want}
	a := newMessagesReplySenderForTest(rec, func() string { return "N0CALL" })
	err := a.SendReply(context.Background(), 0, SourceRF, "K1ABC", "ok")
	if !errors.Is(err, want) {
		t.Fatalf("expected wrapped error, got %v", err)
	}
}

func TestMessagesReplySenderRequiresOurCall(t *testing.T) {
	rec := &recordingSender{}
	a := newMessagesReplySenderForTest(rec, func() string { return "" })
	if err := a.SendReply(context.Background(), 0, SourceRF, "K1ABC", "ok"); err == nil {
		t.Fatal("expected error when OurCall is empty")
	}
}
