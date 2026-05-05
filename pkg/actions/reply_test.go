package actions

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestFormatReplyOK(t *testing.T) {
	got, tr := FormatReply(Result{Status: StatusOK, OutputCapture: "lights on (2 bulbs, 38W)"})
	if got != "ok: lights on (2 bulbs, 38W)" {
		t.Fatalf("got %q", got)
	}
	if tr {
		t.Fatalf("unexpected truncation flag")
	}
}

func TestFormatReplyTruncates(t *testing.T) {
	long := strings.Repeat("x", 200)
	got, tr := FormatReply(Result{Status: StatusOK, OutputCapture: long})
	if len(got) > MaxReplyLen {
		t.Fatalf("len %d > cap %d", len(got), MaxReplyLen)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("missing truncation marker: %q", got)
	}
	if !tr {
		t.Fatalf("expected truncation flag")
	}
}

func TestFormatReplyBadArg(t *testing.T) {
	got, tr := FormatReply(Result{Status: StatusBadArg, StatusDetail: "state"})
	if got != "bad arg: state" {
		t.Fatalf("got %q", got)
	}
	if tr {
		t.Fatalf("unexpected truncation flag")
	}
}

func TestFormatReplyStripsControlChars(t *testing.T) {
	got, _ := FormatReply(Result{Status: StatusOK, OutputCapture: "a\nb\rc"})
	if strings.ContainsAny(got, "\n\r") {
		t.Fatalf("control chars leaked: %q", got)
	}
}

func TestFormatRepliesSingleLineOK(t *testing.T) {
	lines, tr := FormatReplies(Result{Status: StatusOK, OutputCapture: "lights on"}, 1)
	if len(lines) != 1 || lines[0] != "ok: lights on" {
		t.Fatalf("got %#v", lines)
	}
	if tr {
		t.Fatalf("unexpected truncation flag")
	}
}

func TestFormatRepliesMultiLineOK(t *testing.T) {
	out := "temp 72F\nwind 5mph N\nbaro 30.10\""
	lines, tr := FormatReplies(Result{Status: StatusOK, OutputCapture: out}, 3)
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %#v", len(lines), lines)
	}
	if lines[0] != "ok: temp 72F" {
		t.Fatalf("line 0: %q", lines[0])
	}
	if lines[1] != "wind 5mph N" {
		t.Fatalf("line 1: %q", lines[1])
	}
	if lines[2] != `baro 30.10"` {
		t.Fatalf("line 2: %q", lines[2])
	}
	if tr {
		t.Fatalf("unexpected truncation")
	}
}

func TestFormatRepliesDropsBlankLines(t *testing.T) {
	out := "first\n\n\nsecond\n"
	lines, _ := FormatReplies(Result{Status: StatusOK, OutputCapture: out}, 5)
	if len(lines) != 2 {
		t.Fatalf("want 2, got %d: %#v", len(lines), lines)
	}
	if lines[0] != "ok: first" || lines[1] != "second" {
		t.Fatalf("got %#v", lines)
	}
}

func TestFormatRepliesRespectsMax(t *testing.T) {
	out := "a\nb\nc\nd\ne\nf"
	lines, tr := FormatReplies(Result{Status: StatusOK, OutputCapture: out}, 3)
	if len(lines) != 3 {
		t.Fatalf("want 3, got %d", len(lines))
	}
	if !tr {
		t.Fatalf("expected truncation flag (3 of 6 lines)")
	}
}

func TestFormatRepliesClampsLineLength(t *testing.T) {
	out := strings.Repeat("x", 200) + "\nshort"
	lines, tr := FormatReplies(Result{Status: StatusOK, OutputCapture: out}, 2)
	if len(lines) != 2 {
		t.Fatalf("want 2, got %d", len(lines))
	}
	if utf8.RuneCountInString(lines[0]) > MaxReplyLen {
		t.Fatalf("line 0 over cap: %d", utf8.RuneCountInString(lines[0]))
	}
	if !strings.HasSuffix(lines[0], "…") {
		t.Fatalf("missing truncation marker")
	}
	if !tr {
		t.Fatalf("expected truncation flag")
	}
}

func TestFormatRepliesNonOKAlwaysSingleLine(t *testing.T) {
	lines, _ := FormatReplies(
		Result{Status: StatusBadArg, StatusDetail: "state"},
		5,
	)
	if len(lines) != 1 || lines[0] != "bad arg: state" {
		t.Fatalf("got %#v", lines)
	}
}

func TestFormatRepliesEmptyOutputOK(t *testing.T) {
	lines, _ := FormatReplies(Result{Status: StatusOK, OutputCapture: ""}, 3)
	if len(lines) != 1 || lines[0] != "ok" {
		t.Fatalf("got %#v", lines)
	}
}

func TestFormatRepliesStripsControlChars(t *testing.T) {
	out := "a\x07b\nc\x1bd"
	lines, _ := FormatReplies(Result{Status: StatusOK, OutputCapture: out}, 2)
	if len(lines) != 2 {
		t.Fatalf("want 2, got %d", len(lines))
	}
	for _, l := range lines {
		for _, r := range l {
			if r < 0x20 || r == 0x7F {
				t.Fatalf("control char %q in line %q", r, l)
			}
		}
	}
}

func TestFormatRepliesMaxLinesZeroDefaultsToOne(t *testing.T) {
	out := "a\nb"
	lines, tr := FormatReplies(Result{Status: StatusOK, OutputCapture: out}, 0)
	if len(lines) != 1 || lines[0] != "ok: a" {
		t.Fatalf("got %#v", lines)
	}
	if !tr {
		t.Fatalf("expected truncation flag (b dropped)")
	}
}
