package redact

import (
	"net"
	"regexp"
	"strings"
)

// Rule is one redaction unit. Apply is the only thing the Engine
// calls; the Pattern and hostname fields are exposed for test
// introspection. ID is a snake_case tag the operator UI may display
// alongside redacted spans for "redacted by which rule" auditability.
type Rule struct {
	ID      string
	Pattern *regexp.Regexp
	// applyFn receives the Rule value so Apply reads the caller's
	// fields (including any mutations to HostnameLiteral/HostnameHash)
	// even after the Rule has been copied by value.
	applyFn func(r Rule, s string) string

	// HostnameLiteral / HostnameHash are populated only on the
	// "hostname" rule and are mutated by Engine.SetHostname so the
	// scrub stays consistent within one submission.
	HostnameLiteral string
	HostnameHash    string
}

// Apply runs the rule's scrub on s and returns the result.
func (r Rule) Apply(s string) string {
	if r.applyFn == nil {
		return s
	}
	return r.applyFn(r, s)
}

// BuiltinRules returns the canonical scrub rule set. Order is the
// order rules are applied; later rules see earlier rules' output.
//
// Ordering rationale:
//   - email + bearer token first: they look like literal strings and
//     don't overlap any later category
//   - hex/base64 next: would otherwise gobble parts of IPv6 / MAC
//     addresses
//   - IPv4 + IPv6 + MAC: structured numerics; IPv6 must come before
//     MAC because IPv6 ::-collapsed addresses can look MAC-shaped at
//     character level
//   - home dir: filesystem paths
//   - hostname literal: must be last because it depends on per-run
//     SetHostname state
func BuiltinRules() []Rule {
	rules := []Rule{
		{
			ID:      "email",
			Pattern: regexp.MustCompile(`\b[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}\b`),
		},
		{
			ID:      "bearer",
			Pattern: regexp.MustCompile(`Bearer\s+[A-Za-z0-9._\-=+/]+`),
		},
		{
			// = excluded from charset so "key=<23chars>" is not incorrectly
			// inflated to 27 chars by including the "key=" prefix.
			ID:      "hex_or_base64",
			Pattern: regexp.MustCompile(`\b[A-Za-z0-9+/]{24,}\b`),
		},
		{
			// (?:\.\d+)? consumes a 5th octet so net.ParseIP rejects it
			// as nil and the match is left unchanged.
			ID:      "ipv4",
			Pattern: regexp.MustCompile(`\b(?:\d{1,3}\.){3}\d{1,3}(?:\.\d+)?\b`),
		},
		{
			// Matches compressed (::) IPv6 forms like fe80::1234:abcd.
			// Does not match bare HH:MM:SS time strings (no ::).
			ID:      "ipv6",
			Pattern: regexp.MustCompile(`\b[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4})*::(?:[0-9a-fA-F]{1,4}(?::[0-9a-fA-F]{1,4})*)?\b`),
		},
		{
			ID:      "mac",
			Pattern: regexp.MustCompile(`\b[0-9a-fA-F]{2}(?::[0-9a-fA-F]{2}){5}\b`),
		},
		{
			ID:      "home_dir",
			Pattern: regexp.MustCompile(`(?:/home/[^/\s]+|/Users/[^/\s]+|C:\\Users\\[^\\\s]+)`),
		},
		{
			ID: "hostname",
			// Pattern set per-run by Engine.SetHostname; left nil here.
			Pattern: nil,
		},
	}

	rules[0].applyFn = func(r Rule, s string) string { return r.Pattern.ReplaceAllString(s, "[EMAIL]") }
	rules[1].applyFn = func(r Rule, s string) string { return r.Pattern.ReplaceAllString(s, "Bearer [REDACTED]") }
	rules[2].applyFn = func(r Rule, s string) string { return r.Pattern.ReplaceAllString(s, "[REDACTED]") }
	rules[3].applyFn = func(r Rule, s string) string { return applyIPv4(r.Pattern, s) }
	rules[4].applyFn = func(r Rule, s string) string { return r.Pattern.ReplaceAllString(s, "<ip>") }
	rules[5].applyFn = func(r Rule, s string) string { return applyMAC(r.Pattern, s) }
	rules[6].applyFn = func(r Rule, s string) string { return r.Pattern.ReplaceAllString(s, "<home>") }
	rules[7].applyFn = func(r Rule, s string) string {
		if r.HostnameLiteral == "" || r.HostnameHash == "" {
			return s
		}
		return strings.ReplaceAll(s, r.HostnameLiteral, r.HostnameHash)
	}

	return rules
}

// applyIPv4 replaces every IPv4 in s with <ip:loopback>, <ip:rfc1918>,
// or <ip>, preserving the privacy-relevant locality marker.
// Broadcast / non-unicast addresses (e.g. 255.255.255.255) are left
// unchanged — they are not endpoint identifiers.
func applyIPv4(re *regexp.Regexp, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		ip := net.ParseIP(m)
		if ip == nil || ip.To4() == nil {
			return m
		}
		if ip.IsLoopback() {
			return "<ip:loopback>"
		}
		if ip.IsPrivate() {
			return "<ip:rfc1918>"
		}
		if ip.IsGlobalUnicast() {
			return "<ip>"
		}
		return m
	})
}

// applyMAC replaces every MAC with <mac:OUI> where OUI is the first
// three octets — the hardware-vendor prefix the operator UI keeps
// for triage ("yep, that's a Pi onboard NIC").
func applyMAC(re *regexp.Regexp, s string) string {
	return re.ReplaceAllStringFunc(s, func(m string) string {
		// First 8 chars = "xx:xx:xx".
		if len(m) < 8 {
			return m
		}
		return "<mac:" + strings.ToLower(m[:8]) + ">"
	})
}
