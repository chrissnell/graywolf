package releasenotes

import (
	"strings"
	"testing"
)

func TestPlainText_FlattensAndJoins(t *testing.T) {
	src := []byte(`
- version: "0.12.0"
  date: "2026-05-01"
  style: info
  title: "Shiny new thing"
  body: |
    First paragraph that is soft
    wrapped across two lines.

    See **bold** and *italic* and a
    [link to messages](#/messages) here.
`)
	restore := forceParse(src)
	defer restore()

	got, err := PlainText("0.12.0")
	if err != nil {
		t.Fatalf("PlainText: %v", err)
	}
	want := "Shiny new thing\n\n" +
		"First paragraph that is soft wrapped across two lines.\n\n" +
		"See bold and italic and a link to messages here."
	if got != want {
		t.Fatalf("got:\n%q\nwant:\n%q", got, want)
	}
}

func TestPlainText_MissingVersion(t *testing.T) {
	src := []byte(`
- version: "0.12.0"
  date: "2026-05-01"
  style: info
  title: "T"
  body: "B"
`)
	restore := forceParse(src)
	defer restore()

	if _, err := PlainText("9.9.9"); err == nil {
		t.Fatal("expected error for missing version, got nil")
	}
}

// TestPlainText_EmbeddedLatest renders the newest real changelog entry
// and confirms it fits Play's limit after truncation -- a guard that the
// release workflow's whatsnew step won't be rejected by the API.
func TestPlainText_EmbeddedLatest(t *testing.T) {
	notes, err := All()
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	// All() sorts CTA-first; find the newest version regardless of style.
	latest := notes[0].Version
	for _, n := range notes {
		if Compare(n.Version, latest) > 0 {
			latest = n.Version
		}
	}
	text, err := PlainText(latest)
	if err != nil {
		t.Fatalf("PlainText(%q): %v", latest, err)
	}
	if strings.TrimSpace(text) == "" {
		t.Fatalf("empty whatsnew for %q", latest)
	}
	out := Truncate(text, PlayWhatsNewMax)
	if n := len([]rune(out)); n > PlayWhatsNewMax {
		t.Fatalf("truncated text is %d chars, exceeds %d", n, PlayWhatsNewMax)
	}
}

func TestTruncate_UnderLimitUnchanged(t *testing.T) {
	s := "short and sweet."
	if got := Truncate(s, 100); got != s {
		t.Fatalf("got %q, want unchanged %q", got, s)
	}
}

func TestTruncate_SentenceBoundaryNoEllipsis(t *testing.T) {
	s := "First sentence here. Second sentence runs much longer and pushes well past the cap so it must be dropped entirely."
	got := Truncate(s, 30)
	if got != "First sentence here." {
		t.Fatalf("got %q, want clean sentence cut", got)
	}
	if len([]rune(got)) > 30 {
		t.Fatalf("got %d chars, exceeds 30", len([]rune(got)))
	}
}

func TestTruncate_WordBoundaryEllipsis(t *testing.T) {
	s := "alpha beta gamma delta epsilon zeta eta theta iota kappa lambda"
	got := Truncate(s, 20)
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected ellipsis, got %q", got)
	}
	if strings.Contains(got, "  ") {
		t.Fatalf("double space in %q", got)
	}
	if len([]rune(got)) > 20 {
		t.Fatalf("got %d chars, exceeds 20", len([]rune(got)))
	}
	// Must end on a whole word, never mid-word.
	body := strings.TrimSuffix(strings.TrimSpace(got), "...")
	if last := strings.Fields(body); len(last) > 0 && last[len(last)-1] == "epsil" {
		t.Fatalf("truncated mid-word: %q", got)
	}
}
