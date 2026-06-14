package releasenotes

import (
	"fmt"
	"html"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// PlayWhatsNewMax is Google Play's hard limit on the per-language
// "What's new" / release-notes field: 500 characters. The generator
// truncates to this so the Publishing API never rejects an over-long
// note.
const PlayWhatsNewMax = 500

// htmlTag matches a single tag emitted by the release-note renderer
// (<p>, <strong>, <em>, <a ...>). Their attribute values are
// HTML-escaped, so a literal '>' never appears inside a tag.
var htmlTag = regexp.MustCompile(`<[^>]+>`)

// PlainText renders the release note for version as plain UTF-8 text
// suitable for an app-store "What's new" field. The title is the first
// line, separated from the body by a blank line.
//
// The body is produced by the in-app renderer (renderMarkdown) and then
// stripped to text, so the result is exactly what the in-app "What's
// new" popup shows minus styling and link targets: bold/italic markers
// and links collapse to their visible text (nested markers included),
// paragraphs are separated by a blank line, and a deliberate literal
// such as the CALL-* wildcard is preserved. Reusing the renderer means
// there is no second markdown implementation to drift from the popup.
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
		htmlBody, err := renderMarkdown(r.Body)
		if err != nil {
			return "", fmt.Errorf("releasenotes: render %q: %w", version, err)
		}
		body := htmlToText(htmlBody)
		title := strings.TrimSpace(r.Title)
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

// htmlToText converts the renderer's sanitized HTML to plain text:
// paragraph breaks become blank lines, all tags are removed, and HTML
// entities are decoded. Tags are stripped BEFORE unescaping so escaped
// text like "&lt;script&gt;" is never resurrected into a real tag.
func htmlToText(h string) string {
	h = strings.ReplaceAll(h, "</p>", "\n\n")
	h = htmlTag.ReplaceAllString(h, "")
	h = html.UnescapeString(h)
	return strings.TrimSpace(h)
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
	// head is rune-correct. The boundary searches below return BYTE
	// offsets, but every boundary char ('.', ' ', '\n') is single-byte
	// ASCII, so slicing head at those offsets is safe. notes.yaml is
	// ASCII-only by convention; this is the one spot that relies on it.
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
