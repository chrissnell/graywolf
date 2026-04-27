package redact

import (
	"strings"
	"testing"
)

func TestEngine_AppliesAllBuiltinRules(t *testing.T) {
	e := NewEngine()
	e.SetHostname("rosie-pi")

	in := strings.Join([]string{
		"contact chris@example.com about",
		"Authorization: Bearer abc.def.ghijkl",
		"random=abcdef0123456789abcdef01",
		"127.0.0.1, 10.0.0.42, 8.8.8.8 — fe80::1",
		"hwaddr b8:27:eb:11:22:33",
		"open /home/cjs/.config/Graywolf/x",
		"boot from rosie-pi.local",
	}, "\n")

	got := e.Apply(in)

	for _, want := range []string{
		"[EMAIL]",
		"Bearer [REDACTED]",
		"[REDACTED]",
		"<ip:loopback>",
		"<ip:rfc1918>",
		"<ip>",
		"<mac:b8:27:eb>",
		"<home>/.config/Graywolf/x",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in scrubbed output:\n%s", want, got)
		}
	}

	// hostname rule: literal "rosie-pi" must not survive.
	if strings.Contains(got, "rosie-pi") {
		t.Fatalf("literal hostname survived scrub:\n%s", got)
	}
}

func TestEngine_PreservesAPRSCallsigns(t *testing.T) {
	e := NewEngine()
	e.SetHostname("rosie-pi")

	cases := []string{
		"N0CALL transmitting at 144.390",
		"WB7ABC-9 received from KK7XYZ-7",
		"VE3ABC-15 to APRS via KK7XYZ-2",
	}
	for _, c := range cases {
		got := e.Apply(c)
		// The leading callsign(s) must survive verbatim. Pull the
		// first whitespace-delimited token from the input and assert
		// it appears literally in the output.
		first := strings.Fields(c)[0]
		if !strings.Contains(got, first) {
			t.Fatalf("APRS callsign %q lost during scrub of %q\nresult: %q", first, c, got)
		}
	}
}

func TestEngine_AdHocRuleStacksOnTop(t *testing.T) {
	e := NewEngine()
	if err := e.AddRegex("api-XYZ-[0-9]+", "[OPERATOR_REDACTED]"); err != nil {
		t.Fatalf("AddRegex: %v", err)
	}
	got := e.Apply("call site api-XYZ-1234 done")
	if !strings.Contains(got, "[OPERATOR_REDACTED]") {
		t.Fatalf("ad-hoc rule didn't fire:\n%s", got)
	}
}

func TestEngine_AdHocRuleRejectsBadRegex(t *testing.T) {
	e := NewEngine()
	if err := e.AddRegex("[unterminated", ""); err == nil {
		t.Fatal("AddRegex accepted invalid regex")
	}
}

func TestEngine_RuleOrderingDoesNotEatHostnameHash(t *testing.T) {
	// Regression guard: if the hex_or_base64 rule ran AFTER the
	// hostname rule, an unlucky 24-char concatenation could swallow
	// the hash. Hostname runs last by design — confirm.
	e := NewEngine()
	e.SetHostname("zzz") // → 24 chars of hex would not contain this
	got := e.Apply("hostname zzz embedded")
	if strings.Contains(got, "zzz") {
		t.Fatalf("hostname survived: %q", got)
	}
}

func TestEngine_SetHostnameEmptyIsNoop(t *testing.T) {
	e := NewEngine()
	in := "no hostname configured"
	if got := e.Apply(in); got != in {
		t.Fatalf("empty hostname mutated input: %q", got)
	}
}

// Defensive guard: ensure NewEngine is independent across calls.
func TestEngine_AdHocRulesAreInstanceScoped(t *testing.T) {
	a := NewEngine()
	if err := a.AddRegex("api-X", "X"); err != nil {
		t.Fatal(err)
	}
	b := NewEngine()
	got := b.Apply("api-X stays")
	if !strings.Contains(got, "api-X") {
		t.Fatalf("engine a's rules leaked to b: %q", got)
	}
}
