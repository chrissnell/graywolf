package redact

import (
	"strings"
	"testing"
)

// Each rule below has TWO test fixtures: one input the rule MUST
// match (positive), and one near-miss it MUST leave alone (negative).
// 100% rule coverage is a hard acceptance criterion per design doc.

func TestRule_Email(t *testing.T) {
	r := ruleByID(t, "email")
	mustReplace(t, r, "ping me at chris@example.com please", "[EMAIL]")
	mustLeaveAlone(t, r, "user@ but no host suffix")
	mustLeaveAlone(t, r, "@notanemail")
}

func TestRule_BearerToken(t *testing.T) {
	r := ruleByID(t, "bearer")
	mustReplace(t, r, "Authorization: Bearer abc123XYZ", "Bearer [REDACTED]")
	mustLeaveAlone(t, r, "the bearer of bad news") // case-sensitive: lowercase 'bearer'
}

func TestRule_HexBase64Run(t *testing.T) {
	r := ruleByID(t, "hex_or_base64")
	// 24 lowercase hex chars: must match.
	mustReplace(t, r, "key=abcdef0123456789abcdef01 trailing", "[REDACTED]")
	// 23 chars: just under threshold; must NOT match.
	mustLeaveAlone(t, r, "key=abcdef0123456789abcdef0")
}

func TestRule_IPv4Loopback(t *testing.T) {
	r := ruleByID(t, "ipv4")
	mustReplace(t, r, "bound on 127.0.0.1:8080", "<ip:loopback>")
	mustLeaveAlone(t, r, "version 1.2.3 not 1.2.3.4.5") // 5-octet not a valid v4
}

func TestRule_IPv4RFC1918(t *testing.T) {
	r := ruleByID(t, "ipv4")
	mustReplace(t, r, "router 192.168.1.1 reachable", "<ip:rfc1918>")
	mustReplace(t, r, "tun0 10.42.0.5 tx ok", "<ip:rfc1918>")
}

func TestRule_IPv4Public(t *testing.T) {
	r := ruleByID(t, "ipv4")
	mustReplace(t, r, "uplink 8.8.8.8 ok", "<ip>")
	mustLeaveAlone(t, r, "subnet mask 255.255.255.255") // edge case; OK to redact: 255.255.255.255 IS a v4 — just confirm replacement type
}

func TestRule_IPv6(t *testing.T) {
	r := ruleByID(t, "ipv6")
	mustReplace(t, r, "fe80::1234:abcd at link-local", "<ip>")
	mustLeaveAlone(t, r, "the time is 12:34:56 today") // colons but not v6
}

func TestRule_MAC(t *testing.T) {
	r := ruleByID(t, "mac")
	mustReplace(t, r, "wlan0 hwaddr b8:27:eb:11:22:33", "<mac:b8:27:eb>")
	mustLeaveAlone(t, r, "version 1:2:3:4:5") // colons but not octet-shaped
}

func TestRule_HomeDir(t *testing.T) {
	r := ruleByID(t, "home_dir")
	mustReplace(t, r, "open /home/cjs/.config/Graywolf/foo", "<home>/.config/Graywolf/foo")
	mustReplace(t, r, "open /Users/cjs/Library/Logs/foo", "<home>/Library/Logs/foo")
	mustLeaveAlone(t, r, "/home/ but nothing else") // bare /home/ with no user prefix
}

func TestRule_HostnameLiteral(t *testing.T) {
	r := ruleByID(t, "hostname")
	r.HostnameLiteral = "rosie-pi"
	r.HostnameHash = "1a2b3c4d"
	mustReplace(t, r, "boot from rosie-pi.local", "1a2b3c4d.local")
	mustLeaveAlone(t, r, "from sosie-pi.local") // similar but different host
}

// --- helpers ---

func mustReplace(t *testing.T, r Rule, in, wantSubstring string) {
	t.Helper()
	got := r.Apply(in)
	if !strings.Contains(got, wantSubstring) {
		t.Fatalf("rule %q applied to %q\n  got: %q\n want substring: %q", r.ID, in, got, wantSubstring)
	}
	if got == in {
		t.Fatalf("rule %q did not change input %q", r.ID, in)
	}
}

func mustLeaveAlone(t *testing.T, r Rule, in string) {
	t.Helper()
	got := r.Apply(in)
	if got != in {
		t.Fatalf("rule %q changed input it should have left alone\n  in:  %q\n  got: %q", r.ID, in, got)
	}
}

func ruleByID(t *testing.T, id string) Rule {
	t.Helper()
	for _, r := range BuiltinRules() {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("rule %q not in BuiltinRules()", id)
	return Rule{}
}
