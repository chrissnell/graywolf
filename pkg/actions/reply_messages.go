package actions

import (
	"context"
	"errors"

	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
)

// messageSender narrows messages.Service to the one method the
// adapter needs. Lets unit tests substitute a recorder without
// standing up the full service.
type messageSender interface {
	SendMessage(ctx context.Context, req messages.SendMessageRequest) (*configstore.Message, error)
}

// MessagesReplySender adapts messages.Service to the actions.ReplySender
// contract. It constructs a one-off outbound DM row through the same
// path operator-composed messages take, so replies inherit msg-id
// allocation, the operator outbound view, and the retry ladder. The
// inbound transport is reflected back to the originator: RF inbound
// uses RF-first-with-IS-fallback; IS inbound uses IS-only.
type MessagesReplySender struct {
	svc     messageSender
	ourCall func() string
}

// NewMessagesReplySender constructs an adapter. ourCall must return
// the primary station callsign (with or without SSID); messages
// service uses it as the From address on the outbound row.
func NewMessagesReplySender(svc *messages.Service, ourCall func() string) *MessagesReplySender {
	return &MessagesReplySender{svc: svc, ourCall: ourCall}
}

// newMessagesReplySenderForTest is the test-only constructor that
// accepts an interface seam. Production code uses
// NewMessagesReplySender.
func newMessagesReplySenderForTest(svc messageSender, ourCall func() string) *MessagesReplySender {
	return &MessagesReplySender{svc: svc, ourCall: ourCall}
}

func (a *MessagesReplySender) SendReply(ctx context.Context, _ uint32, source Source, toCall, text string) error {
	if a.svc == nil {
		return errors.New("actions: nil messages service")
	}
	if a.ourCall == nil {
		return errors.New("actions: nil ourCall provider")
	}
	our := a.ourCall()
	if our == "" {
		return errors.New("actions: ourCall is empty")
	}
	req := messages.SendMessageRequest{
		To:         toCall,
		Text:       text,
		OurCall:    our,
		ThreadKind: messages.ThreadKindDM,
	}
	switch source {
	case SourceIS:
		// Inbound came in over IS — RF cannot be assumed reachable.
		req.FallbackPolicyOverride = messages.FallbackPolicyISOnly
	case SourceRF:
		// Inbound came in over RF — try RF first, then IS.
		req.FallbackPolicyOverride = messages.FallbackPolicyISFallback
	}
	_, err := a.svc.SendMessage(ctx, req)
	return err
}
