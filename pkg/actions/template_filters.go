package actions

import (
	"bytes"
	"encoding/json"
	"html"
	"net/url"
	"regexp"
	"strings"
)

// tokenRE matches {{name}} and {{name|filter}} where name is one of:
//
//	arg              (freeform synthetic value)
//	arg.<key>        (kv-mode value by key; key matches [A-Za-z0-9._-])
//	action, sender-callsign, otp-verified, otp-cred, source
//
// and filter is one of: json, url, html (optional).
//
// Matching {{ }} eagerly is fine — substituted text is never re-scanned
// (single-pass ReplaceAllStringFunc), so a value containing literal
// "{{x}}" cannot trigger a second substitution.
var tokenRE = regexp.MustCompile(`\{\{([a-zA-Z][a-zA-Z0-9._-]*)(?:\|(json|url|html))?\}\}`)

// expandTokenFiltered substitutes tokens in `in` using a single regex
// scan. baseEncoder applies to bare tokens (no filter); filtered tokens
// override it. Used by webhook URL templates (urlEncoder default) and
// body templates (identityEncoder default).
func expandTokenFiltered(in string, inv Invocation, baseEncoder tokenEncoder) string {
	return tokenRE.ReplaceAllStringFunc(in, func(match string) string {
		m := tokenRE.FindStringSubmatch(match)
		if len(m) == 0 {
			return match
		}
		name := m[1]
		filter := m[2]
		raw, ok := lookupTokenValue(name, inv)
		if !ok {
			return match
		}
		if filter != "" {
			return applyFilter(raw, filter)
		}
		return baseEncoder(raw)
	})
}

func lookupTokenValue(name string, inv Invocation) (string, bool) {
	switch name {
	case "action":
		return inv.ActionName, true
	case "sender-callsign":
		return inv.SenderCall, true
	case "otp-verified":
		return boolStr(inv.OTPVerified), true
	case "otp-cred":
		return inv.OTPCredName, true
	case "source":
		return string(inv.Source), true
	case "arg":
		// Freeform synthetic value. Only present when ArgMode=freeform.
		for _, kv := range inv.Args {
			if kv.Key == FreeformArgKey {
				return kv.Value, true
			}
		}
		return "", false
	}
	const argDot = "arg."
	if strings.HasPrefix(name, argDot) {
		key := name[len(argDot):]
		for _, kv := range inv.Args {
			if kv.Key == key {
				return kv.Value, true
			}
		}
	}
	return "", false
}

// applyFilter encodes `raw` for the requested context. The encodings are
// chosen so an operator can place the token inside a literal: a JSON
// string literal, a URL query, or an HTML body.
func applyFilter(raw, filter string) string {
	switch filter {
	case "json":
		// Encode as a JSON string then strip the surrounding quotes so
		// the operator can place {{arg|json}} inside their own quotes
		// (e.g., `"msg":"{{arg|json}}"`).
		//
		// SetEscapeHTML(false) keeps `<`, `>`, `&` literal — the
		// surrounding context is JSON, not HTML. Operators rendering
		// the JSON into a browser should use {{arg|html}} on the
		// rendering side instead.
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		if err := enc.Encode(raw); err != nil {
			return ""
		}
		// Encoder appends a trailing newline; strip it plus the quotes.
		out := bytes.TrimRight(buf.Bytes(), "\n")
		if len(out) >= 2 {
			return string(out[1 : len(out)-1])
		}
		return ""
	case "url":
		return url.QueryEscape(raw)
	case "html":
		return html.EscapeString(raw)
	}
	return raw
}
