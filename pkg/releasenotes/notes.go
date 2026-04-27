// Package releasenotes owns the embedded release-note content that
// drives the "What's new" popup and About-page section.
//
// The authoritative list lives in notes.yaml, embedded at build time.
// Parsing (including markdown-to-HTML rendering) is lazy: the first
// call to All() or Unseen() triggers a single sync.Once pass that
// validates, sorts, and server-renders every note body. A malformed
// changelog surfaces as a 500 on the three /api/release-notes
// endpoints — the rest of the binary (iGate, digipeater, webapi) stays
// up.
//
// Sort order is fixed at parse time (CTA-first, then newest-version
// descending) so handlers serve pre-sorted bytes without per-request
// work.
package releasenotes

import (
	_ "embed"
	"errors"
	"fmt"
	"html/template"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed notes.yaml
var notesYAML []byte

// maxTitleLen caps a release-note title. Enforced at parse time so a
// long title fails CI (via make go-test's eager parse) rather than
// sneaking into an operator's popup. Chosen to comfortably fit the
// card header at all supported viewport widths.
const maxTitleLen = 80

// Note is the parsed, post-render shape of a single release-note
// entry. Body carries server-rendered, sanitized HTML (see D5) — it
// is safe to hand to the frontend as-is and bind via {@html ...}.
type Note struct {
	SchemaVersion int    `json:"schema_version"`
	Version       string `json:"version"`
	Date          string `json:"date"`
	Style         string `json:"style"`
	Title         string `json:"title"`
	Body          string `json:"body"`
}

// rawNote is the YAML-side shape before rendering. `schema_version`
// is optional and defaults to 1 when omitted.
type rawNote struct {
	SchemaVersion *int   `yaml:"schema_version"`
	Version       string `yaml:"version"`
	Date          string `yaml:"date"`
	Style         string `yaml:"style"`
	Title         string `yaml:"title"`
	Body          string `yaml:"body"`
}

// parse state is package-level and guarded by parseOnce. If parse
// fails, parseErr is set and every subsequent handler call returns it
// until process restart. parseOnce is a pointer so forceParse can
// swap a fresh *sync.Once in without copying the lock.
var (
	parseOnce = &sync.Once{}
	parsed    []Note
	parseErr  error

	// source allows tests to swap in their own YAML bytes via
	// forceParse. Production leaves it at the embedded content.
	source = notesYAML
)

// All returns every parsed note in CTA-first, version-desc order.
// Triggers the lazy parse on first call.
func All() ([]Note, error) {
	ensureParsed()
	if parseErr != nil {
		return nil, parseErr
	}
	// Return a shallow copy so a caller can't scramble package state.
	out := make([]Note, len(parsed))
	copy(out, parsed)
	return out, nil
}

// Unseen returns notes strictly newer than lastSeen, in the same
// CTA-first, version-desc order. An empty lastSeen returns every note
// (per the semver Compare contract: "" < any real version).
func Unseen(lastSeen string) ([]Note, error) {
	all, err := All()
	if err != nil {
		return nil, err
	}
	out := make([]Note, 0, len(all))
	for _, n := range all {
		if Compare(n.Version, lastSeen) > 0 {
			out = append(out, n)
		}
	}
	return out, nil
}

func ensureParsed() {
	parseOnce.Do(func() {
		parsed, parseErr = parseAndRender(source)
	})
}

// forceParse is a test hook. It resets the sync.Once and optionally
// swaps the source bytes; the next All()/Unseen() call triggers a
// fresh parse. Returns the swap-back function so tests can restore
// state via t.Cleanup.
func forceParse(src []byte) func() {
	prevSrc := source
	if src != nil {
		source = src
	}
	prevOnce := parseOnce
	prevParsed := parsed
	prevErr := parseErr

	parseOnce = &sync.Once{}
	parsed = nil
	parseErr = nil

	return func() {
		source = prevSrc
		parseOnce = prevOnce
		parsed = prevParsed
		parseErr = prevErr
	}
}

// parseAndRender validates YAML, renders each body, and sorts the
// result. Pure function of its input for testability.
func parseAndRender(src []byte) ([]Note, error) {
	var raws []rawNote
	if err := yaml.Unmarshal(src, &raws); err != nil {
		return nil, fmt.Errorf("releasenotes: parse yaml: %w", err)
	}
	if len(raws) == 0 {
		return nil, errors.New("releasenotes: notes.yaml is empty")
	}
	out := make([]Note, 0, len(raws))
	seenVersions := make(map[string]struct{}, len(raws))
	for i, r := range raws {
		n, err := renderOne(r)
		if err != nil {
			return nil, fmt.Errorf("releasenotes: entry %d (%q): %w", i, r.Version, err)
		}
		if _, dup := seenVersions[n.Version]; dup {
			return nil, fmt.Errorf("releasenotes: duplicate version %q", n.Version)
		}
		seenVersions[n.Version] = struct{}{}
		out = append(out, n)
	}
	// Enforce source-file ordering: strictly descending by version.
	// Catches merge mistakes where two entries land out of order.
	for i := 1; i < len(out); i++ {
		if Compare(out[i-1].Version, out[i].Version) <= 0 {
			return nil, fmt.Errorf(
				"releasenotes: file ordering: entry %q must come after %q (strict descending)",
				out[i-1].Version, out[i].Version,
			)
		}
	}
	// Stable sort: CTA-first, then version descending. The file is
	// already version-desc (checked above), so the effect is to pull
	// every CTA to the top while preserving its own version order.
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := out[i].Style, out[j].Style
		if si != sj {
			return si == "cta"
		}
		return Compare(out[i].Version, out[j].Version) > 0
	})
	return out, nil
}

// renderOne validates a single raw note and renders its body to
// sanitized HTML. Returns an error on any validation or link-format
// failure so the package parse fails loudly (a silent runtime drop
// would be worse — it would ship operators a silently-broken note).
func renderOne(r rawNote) (Note, error) {
	if r.Version == "" {
		return Note{}, errors.New("version is required")
	}
	if !isStrictSemver(r.Version) {
		return Note{}, fmt.Errorf("version %q is not strict x.y.z", r.Version)
	}
	if r.Date == "" {
		return Note{}, errors.New("date is required")
	}
	if _, err := time.Parse("2006-01-02", r.Date); err != nil {
		return Note{}, fmt.Errorf("date %q is not YYYY-MM-DD", r.Date)
	}
	switch r.Style {
	case "info", "cta":
	default:
		return Note{}, fmt.Errorf("style %q must be \"info\" or \"cta\"", r.Style)
	}
	if r.Title == "" {
		return Note{}, errors.New("title is required")
	}
	if len(r.Title) > maxTitleLen {
		return Note{}, fmt.Errorf("title length %d exceeds %d chars", len(r.Title), maxTitleLen)
	}
	if strings.TrimSpace(r.Body) == "" {
		return Note{}, errors.New("body is required")
	}
	html, err := renderMarkdown(r.Body)
	if err != nil {
		return Note{}, fmt.Errorf("render body: %w", err)
	}
	schema := 1
	if r.SchemaVersion != nil {
		schema = *r.SchemaVersion
	}
	if schema < 1 {
		return Note{}, fmt.Errorf("schema_version %d must be >= 1", schema)
	}
	return Note{
		SchemaVersion: schema,
		Version:       r.Version,
		Date:          r.Date,
		Style:         r.Style,
		Title:         r.Title,
		Body:          html,
	}, nil
}

// isStrictSemver enforces exactly three non-negative integer
// components separated by dots. Refuses leading zeros? No — "0.11.0"
// is perfectly valid, only leading zeros like "01" would be weird and
// we simply accept them (strconv-parseable is the bar). Refuses any
// prefix like "v".
func isStrictSemver(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for i := 0; i < len(p); i++ {
			if p[i] < '0' || p[i] > '9' {
				return false
			}
		}
	}
	return true
}

// renderMarkdown translates the restricted markdown subset to
// sanitized HTML. The supported constructs are:
//
//   - blank lines → paragraph breaks
//   - **bold** → <strong>
//   - *italic* → <em>
//   - [text](#/path) → <a href="#/path">text</a>, with text subject to
//     the same inline rules recursively (bold / italic inside link
//     text is allowed; nested links are not).
//
// Everything else is HTML-escaped via html/template.HTMLEscapeString.
// Any link whose href does not start with "#/" is rejected at parse
// time (we never silently drop the link or emit an unsafe href).
func renderMarkdown(body string) (string, error) {
	// Normalize line endings so paragraph splitting is deterministic.
	body = strings.ReplaceAll(body, "\r\n", "\n")
	body = strings.ReplaceAll(body, "\r", "\n")
	// Split on blank lines into paragraphs. A "blank line" is a line
	// that is empty or whitespace-only after trim.
	paragraphs := splitParagraphs(body)

	var b strings.Builder
	for _, p := range paragraphs {
		b.WriteString("<p>")
		rendered, err := renderInline(p)
		if err != nil {
			return "", err
		}
		b.WriteString(rendered)
		b.WriteString("</p>")
	}
	return b.String(), nil
}

func splitParagraphs(body string) []string {
	lines := strings.Split(body, "\n")
	var paras []string
	var cur []string
	flush := func() {
		if len(cur) == 0 {
			return
		}
		// Join with a single space: markdown soft-wraps inside a
		// paragraph become a single logical line.
		joined := strings.TrimSpace(strings.Join(cur, " "))
		if joined != "" {
			paras = append(paras, joined)
		}
		cur = cur[:0]
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		cur = append(cur, strings.TrimSpace(line))
	}
	flush()
	return paras
}

// renderInline walks a paragraph and emits escaped HTML with the
// supported inline constructs converted. The tokenizer is
// deliberately small — it does not try to match full CommonMark. Any
// construct we don't recognize is treated as literal text and
// escaped.
func renderInline(s string) (string, error) {
	var b strings.Builder
	i := 0
	for i < len(s) {
		switch {
		case strings.HasPrefix(s[i:], "**"):
			// Bold: find the matching "**".
			end := strings.Index(s[i+2:], "**")
			if end < 0 {
				// Unclosed emphasis: treat literally.
				b.WriteString(template.HTMLEscapeString("**"))
				i += 2
				continue
			}
			inner := s[i+2 : i+2+end]
			// Bold doesn't allow a link inside it per our subset —
			// keep the renderer simple; escape inner text content,
			// but we still walk it to allow nested *italic* for
			// completeness (rare). Simpler: escape literally.
			b.WriteString("<strong>")
			sub, err := renderInline(inner)
			if err != nil {
				return "", err
			}
			b.WriteString(sub)
			b.WriteString("</strong>")
			i += 2 + end + 2
		case s[i] == '*' && !isAsteriskPair(s, i):
			// Italic: find the matching single "*" that is not part
			// of a "**".
			end := findSingleStar(s[i+1:])
			if end < 0 {
				b.WriteString(template.HTMLEscapeString("*"))
				i++
				continue
			}
			inner := s[i+1 : i+1+end]
			b.WriteString("<em>")
			sub, err := renderInline(inner)
			if err != nil {
				return "", err
			}
			b.WriteString(sub)
			b.WriteString("</em>")
			i += 1 + end + 1
		case s[i] == '[':
			// Link: [text](href).
			textEnd := strings.Index(s[i+1:], "]")
			if textEnd < 0 {
				b.WriteString(template.HTMLEscapeString("["))
				i++
				continue
			}
			// Must be immediately followed by '('.
			openParen := i + 1 + textEnd + 1
			if openParen >= len(s) || s[openParen] != '(' {
				b.WriteString(template.HTMLEscapeString("["))
				i++
				continue
			}
			closeParen := strings.Index(s[openParen+1:], ")")
			if closeParen < 0 {
				b.WriteString(template.HTMLEscapeString("["))
				i++
				continue
			}
			text := s[i+1 : i+1+textEnd]
			href := s[openParen+1 : openParen+1+closeParen]
			if !isAllowedHref(href) {
				return "", fmt.Errorf("link href %q is not an internal #/... route", href)
			}
			b.WriteString(`<a href="`)
			// href has been validated to start with "#/"; still
			// escape it defensively so any characters inside the
			// path that would break out of the attribute value
			// context (quotes, angle brackets) get neutralized.
			b.WriteString(template.HTMLEscapeString(href))
			b.WriteString(`">`)
			// Link text supports the same inline rules, so bold or
			// italic inside a link works. Nested links are not
			// possible because '[' inside the inner text would be
			// escaped on the non-matched path.
			inner, err := renderInline(text)
			if err != nil {
				return "", err
			}
			b.WriteString(inner)
			b.WriteString("</a>")
			i = openParen + 1 + closeParen + 1
		default:
			// Literal character: write its HTML-escaped form.
			b.WriteString(template.HTMLEscapeString(string(s[i])))
			i++
		}
	}
	return b.String(), nil
}

// isAsteriskPair reports whether s[i] is the first byte of a "**"
// pair. Used to disambiguate italic "*" from bold "**".
func isAsteriskPair(s string, i int) bool {
	return i+1 < len(s) && s[i+1] == '*'
}

// findSingleStar locates the next single '*' that is not part of
// "**". Returns the index in s, or -1 if none.
func findSingleStar(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] != '*' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '*' {
			i++ // skip the pair
			continue
		}
		return i
	}
	return -1
}

// isAllowedHref enforces the "internal hash route only" policy.
// Accepts exactly hrefs that begin with "#/" and contain no embedded
// whitespace, quotes, angle brackets, or control characters. Rejects
// everything else — external URLs, "javascript:", "data:",
// protocol-relative ("//evil.com"), absolute paths, bare fragments
// ("#top"), and empty strings.
func isAllowedHref(href string) bool {
	if !strings.HasPrefix(href, "#/") {
		return false
	}
	for i := 0; i < len(href); i++ {
		c := href[i]
		if c < 0x20 || c == 0x7f {
			return false
		}
		switch c {
		case ' ', '\t', '"', '\'', '<', '>', '`':
			return false
		}
	}
	return true
}
