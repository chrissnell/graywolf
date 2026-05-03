package actions

import (
	"strings"
	"testing"
)

func TestFormatReplyOK(t *testing.T) {
	got := FormatReply(Result{Status: StatusOK, OutputCapture: "lights on (2 bulbs, 38W)"})
	if got != "ok: lights on (2 bulbs, 38W)" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatReplyTruncates(t *testing.T) {
	long := strings.Repeat("x", 200)
	got := FormatReply(Result{Status: StatusOK, OutputCapture: long})
	if len(got) > MaxReplyLen {
		t.Fatalf("len %d > cap %d", len(got), MaxReplyLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("missing truncation marker: %q", got)
	}
}

func TestFormatReplyBadArg(t *testing.T) {
	got := FormatReply(Result{Status: StatusBadArg, StatusDetail: "state"})
	if got != "bad arg: state" {
		t.Fatalf("got %q", got)
	}
}

func TestFormatReplyStripsControlChars(t *testing.T) {
	got := FormatReply(Result{Status: StatusOK, OutputCapture: "a\nb\rc"})
	if strings.ContainsAny(got, "\n\r") {
		t.Fatalf("control chars leaked: %q", got)
	}
}
