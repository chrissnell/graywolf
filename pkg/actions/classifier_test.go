package actions

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
	"gorm.io/gorm"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/ax25"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
	"github.com/chrissnell/graywolf/pkg/txgovernor"
)

type stubSubmitter struct {
	submits []submitCall
	replies []replyCall
}

type submitCall struct {
	inv     Invocation
	action  *configstore.Action
	channel uint32
}

type replyCall struct {
	inv     Invocation
	channel uint32
	res     Result
}

func (s *stubSubmitter) Submit(_ context.Context, inv Invocation, a *configstore.Action, ch uint32) {
	s.submits = append(s.submits, submitCall{inv: inv, action: a, channel: ch})
}

func (s *stubSubmitter) Reply(_ context.Context, inv Invocation, ch uint32, res Result) {
	s.replies = append(s.replies, replyCall{inv: inv, channel: ch, res: res})
}

type stubActionStore struct {
	byName map[string]*configstore.Action
	err    error
}

func (s *stubActionStore) GetActionByName(_ context.Context, name string) (*configstore.Action, error) {
	if s.err != nil {
		return nil, s.err
	}
	a, ok := s.byName[name]
	if !ok {
		return nil, nil
	}
	return a, nil
}

type stubCredStore struct {
	byID map[uint]*configstore.OTPCredential
	err  error
}

func (s *stubCredStore) GetOTPCredential(_ context.Context, id uint) (*configstore.OTPCredential, error) {
	if s.err != nil {
		return nil, s.err
	}
	c, ok := s.byID[id]
	if !ok {
		return nil, nil
	}
	return c, nil
}

func ourCallProvider(call string) func() string { return func() string { return call } }

// makeMessagePkt builds an inbound message packet with the given
// addressee + body, originating from sender, on the given direction.
func makeMessagePkt(direction aprs.Direction, sender, addressee, body string, channel int) *aprs.DecodedAPRSPacket {
	return &aprs.DecodedAPRSPacket{
		Source:    sender,
		Type:      aprs.PacketMessage,
		Direction: direction,
		Channel:   channel,
		Message: &aprs.Message{
			Addressee: addressee,
			Text:      body,
		},
	}
}

func newClassifierForTest(t *testing.T, sub *stubSubmitter, actions *stubActionStore, creds *stubCredStore, listeners []string, tac *messages.TacticalSet) *Classifier {
	t.Helper()
	ls := NewAddresseeSet()
	ls.Replace(listeners)
	if tac == nil {
		tac = messages.NewTacticalSet()
	}
	return NewClassifier(ClassifierConfig{
		OurCall:     ourCallProvider("N0CALL"),
		TacticalSet: tac,
		Listeners:   ls,
		ActionStore: actions,
		CredStore:   creds,
		OTPVerifier: NewOTPVerifier(OTPVerifierConfig{Now: time.Now}),
		Runner:      sub,
	})
}

func TestClassifyNotMessageNotConsumed(t *testing.T) {
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{}, &stubCredStore{}, nil, nil)
	pkt := &aprs.DecodedAPRSPacket{Source: "OTHER", Type: aprs.PacketPosition}
	if c.Classify(context.Background(), pkt) {
		t.Fatal("non-message packet must not be consumed")
	}
}

func TestClassifyAddressedToUsNoPrefixNotConsumed(t *testing.T) {
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "hello", 0)
	if c.Classify(context.Background(), pkt) {
		t.Fatal("plain message must fall through to inbox")
	}
	if len(sub.submits) != 0 || len(sub.replies) != 0 {
		t.Fatal("nothing should have been dispatched")
	}
}

func TestClassifyNotAddressedToUsNotConsumed(t *testing.T) {
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "STRANGER", "@@#ping", 0)
	if c.Classify(context.Background(), pkt) {
		t.Fatal("packet not addressed to us must not be consumed")
	}
}

func TestClassifyAddressedToTacticalConsumed(t *testing.T) {
	tac := messages.NewTacticalSet()
	tac.Store(map[string]struct{}{"BASE": {}})
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: false}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, nil, tac)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "BASE", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("tactical-addressed packet should be consumed")
	}
	if len(sub.submits) != 1 {
		t.Fatalf("expected 1 submit, got %d", len(sub.submits))
	}
}

func TestClassifyAddressedToListenerConsumed(t *testing.T) {
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: false}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, []string{"GWACT"}, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "GWACT", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("listener-addressed packet should be consumed")
	}
	if len(sub.submits) != 1 {
		t.Fatalf("expected submit, got %d submits / %d replies", len(sub.submits), len(sub.replies))
	}
}

func TestClassifyParseErrorRepliesUnknown(t *testing.T) {
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{}, &stubCredStore{}, nil, nil)
	// Has @@ prefix and is for us, but missing #.
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@bogus", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("parse-failed @@ message must still be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusUnknown {
		t.Fatalf("expected one StatusUnknown reply, got %+v", sub.replies)
	}
	if len(sub.submits) != 0 {
		t.Fatal("must not submit a parse-failed invocation")
	}
}

func TestClassifyUnknownActionRepliesUnknown(t *testing.T) {
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@#nope", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("unknown action must still be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusUnknown {
		t.Fatalf("expected StatusUnknown, got %+v", sub.replies)
	}
}

func TestClassifyAllowlistMissDenied(t *testing.T) {
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: false, SenderAllowlist: "W1AW,K1ABC-*"}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "STRANGER", "N0CALL", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("denied submission must still be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusDenied {
		t.Fatalf("expected StatusDenied, got %+v", sub.replies)
	}
	if len(sub.submits) != 0 {
		t.Fatal("denied invocations must not enter the queue")
	}
}

func TestClassifyAllowlistHitDispatched(t *testing.T) {
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: false, SenderAllowlist: "W1AW,K1ABC-*"}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "K1ABC-7", "N0CALL", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("allowed sender must be consumed")
	}
	if len(sub.submits) != 1 {
		t.Fatalf("expected 1 submit, got %d (replies=%d)", len(sub.submits), len(sub.replies))
	}
	if sub.submits[0].inv.SenderCall != "K1ABC-7" {
		t.Fatalf("sender call uppercased and preserved expected K1ABC-7, got %q", sub.submits[0].inv.SenderCall)
	}
}

func TestClassifyOTPRequiredButNoCredentialReplies(t *testing.T) {
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: true}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@123456#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("OTP-required action without credential must be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusNoCredential {
		t.Fatalf("expected StatusNoCredential, got %+v", sub.replies)
	}
}

func TestClassifyBadOTPReplies(t *testing.T) {
	credID := uint(5)
	cred := &configstore.OTPCredential{ID: credID, Name: "chris", SecretB32: "JBSWY3DPEHPK3PXP"}
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: true, OTPCredentialID: &credID}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub,
		&stubActionStore{byName: map[string]*configstore.Action{"ping": a}},
		&stubCredStore{byID: map[uint]*configstore.OTPCredential{credID: cred}},
		nil, nil,
	)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@000000#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("bad OTP must still be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusBadOTP {
		t.Fatalf("expected StatusBadOTP, got %+v", sub.replies)
	}
}

func TestClassifyValidOTPDispatched(t *testing.T) {
	credID := uint(5)
	secret := "JBSWY3DPEHPK3PXP"
	cred := &configstore.OTPCredential{ID: credID, Name: "chris", SecretB32: secret}
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: true, OTPCredentialID: &credID}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub,
		&stubActionStore{byName: map[string]*configstore.Action{"ping": a}},
		&stubCredStore{byID: map[uint]*configstore.OTPCredential{credID: cred}},
		nil, nil,
	)
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@"+code+"#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("valid OTP must be consumed")
	}
	if len(sub.submits) != 1 {
		t.Fatalf("expected 1 submit, got %d (replies=%d)", len(sub.submits), len(sub.replies))
	}
	if !sub.submits[0].inv.OTPVerified || sub.submits[0].inv.OTPCredName != "chris" {
		t.Fatalf("expected OTPVerified=true and cred name set, got %+v", sub.submits[0].inv)
	}
	if sub.submits[0].inv.OTPCredentialID != credID {
		t.Fatalf("expected OTPCredentialID=%d on dispatched invocation, got %d", credID, sub.submits[0].inv.OTPCredentialID)
	}
}

func TestClassifyBadArgRepliesWithKey(t *testing.T) {
	a := &configstore.Action{
		ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: false,
		ArgSchema: `[{"key":"room","regex":"^[a-z]+$"}]`,
	}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@#ping room=KITCHEN", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("bad arg must still be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusBadArg {
		t.Fatalf("expected StatusBadArg, got %+v", sub.replies)
	}
	if !strings.Contains(sub.replies[0].res.StatusDetail, "room") {
		t.Fatalf("expected detail to name 'room', got %q", sub.replies[0].res.StatusDetail)
	}
}

func TestClassifyArgsSanitizedBeforeSubmit(t *testing.T) {
	a := &configstore.Action{
		ID: 1, Name: "lights", Type: "command", Enabled: true, OTPRequired: false,
		ArgSchema: `[{"key":"room","regex":"^[a-z]+$"}]`,
	}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"lights": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@#lights room=kitchen", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("expected consumed")
	}
	if len(sub.submits) != 1 {
		t.Fatalf("expected submit, got replies=%+v", sub.replies)
	}
	got := sub.submits[0].inv.Args
	if len(got) != 1 || got[0].Key != "room" || got[0].Value != "kitchen" {
		t.Fatalf("unexpected sanitized args: %+v", got)
	}
}

func TestClassifyISDirectionPassesSourceIS(t *testing.T) {
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: false}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionIS, "OTHER", "N0CALL", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("expected consumed")
	}
	if len(sub.submits) != 1 || sub.submits[0].inv.Source != SourceIS {
		t.Fatalf("expected SourceIS, got %+v", sub.submits)
	}
}

func TestClassifyStoreNotFoundTreatedAsUnknown(t *testing.T) {
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{err: gorm.ErrRecordNotFound}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("not-found path still consumes the packet")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusUnknown {
		t.Fatalf("expected StatusUnknown on not-found, got %+v", sub.replies)
	}
}

func TestClassifyStoreFailureRepliesError(t *testing.T) {
	// A real DB failure (outage, corrupt page, etc.) must NOT come
	// back as "unknown action" — that misleads operators trying
	// legitimate OTP-authenticated requests during a partial outage.
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{err: errors.New("db down")}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("store-failure path still consumes the packet")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusError {
		t.Fatalf("expected StatusError on store failure, got %+v", sub.replies)
	}
	if sub.replies[0].res.StatusDetail != "store" {
		t.Fatalf("expected detail=store, got %q", sub.replies[0].res.StatusDetail)
	}
}

func TestClassifyCredentialStoreFailureRepliesError(t *testing.T) {
	credID := uint(5)
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: true, OTPCredentialID: &credID}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub,
		&stubActionStore{byName: map[string]*configstore.Action{"ping": a}},
		&stubCredStore{err: errors.New("db down")},
		nil, nil,
	)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@123456#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("credential-store failure path still consumes the packet")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusError {
		t.Fatalf("expected StatusError on credential store failure, got %+v", sub.replies)
	}
}

func TestClassifyMissingOTPRepliesBadOTPMissing(t *testing.T) {
	credID := uint(5)
	cred := &configstore.OTPCredential{ID: credID, Name: "chris", SecretB32: "JBSWY3DPEHPK3PXP"}
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: true, OTPCredentialID: &credID}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub,
		&stubActionStore{byName: map[string]*configstore.Action{"ping": a}},
		&stubCredStore{byID: map[uint]*configstore.OTPCredential{credID: cred}},
		nil, nil,
	)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("missing OTP must still be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusBadOTP {
		t.Fatalf("expected StatusBadOTP, got %+v", sub.replies)
	}
	if sub.replies[0].res.StatusDetail != "missing" {
		t.Fatalf("expected detail=missing, got %q", sub.replies[0].res.StatusDetail)
	}
}

func TestClassifyThirdPartyEnvelopeIsUnwrapped(t *testing.T) {
	// An action message gated through APRS101 ch 20 third-party
	// arrives with the gating iGate as outer Source and the original
	// author as ThirdParty.Source. The classifier must treat it as if
	// it had arrived directly: addressee match, sender = author.
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: false, SenderAllowlist: "K1ABC"}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, nil, nil)
	pkt := &aprs.DecodedAPRSPacket{
		Source:    "GATEWAY-1",
		Type:      aprs.PacketThirdParty,
		Direction: aprs.DirectionIS,
		ThirdParty: &aprs.DecodedAPRSPacket{
			Source: "K1ABC",
			Type:   aprs.PacketMessage,
			Message: &aprs.Message{
				Addressee: "N0CALL",
				Text:      "@@#ping",
			},
		},
	}
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("third-party-wrapped action must be consumed")
	}
	if len(sub.submits) != 1 {
		t.Fatalf("expected 1 submit, got %d (replies=%d)", len(sub.submits), len(sub.replies))
	}
	if sub.submits[0].inv.SenderCall != "K1ABC" {
		t.Fatalf("expected inner author K1ABC, got %q", sub.submits[0].inv.SenderCall)
	}
	if sub.submits[0].inv.Source != SourceIS {
		t.Fatalf("expected SourceIS, got %q", sub.submits[0].inv.Source)
	}
}

func TestClassifyNilVerifierRepliesError(t *testing.T) {
	credID := uint(5)
	cred := &configstore.OTPCredential{ID: credID, Name: "chris", SecretB32: "JBSWY3DPEHPK3PXP"}
	a := &configstore.Action{ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: true, OTPCredentialID: &credID}
	sub := &stubSubmitter{}
	cfg := ClassifierConfig{
		OurCall:     ourCallProvider("N0CALL"),
		TacticalSet: nil,
		Listeners:   NewAddresseeSet(),
		ActionStore: &stubActionStore{byName: map[string]*configstore.Action{"ping": a}},
		CredStore:   &stubCredStore{byID: map[uint]*configstore.OTPCredential{credID: cred}},
		OTPVerifier: nil,
		Runner:      sub,
	}
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@123456#ping", 0)
	if !NewClassifier(cfg).Classify(context.Background(), pkt) {
		t.Fatal("nil verifier path must still be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusError {
		t.Fatalf("expected StatusError, got %+v", sub.replies)
	}
	if sub.replies[0].res.StatusDetail != "no verifier" {
		t.Fatalf("expected detail='no verifier', got %q", sub.replies[0].res.StatusDetail)
	}
}

func TestClassifyFreeformDispatchesRawTail(t *testing.T) {
	a := &configstore.Action{
		ID: 7, Name: "sms", Type: "command", Enabled: true, OTPRequired: false,
		ArgMode:   "freeform",
		ArgSchema: `[{"key":"arg","regex":"^\\+[1-9][0-9]{6,14} .+$","max_len":120,"required":true}]`,
	}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"sms": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "KE0XYZ", "N0CALL", "@@#sms +15555551212 hello world", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("freeform action must be consumed")
	}
	if len(sub.submits) != 1 {
		t.Fatalf("expected 1 submit, got submits=%d replies=%+v", len(sub.submits), sub.replies)
	}
	got := sub.submits[0].inv.Args
	if len(got) != 1 || got[0].Key != FreeformArgKey || got[0].Value != "+15555551212 hello world" {
		t.Fatalf("freeform args wrong: %+v", got)
	}
}

func TestClassifyFreeformBadArgRepliesBadArg(t *testing.T) {
	a := &configstore.Action{
		ID: 7, Name: "sms", Type: "command", Enabled: true, OTPRequired: false,
		ArgMode:   "freeform",
		ArgSchema: `[{"key":"arg","regex":"^[A-Z]+$","max_len":50,"required":true}]`,
	}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"sms": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "KE0XYZ", "N0CALL", "@@#sms lowercase payload", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("freeform bad-arg must be consumed")
	}
	if len(sub.submits) != 0 {
		t.Fatalf("expected no submit, got %+v", sub.submits)
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusBadArg {
		t.Fatalf("expected StatusBadArg, got %+v", sub.replies)
	}
}

func TestClassifyKVActionWithFreeformPayloadRepliesBadArg(t *testing.T) {
	// kv-mode Action receiving a non-key=value payload must surface
	// StatusBadArg, not StatusUnknown — the action exists, the args
	// are wrong.
	a := &configstore.Action{
		ID: 1, Name: "ping", Type: "command", Enabled: true, OTPRequired: false,
		ArgSchema: `[{"key":"room","regex":"^[a-z]+$"}]`,
	}
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{byName: map[string]*configstore.Action{"ping": a}}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@#ping bareword", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("kv parse-fail with valid action name must be consumed")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusBadArg {
		t.Fatalf("expected StatusBadArg, got %+v", sub.replies)
	}
}

func TestSenderAllowed(t *testing.T) {
	cases := []struct {
		csv, sender string
		want        bool
	}{
		{"", "ANY", true},
		{"W1AW", "W1AW", true},
		{"W1AW", "W1AW-7", false},
		{"W1AW-*", "W1AW", true},
		{"W1AW-*", "W1AW-7", true},
		{"W1AW-*", "W1AWS", false},
		{"W1AW,K1ABC-*", "K1ABC-7", true},
		{" W1AW , K1ABC-* ", "W1AW", true},
		{"-*", "ANY", false},
	}
	for _, tc := range cases {
		got := senderAllowed(tc.sender, tc.csv)
		if got != tc.want {
			t.Errorf("senderAllowed(%q,%q)=%v want %v", tc.sender, tc.csv, got, tc.want)
		}
	}
}

// classifierTxSink captures Submit calls for the preflight integration
// tests. Mirrors the pkg/messages fakeTxSink, redefined here because Go
// scopes test fakes per-package.
type classifierTxSink struct {
	mu     sync.Mutex
	frames []classifierSubmit
}

type classifierSubmit struct {
	Channel uint32
	Frame   *ax25.Frame
	Src     txgovernor.SubmitSource
}

func (f *classifierTxSink) Submit(_ context.Context, ch uint32, frame *ax25.Frame, src txgovernor.SubmitSource) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.frames = append(f.frames, classifierSubmit{Channel: ch, Frame: frame, Src: src})
	return nil
}

func (f *classifierTxSink) list() []classifierSubmit {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]classifierSubmit, len(f.frames))
	copy(out, f.frames)
	return out
}

type classifierIGate struct {
	mu sync.Mutex
	ll []string
}

func (f *classifierIGate) SendLine(line string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ll = append(f.ll, line)
	return nil
}

// newClassifierTestPreflight builds a real messages.Preflight wired
// over package-local fakes so the classifier's auto-ACK path is
// exercised end to end.
func newClassifierTestPreflight(t *testing.T) (*messages.Preflight, *classifierTxSink, *classifierIGate) {
	t.Helper()
	sink := &classifierTxSink{}
	igs := &classifierIGate{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	pf, err := messages.NewPreflight(messages.PreflightConfig{
		OurCall:        func() string { return "N0CALL" },
		TxSink:         sink,
		IGateSender:    igs,
		Logger:         logger,
		AutoAckChannel: 1,
	})
	if err != nil {
		t.Fatalf("NewPreflight: %v", err)
	}
	return pf, sink, igs
}

// newClassifierWithPreflight wires a Classifier with a Preflight and
// the OTP-free "unlock" action. Returns the classifier and its
// stubSubmitter so tests can assert on Submit count.
func newClassifierWithPreflight(t *testing.T, pf *messages.Preflight) (*Classifier, *stubSubmitter) {
	t.Helper()
	a := &configstore.Action{
		ID: 1, Name: "unlock", Type: "command",
		Enabled: true, OTPRequired: false,
	}
	sub := &stubSubmitter{}
	c := NewClassifier(ClassifierConfig{
		OurCall:     ourCallProvider("N0CALL"),
		TacticalSet: messages.NewTacticalSet(),
		Listeners:   NewAddresseeSet(),
		ActionStore: &stubActionStore{byName: map[string]*configstore.Action{"unlock": a}},
		CredStore:   &stubCredStore{},
		OTPVerifier: NewOTPVerifier(OTPVerifierConfig{Now: time.Now}),
		Runner:      sub,
		Preflight:   pf,
	})
	return c, sub
}

func makeActionPacket(sender, addressee, body, msgID string, dir aprs.Direction) *aprs.DecodedAPRSPacket {
	return &aprs.DecodedAPRSPacket{
		Source:    sender,
		Type:      aprs.PacketMessage,
		Direction: dir,
		Channel:   1,
		Message: &aprs.Message{
			Addressee: addressee,
			Text:      body,
			MessageID: msgID,
		},
	}
}

func TestClassifierFiresAutoAckOnFirstCopy(t *testing.T) {
	pf, sink, _ := newClassifierTestPreflight(t)
	c, sub := newClassifierWithPreflight(t, pf)

	pkt := makeActionPacket("W1ABC", "N0CALL", "@@#unlock", "042", aprs.DirectionRF)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("classifier must consume @@-prefixed packet")
	}
	if got := len(sink.list()); got != 1 {
		t.Fatalf("auto-ACK count = %d, want 1", got)
	}
	if got := len(sub.submits); got != 1 {
		t.Fatalf("Submit call count = %d, want 1", got)
	}
}

func TestClassifierDedupSecondCopyNoSubmit(t *testing.T) {
	pf, sink, _ := newClassifierTestPreflight(t)
	c, sub := newClassifierWithPreflight(t, pf)

	pkt := makeActionPacket("W1ABC", "N0CALL", "@@#unlock", "042", aprs.DirectionRF)
	_ = c.Classify(context.Background(), pkt)

	dup := makeActionPacket("W1ABC", "N0CALL", "@@#unlock", "042", aprs.DirectionRF)
	if !c.Classify(context.Background(), dup) {
		t.Fatal("dup must still be consumed")
	}
	// APRS101 §14.2 — every copy is acked.
	if got := len(sink.list()); got != 2 {
		t.Fatalf("auto-ACK count after dup = %d, want 2", got)
	}
	if got := len(sub.submits); got != 1 {
		t.Fatalf("Submit count after dup = %d, want 1 (dedup must suppress)", got)
	}
}

func TestClassifierMissingMsgIDSkipsACKAndDedup(t *testing.T) {
	pf, sink, _ := newClassifierTestPreflight(t)
	c, sub := newClassifierWithPreflight(t, pf)

	pkt := makeActionPacket("W1ABC", "N0CALL", "@@#unlock", "", aprs.DirectionRF)
	_ = c.Classify(context.Background(), pkt)
	if got := len(sink.list()); got != 0 {
		t.Fatalf("no msgID must skip auto-ACK, got %d", got)
	}
	if got := len(sub.submits); got != 1 {
		t.Fatalf("no msgID must still Submit (no dedup key), got %d", got)
	}
}

// TestClassifierThreeIdenticalCopiesACKThriceFireOnce models the
// operational failure: an action sender retries while iGate fan-out
// delivers extra copies. The classifier must ACK every copy (so the
// sender stops retrying) but only fire the executor on the first copy.
func TestClassifierThreeIdenticalCopiesACKThriceFireOnce(t *testing.T) {
	pf, sink, _ := newClassifierTestPreflight(t)
	c, sub := newClassifierWithPreflight(t, pf)

	for i := 0; i < 3; i++ {
		pkt := makeActionPacket("W1ABC", "N0CALL", "@@#unlock", "042", aprs.DirectionRF)
		if !c.Classify(context.Background(), pkt) {
			t.Fatalf("copy %d not consumed", i)
		}
	}

	if got := len(sink.list()); got != 3 {
		t.Fatalf("auto-ACK count after 3 copies = %d, want 3", got)
	}
	if got := len(sub.submits); got != 1 {
		t.Fatalf("Submit count after 3 copies = %d, want 1", got)
	}
}
