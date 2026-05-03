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

func FormatReply(r Result) string {
	word := statusWord(r.Status)
	var detail string
	switch r.Status {
	case StatusOK:
		detail = firstLineSnippet(r.OutputCapture)
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

func firstLineSnippet(s string) string {
	s = sanitizeReplyText(s)
	if utf8.RuneCountInString(s) > 50 {
		runes := []rune(s)
		s = string(runes[:50]) + "…"
	}
	return s
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
