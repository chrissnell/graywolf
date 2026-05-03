package actions

import (
	"context"
	"encoding/json"
	"errors"
	"strings"

	"github.com/chrissnell/graywolf/pkg/aprs"
	"github.com/chrissnell/graywolf/pkg/configstore"
	"github.com/chrissnell/graywolf/pkg/messages"
)

// ActionPrefix is the on-air sentinel that diverts an inbound message
// from the inbox to the Actions runner.
const ActionPrefix = "@@"

// Submitter is the runner-facing interface used by the classifier.
// Submit enqueues a normal invocation; Reply short-circuits with a
// synthetic result for outcomes the runner never sees (denied,
// bad_arg, bad_otp, no_credential).
type Submitter interface {
	Submit(ctx context.Context, inv Invocation, a *configstore.Action, channel uint32)
	Reply(ctx context.Context, inv Invocation, channel uint32, res Result)
}

// ActionLookup loads an Action by name. Wraps the configstore for
// testability.
type ActionLookup interface {
	GetActionByName(ctx context.Context, name string) (*configstore.Action, error)
}

// CredentialLookup loads an OTP credential by ID.
type CredentialLookup interface {
	GetOTPCredential(ctx context.Context, id uint) (*configstore.OTPCredential, error)
}

// ClassifierConfig wires the classifier to the rest of the subsystem.
type ClassifierConfig struct {
	OurCall     func() string
	TacticalSet *messages.TacticalSet
	Listeners   *AddresseeSet
	ActionStore ActionLookup
	CredStore   CredentialLookup
	OTPVerifier *OTPVerifier
	Runner      Submitter
}

// Classifier inspects inbound APRS messages and decides whether they
// are Actions traffic. Lives in the rxfanout hot path; never blocks
// on I/O directly — store + verifier calls are bounded synchronous
// reads, and the runner submission is non-blocking.
type Classifier struct{ cfg ClassifierConfig }

func NewClassifier(cfg ClassifierConfig) *Classifier { return &Classifier{cfg: cfg} }

// Classify inspects pkt. Returns true when the packet was consumed
// by the Actions subsystem and the messages router must skip it.
//
// Consumption rule: the packet is an APRS message addressed to the
// trigger surface (station call / tactical alias / listener
// addressee) AND its body begins with ActionPrefix. Parse failures
// on a consumed packet still return true — they emit an "unknown"
// audit row + reply rather than leaking partial Actions noise into
// the inbox.
//
// Third-party APRS101 ch 20 envelopes are unwrapped before
// classification so an action gated from IS→RF (or vice-versa) is
// claimed by Classify, not by the messages router.
func (c *Classifier) Classify(ctx context.Context, pkt *aprs.DecodedAPRSPacket) bool {
	if pkt == nil {
		return false
	}
	innerSource, innerMsg := effectiveMessage(pkt)
	if innerMsg == nil {
		return false
	}
	if !strings.HasPrefix(innerMsg.Text, ActionPrefix) {
		return false
	}
	addr := innerMsg.Addressee
	match := messages.MatchAddressee(c.cfg.OurCall(), addr, c.cfg.TacticalSet)
	if !match.IsForUs && !(c.cfg.Listeners != nil && c.cfg.Listeners.Contains(addr)) {
		return false
	}

	source := SourceRF
	if pkt.Direction == aprs.DirectionIS {
		source = SourceIS
	}
	sender := strings.ToUpper(strings.TrimSpace(innerSource))
	channel := uint32(pkt.Channel)

	parsed, err := Parse(innerMsg.Text)
	if err != nil {
		c.cfg.Runner.Reply(ctx, Invocation{
			SenderCall: sender, Source: source,
		}, channel, Result{Status: StatusUnknown})
		return true
	}

	a, err := c.cfg.ActionStore.GetActionByName(ctx, parsed.Action)
	if err != nil || a == nil {
		c.cfg.Runner.Reply(ctx, Invocation{
			ActionName: parsed.Action, SenderCall: sender, Source: source,
		}, channel, Result{Status: StatusUnknown})
		return true
	}

	inv := Invocation{
		ActionID:   a.ID,
		ActionName: a.Name,
		SenderCall: sender,
		Source:     source,
	}

	// Sender allowlist. Runs before OTP so a denied sender never
	// gets to learn whether their guess was structurally valid.
	if !senderAllowed(sender, a.SenderAllowlist) {
		c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusDenied})
		return true
	}

	// OTP verification.
	if a.OTPRequired {
		if a.OTPCredentialID == nil {
			c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusNoCredential})
			return true
		}
		cred, cerr := c.cfg.CredStore.GetOTPCredential(ctx, *a.OTPCredentialID)
		if cerr != nil || cred == nil {
			c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusNoCredential})
			return true
		}
		if c.cfg.OTPVerifier == nil {
			// Wiring bug, not a missing operator credential.
			c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusError, StatusDetail: "no verifier"})
			return true
		}
		if parsed.OTPDigits == "" {
			c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusBadOTP, StatusDetail: "missing"})
			return true
		}
		ok, verr := c.cfg.OTPVerifier.Verify(cred.ID, cred.SecretB32, parsed.OTPDigits)
		if verr != nil {
			detail := "verify"
			if errors.Is(verr, ErrOTPReplay) {
				detail = "replay"
			}
			c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusBadOTP, StatusDetail: detail})
			return true
		}
		if !ok {
			c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusBadOTP})
			return true
		}
		inv.OTPVerified = true
		inv.OTPCredName = cred.Name
	}

	// Sanitize args against the action's schema.
	schema, schemaErr := decodeArgSchema(a.ArgSchema)
	if schemaErr != nil {
		c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusError, StatusDetail: "schema"})
		return true
	}
	clean, sErr := Sanitize(schema, parsed.Args)
	if sErr != nil {
		var bae *BadArgError
		detail := "bad arg"
		if errors.As(sErr, &bae) {
			detail = bae.Key
		}
		c.cfg.Runner.Reply(ctx, inv, channel, Result{Status: StatusBadArg, StatusDetail: detail})
		return true
	}
	inv.Args = clean

	c.cfg.Runner.Submit(ctx, inv, a, channel)
	return true
}

// effectiveMessage returns the (source, message) pair to classify
// against, unwrapping a single layer of APRS101 ch 20 third-party
// envelope. Returns (_, nil) when pkt is not a message and is not
// a third-party-wrapped message.
//
// For third-party traffic the inner source is the original author
// (the relaying iGate's call appears on pkt.Source instead). The
// allowlist + audit rows must use the author, otherwise the
// allowlist becomes "must be heard from this iGate".
func effectiveMessage(pkt *aprs.DecodedAPRSPacket) (string, *aprs.Message) {
	if pkt.Type == aprs.PacketThirdParty && pkt.ThirdParty != nil &&
		pkt.ThirdParty.Type == aprs.PacketMessage && pkt.ThirdParty.Message != nil {
		return pkt.ThirdParty.Source, pkt.ThirdParty.Message
	}
	if pkt.Type == aprs.PacketMessage && pkt.Message != nil {
		return pkt.Source, pkt.Message
	}
	return "", nil
}

// senderAllowed reports whether sender (already uppercased) is
// permitted by the CSV allowlist. An empty allowlist allows everyone.
// Tokens may be exact matches or "BASE-*" wildcards that match BASE
// or any BASE-N SSID.
func senderAllowed(sender, csv string) bool {
	csv = strings.TrimSpace(csv)
	if csv == "" {
		return true
	}
	for _, pat := range strings.Split(csv, ",") {
		pat = strings.ToUpper(strings.TrimSpace(pat))
		if pat == "" {
			continue
		}
		if strings.HasSuffix(pat, "-*") {
			base := strings.TrimSuffix(pat, "-*")
			if base == "" {
				continue
			}
			if sender == base || strings.HasPrefix(sender, base+"-") {
				return true
			}
		} else if pat == sender {
			return true
		}
	}
	return false
}

func decodeArgSchema(s string) ([]ArgSpec, error) {
	if s == "" || s == "[]" {
		return nil, nil
	}
	var out []ArgSpec
	if err := json.Unmarshal([]byte(s), &out); err != nil {
		return nil, err
	}
	return out, nil
}
