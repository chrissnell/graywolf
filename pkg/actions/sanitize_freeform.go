package actions

import (
	"errors"
	"fmt"
)

// SanitizeFreeform validates a single untokenized payload against an
// Action's freeform arg schema. The schema must contain exactly one
// ArgSpec; the synthetic FreeformArgKey is imposed on the result so
// downstream executors can rely on a stable key.
//
// Defense layers, in order:
//
//  1. Schema shape — exactly one ArgSpec.
//  2. Required check — empty value rejected when Required.
//  3. Length — operator MaxLen, hard-capped by ceiling.
//  4. Control-char floor — bytes outside printable ASCII + extended
//     UTF-8 are rejected unconditionally. Specifically: any byte in
//     0x00..0x1F or 0x7F. Catches tabs, NUL, CR/LF, escape codes and
//     other terminal/log-injection vectors regardless of operator
//     regex permissiveness.
//  5. Regex — operator pattern, defaulted to `.*` if empty so a blank
//     pattern doesn't fall through to the kv default that requires the
//     value to look like an identifier.
//
// Order matters: the control-char floor runs BEFORE regex so a
// permissive `.*` cannot widen the floor.
func SanitizeFreeform(schema []ArgSpec, raw string, ceiling int) ([]KeyValue, error) {
	if len(schema) != 1 {
		return nil, errors.New("freeform schema must have exactly one ArgSpec")
	}
	spec := schema[0]

	if raw == "" {
		if spec.Required {
			return nil, &BadArgError{Key: FreeformArgKey, Reason: "missing"}
		}
		return nil, nil
	}

	maxLen := spec.MaxLen
	if maxLen <= 0 || maxLen > ceiling {
		maxLen = ceiling
	}
	if len(raw) > maxLen {
		return nil, &BadArgError{Key: FreeformArgKey, Reason: "too long"}
	}

	if i, ok := firstControlByte(raw); ok {
		return nil, &BadArgError{Key: FreeformArgKey, Reason: fmt.Sprintf("control char at byte %d", i)}
	}

	pat := spec.Regex
	if pat == "" {
		// Freeform's default is permissive — the operator opted into a
		// single payload, the kv-style identifier regex would reject
		// almost any realistic message body. Control-char floor above
		// already filters the dangerous bytes.
		pat = `.*`
	}
	re, err := compileRegex(pat)
	if err != nil {
		return nil, &BadArgError{Key: FreeformArgKey, Reason: "schema regex invalid"}
	}
	if !re.MatchString(raw) {
		return nil, &BadArgError{Key: FreeformArgKey, Reason: "regex"}
	}

	return []KeyValue{{Key: FreeformArgKey, Value: raw}}, nil
}

// firstControlByte returns the byte offset of the first ASCII control
// character (0x00..0x1F or 0x7F) in s. Multi-byte UTF-8 sequences are
// allowed through — those start at >= 0x80 and never collide with the
// rejection range. Tabs are rejected even though they are technically
// "whitespace": APRS bodies have no legitimate use for tab and tabs
// in shell-script argv smuggle word boundaries.
func firstControlByte(s string) (int, bool) {
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c < 0x20 || c == 0x7F {
			return i, true
		}
	}
	return 0, false
}
