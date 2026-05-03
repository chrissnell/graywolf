package actions

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
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

func TestClassifyStoreErrorTreatedAsUnknown(t *testing.T) {
	sub := &stubSubmitter{}
	c := newClassifierForTest(t, sub, &stubActionStore{err: errors.New("db")}, &stubCredStore{}, nil, nil)
	pkt := makeMessagePkt(aprs.DirectionRF, "OTHER", "N0CALL", "@@#ping", 0)
	if !c.Classify(context.Background(), pkt) {
		t.Fatal("store error path still consumes the packet")
	}
	if len(sub.replies) != 1 || sub.replies[0].res.Status != StatusUnknown {
		t.Fatalf("expected StatusUnknown on store error, got %+v", sub.replies)
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
