package actions

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

// MaxReplyLen is the per-message APRS text cap (one frame).
const MaxReplyLen = 67

// MaxReplyLinesCeiling is the hard upper bound enforced by the
// validator on Action.MaxReplyLines. Operators choose 1..ceiling; the
// runtime clamps anything outside that range. The cap exists because
// each extra line is one extra RF frame plus its own ack/retry budget;
// >5 turns a single trigger into an airtime storm.
const MaxReplyLinesCeiling = 5

// DefaultMaxReplyLines is the value used when an Action row pre-dates
// the column or the operator left it at zero.
const DefaultMaxReplyLines = 1

// ReplySender dispatches one reply back to the originator over the
// matching transport. The RF/IS routing is the implementation's
// concern; the runner just hands over the addressee + text. One call
// per line — the runner loops when MaxReplyLines > 1.
type ReplySender interface {
	SendReply(ctx context.Context, channel uint32, source Source, toCall, text string) error
}

func statusWord(s Status) string {
	switch s {
	case StatusOK:
		return "ok"
	case StatusBadOTP:
		return "bad otp"
	case StatusBadArg:
		return "bad arg"
	case StatusDenied:
		return "denied"
	case StatusDisabled:
		return "disabled"
	case StatusUnknown:
		return "unknown"
	case StatusNoCredential:
		return "no-credential"
	case StatusBusy:
		return "busy"
	case StatusRateLimited:
		return "rate-limited"
	case StatusTimeout:
		return "timeout"
	default:
		return "error"
	}
}

// FormatReplies produces up to maxLines on-air reply strings from a
// Result. Line 1 carries the status-word prefix ("ok: ..."); lines 2..N
// are plain output lines (no prefix). Each line is sanitized of control
// characters and capped at MaxReplyLen runes. Non-OK statuses always
// collapse to a single line — there is no point fanning a bad-arg or
// timeout failure across multiple frames.
//
// maxLines <= 0 is treated as 1. Blank lines in the captured output
// are dropped. Returns the produced lines and a bool that is true when
// any data was dropped: line count exceeded maxLines, an individual
// line was clamped to MaxReplyLen, or the assembled status+detail was
// truncated.
func FormatReplies(r Result, maxLines int) ([]string, bool) {
	if maxLines <= 0 {
		maxLines = 1
	}

	// Non-OK paths surface a single-line reply with the status-word
	// detail. Multi-line is meaningful only for executor success
	// output; failures should be tight.
	if r.Status != StatusOK {
		return []string{formatStatusLine(r)}, false
	}

	if maxLines == 1 {
		rawLines := splitNonEmptyLines(r.OutputCapture)
		if len(rawLines) == 0 {
			return []string{statusWord(StatusOK)}, false
		}
		line, tr := formatOKSingle(rawLines[0])
		if len(rawLines) > 1 {
			tr = true
		}
		return []string{line}, tr
	}

	rawLines := splitNonEmptyLines(r.OutputCapture)
	if len(rawLines) == 0 {
		return []string{statusWord(StatusOK)}, false
	}

	out := make([]string, 0, maxLines)
	truncated := false

	// Line 1 wears the status-word prefix.
	first, tr := assembleOKLine(rawLines[0])
	out = append(out, first)
	if tr {
		truncated = true
	}

	for i := 1; i < len(rawLines) && len(out) < maxLines; i++ {
		line, lt := clampLine(sanitizeReplyText(rawLines[i]))
		if line == "" {
			continue
		}
		out = append(out, line)
		if lt {
			truncated = true
		}
	}

	if len(rawLines) > maxLines {
		truncated = true
	}
	return out, truncated
}

// FormatReply preserves the legacy single-line API. New code should
// call FormatReplies; this wrapper is kept so call sites that have not
// been updated keep working (the two production callers — runner +
// service test-fire audit — are switched to FormatReplies in this
// change; this wrapper exists for anything else, e.g. webapi
// test-fire which still reports a single primary line in addition to
// the full slice).
func FormatReply(r Result) (string, bool) {
	lines, tr := FormatReplies(r, 1)
	if len(lines) == 0 {
		return "", tr
	}
	return lines[0], tr
}

// formatStatusLine builds the legacy one-line status reply for any
// non-OK Result. Equivalent to the pre-multi-line FormatReply body.
func formatStatusLine(r Result) string {
	word := statusWord(r.Status)
	var detail string
	switch r.Status {
	case StatusBadArg, StatusError, StatusTimeout:
		detail = sanitizeReplyText(r.StatusDetail)
	}
	if detail == "" {
		return word
	}
	full := word + ": " + detail
	if utf8.RuneCountInString(full) <= MaxReplyLen {
		return full
	}
	return truncateReply(full)
}

// formatOKSingle is the single-line OK path — same shape as the legacy
// FormatReply for callers that opt out of multi-line. Uses the
// 50-rune snippet so the legacy "compact reply" budget is preserved.
func formatOKSingle(output string) (string, bool) {
	detail, tr := firstLineSnippet(output)
	if detail == "" {
		return statusWord(StatusOK), tr
	}
	full := "ok: " + detail
	if utf8.RuneCountInString(full) <= MaxReplyLen {
		return full, tr
	}
	return truncateReply(full), true
}

// assembleOKLine wraps a stdout line as the first line of a multi-line
// OK reply, applying the "ok: " prefix and the per-line cap. Reports
// truncation when either the prefix overflowed or the input itself was
// clamped.
func assembleOKLine(s string) (string, bool) {
	clean, tr := clampLine(sanitizeReplyText(s))
	if clean == "" {
		return statusWord(StatusOK), tr
	}
	full := "ok: " + clean
	if utf8.RuneCountInString(full) <= MaxReplyLen {
		return full, tr
	}
	return truncateReply(full), true
}

// clampLine sanitizes and length-caps a single output line. Empty
// input returns "" with no truncation.
func clampLine(s string) (string, bool) {
	if utf8.RuneCountInString(s) > MaxReplyLen {
		runes := []rune(s)
		return string(runes[:MaxReplyLen-1]) + "…", true
	}
	return s, false
}

func firstLineSnippet(s string) (string, bool) {
	s = sanitizeReplyText(s)
	if utf8.RuneCountInString(s) > 50 {
		runes := []rune(s)
		return string(runes[:50]) + "…", true
	}
	return s, false
}

// splitNonEmptyLines splits on "\n" or "\r\n" and drops blank lines
// after sanitizing whitespace. Order is preserved.
func splitNonEmptyLines(s string) []string {
	if s == "" {
		return nil
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	parts := strings.Split(s, "\n")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		clean := strings.TrimSpace(stripControlChars(p))
		if clean == "" {
			continue
		}
		out = append(out, clean)
	}
	return out
}

// stripControlChars removes ASCII C0 + DEL but leaves printable bytes
// (and multi-byte UTF-8) intact. Used by both single-line snippet and
// multi-line splitter.
func stripControlChars(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if (r < 0x20 && r != '\n' && r != '\r' && r != '\t') || r == 0x7F {
			continue
		}
		if r == '\t' {
			b.WriteByte(' ')
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// sanitizeReplyText collapses CR/LF to space and strips control chars
// for single-line use. Multi-line callers split first, then call this
// per line via clampLine.
func sanitizeReplyText(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = stripControlChars(s)
	return strings.TrimSpace(s)
}

func truncateReply(s string) string {
	limit := MaxReplyLen - 1
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return fmt.Sprintf("%s…", string(runes[:limit]))
}
