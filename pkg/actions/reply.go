package actions

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const MaxReplyLen = 67 // APRS message text limit

// ReplySender dispatches one reply back to the originator over the
// matching transport. The RF/IS routing is the implementation's
// concern; the runner just hands over the addressee + text.
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

// FormatReply produces the on-air reply text and reports whether any
// truncation occurred (either the snippet was shortened to fit the
// 50-char per-line cap, or the assembled reply exceeded MaxReplyLen).
// The bool is what gets stored in ActionInvocation.Truncated.
func FormatReply(r Result) (string, bool) {
	word := statusWord(r.Status)
	var (
		detail    string
		truncated bool
	)
	switch r.Status {
	case StatusOK:
		detail, truncated = firstLineSnippet(r.OutputCapture)
	case StatusBadArg, StatusError, StatusTimeout:
		detail = sanitizeReplyText(r.StatusDetail)
	}
	if detail == "" {
		return word, truncated
	}
	full := word + ": " + detail
	if utf8.RuneCountInString(full) <= MaxReplyLen {
		return full, truncated
	}
	return truncateReply(full), true
}

func firstLineSnippet(s string) (string, bool) {
	s = sanitizeReplyText(s)
	if utf8.RuneCountInString(s) > 50 {
		runes := []rune(s)
		return string(runes[:50]) + "…", true
	}
	return s, false
}

func sanitizeReplyText(s string) string {
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	return strings.TrimSpace(s)
}

func truncateReply(s string) string {
	limit := MaxReplyLen - 1 // leave room for the … rune
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return fmt.Sprintf("%s…", string(runes[:limit]))
}
