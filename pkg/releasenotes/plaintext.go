package releasenotes

import (
	"fmt"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlayWhatsNewMax is Google Play's hard limit on the per-language
// "What's new" / release-notes field: 500 characters. The generator
// truncates to this so the Publishing API never rejects an over-long
// note.
const PlayWhatsNewMax = 500

var (
	// [text](href) -> text. The hrefs in notes.yaml are internal #/...
	// app routes that mean nothing in a store listing, so only the
	// visible text survives.
	mdLink = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	// **bold** then *italic* markers -- store "What's new" text is
	// unstyled plain text. Bold runs first so a leftover single star
	// from the bold pass can't confuse the italic pass.
	mdBold   = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	mdItalic = regexp.MustCompile(`\*([^*]+)\*`)
)

// PlainText renders the release note for version as plain UTF-8 text
// suitable for an app-store "What's new" field. The title is the first
// line, separated from the body by a blank line. Markdown links collapse
// to their visible text and bold/italic markers are stripped. Soft-
// wrapped lines within a paragraph join into one line; paragraphs are
// separated by a blank line (mirroring the in-app renderer's paragraph
// model). Returns an error if no note matches version.
//
// The result is NOT length-capped; pass it through Truncate for stores
// with a character limit (see PlayWhatsNewMax).
func PlainText(version string) (string, error) {
	var raws []rawNote
	if err := yaml.Unmarshal(source, &raws); err != nil {
		return "", fmt.Errorf("releasenotes: parse yaml: %w", err)
	}
	for _, r := range raws {
		if r.Version != version {
			continue
		}
		title := strings.TrimSpace(r.Title)
		paras := splitParagraphs(r.Body)
		for i, p := range paras {
			paras[i] = flattenInline(p)
		}
		body := strings.Join(paras, "\n\n")
		switch {
		case title == "":
			return body, nil
		case body == "":
			return title, nil
		default:
			return title + "\n\n" + body, nil
		}
	}
	return "", fmt.Errorf("releasenotes: no note for version %q", version)
}

// flattenInline strips the restricted markdown subset (links, bold,
// italic) down to plain text. Links resolve first so any bold/italic
// inside link text is then handled by the marker passes.
func flattenInline(s string) string {
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdBold.ReplaceAllString(s, "$1")
	s = mdItalic.ReplaceAllString(s, "$1")
	return strings.TrimSpace(s)
}

// Truncate shortens s to at most max characters, backing off to the
// nearest sentence end (a '.' followed by space, newline, or end of
// string) and then to a word boundary so the result never ends mid-word.
// A word-boundary or hard cut appends an ellipsis to signal there's
// more; a clean sentence end does not. max <= 0 or s already within the
// limit returns s unchanged. Length is measured in characters (runes).
func Truncate(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	const ell = "..."
	budget := max - len([]rune(ell))
	if budget <= 0 {
		return string(r[:max])
	}
	head := string(r[:budget])
	// A complete sentence reads cleanly; prefer it when it keeps at
	// least half the budget (avoids cutting back to a tiny fragment).
	if i := lastSentenceEnd(head); i >= budget/2 {
		return strings.TrimRight(head[:i+1], " \n")
	}
	if i := strings.LastIndexByte(head, ' '); i > 0 {
		return strings.TrimRight(head[:i], " \n") + " " + ell
	}
	return head + ell
}

// lastSentenceEnd returns the index of the last '.' that ends a sentence
// (followed by a space, newline, or the end of the string), or -1.
func lastSentenceEnd(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] != '.' {
			continue
		}
		if i+1 >= len(s) || s[i+1] == ' ' || s[i+1] == '\n' {
			return i
		}
	}
	return -1
}
