package releasenotes

import (
	"strings"
	"testing"
)

// TestParseEmbedded forces an eager parse of the embedded notes.yaml so
// CI fails if a merge lands a malformed changelog.
func TestParseEmbedded(t *testing.T) {
	restore := forceParse(nil)
	defer restore()
	notes, err := All()
	if err != nil {
		t.Fatalf("parse embedded notes.yaml: %v", err)
	}
	if len(notes) < 2 {
		t.Fatalf("expected at least 2 seed entries, got %d", len(notes))
	}
	// Sanity: every note must have a version, title, body, and a known
	// style.
	for _, n := range notes {
		if n.Version == "" || n.Title == "" || n.Body == "" {
			t.Fatalf("empty field: %+v", n)
		}
		if n.Style != "info" && n.Style != "cta" {
			t.Fatalf("bad style: %+v", n)
		}
		if n.SchemaVersion < 1 {
			t.Fatalf("schema_version %d < 1", n.SchemaVersion)
		}
	}
}

func TestParseAndRender_Valid(t *testing.T) {
	src := []byte(`
- version: "0.11.0"
  date: "2026-04-21"
  style: cta
  title: "CTA two"
  body: "Please do **something**."
- version: "0.10.11"
  date: "2026-04-18"
  style: info
  title: "Info one"
  body: "See [Messages](#/messages)."
`)
	notes, err := parseAndRender(src)
	if err != nil {
		t.Fatalf("parseAndRender: %v", err)
	}
	if len(notes) != 2 {
		t.Fatalf("want 2 notes, got %d", len(notes))
	}
	// CTA first regardless of version age (here CTA is newer anyway).
	if notes[0].Style != "cta" {
		t.Fatalf("want cta first, got %q", notes[0].Style)
	}
	if notes[1].Style != "info" {
		t.Fatalf("want info second, got %q", notes[1].Style)
	}
	// schema_version defaults to 1 when absent.
	for _, n := range notes {
		if n.SchemaVersion != 1 {
			t.Fatalf("want default schema_version 1, got %d", n.SchemaVersion)
		}
	}
	// Link rendered.
	if !strings.Contains(notes[1].Body, `<a href="#/messages">Messages</a>`) {
		t.Fatalf("want link in body, got %q", notes[1].Body)
	}
	// Bold rendered.
	if !strings.Contains(notes[0].Body, `<strong>something</strong>`) {
		t.Fatalf("want bold in body, got %q", notes[0].Body)
	}
}

func TestCTASortingWithMixedAges(t *testing.T) {
	// An OLDER CTA must still sort above a NEWER info.
	src := []byte(`
- version: "0.12.0"
  date: "2026-05-01"
  style: info
  title: "Newer info"
  body: "Hello world."
- version: "0.11.0"
  date: "2026-04-21"
  style: cta
  title: "Older CTA"
  body: "Do a thing."
`)
	notes, err := parseAndRender(src)
	if err != nil {
		t.Fatalf("parseAndRender: %v", err)
	}
	if notes[0].Style != "cta" {
		t.Fatalf("expected older CTA first, got %+v then %+v", notes[0], notes[1])
	}
	if notes[0].Version != "0.11.0" {
		t.Fatalf("expected 0.11.0 first, got %q", notes[0].Version)
	}
}

func TestFileMustBeDescending(t *testing.T) {
	src := []byte(`
- version: "0.10.0"
  date: "2026-01-01"
  style: info
  title: "Older"
  body: "x"
- version: "0.11.0"
  date: "2026-04-21"
  style: info
  title: "Newer"
  body: "y"
`)
	if _, err := parseAndRender(src); err == nil {
		t.Fatal("expected error on ascending order")
	}
}

func TestDuplicateVersionRejected(t *testing.T) {
	src := []byte(`
- version: "0.11.0"
  date: "2026-04-21"
  style: info
  title: "A"
  body: "x"
- version: "0.11.0"
  date: "2026-04-22"
  style: info
  title: "B"
  body: "y"
`)
	if _, err := parseAndRender(src); err == nil {
		t.Fatal("expected error on duplicate versions")
	}
}

func TestSchemaValidation(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"missing version", `- date: "2026-04-21"
  style: info
  title: "X"
  body: "y"`},
		{"bad version", `- version: "v0.11.0"
  date: "2026-04-21"
  style: info
  title: "X"
  body: "y"`},
		{"non-strict semver", `- version: "0.11"
  date: "2026-04-21"
  style: info
  title: "X"
  body: "y"`},
		{"bad date", `- version: "0.11.0"
  date: "2026/04/21"
  style: info
  title: "X"
  body: "y"`},
		{"bad style", `- version: "0.11.0"
  date: "2026-04-21"
  style: warning
  title: "X"
  body: "y"`},
		{"empty title", `- version: "0.11.0"
  date: "2026-04-21"
  style: info
  title: ""
  body: "y"`},
		{"long title", `- version: "0.11.0"
  date: "2026-04-21"
  style: info
  title: "` + strings.Repeat("x", 81) + `"
  body: "y"`},
		{"empty body", `- version: "0.11.0"
  date: "2026-04-21"
  style: info
  title: "X"
  body: ""`},
		{"bad schema version", `- version: "0.11.0"
  date: "2026-04-21"
  style: info
  schema_version: 0
  title: "X"
  body: "y"`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseAndRender([]byte(tc.yaml)); err == nil {
				t.Fatalf("expected error for %s", tc.name)
			}
		})
	}
}

func TestLinkFormatValidation(t *testing.T) {
	for _, bad := range []string{
		"Go [home](https://evil.com).",
		"Go [home](javascript:alert(1)).",
		"Go [home](data:text/html,<script>alert(1)</script>).",
		"Go [home](//evil.com).",
		"Go [home](/callsign).",
		"Go [home](#top).",
		"Go [home](#).",
		"Go [home]().",
		"Go [home](# /callsign).", // whitespace inside href
	} {
		src := []byte(`
- version: "0.11.0"
  date: "2026-04-21"
  style: info
  title: "Bad"
  body: "` + bad + `"
`)
		if _, err := parseAndRender(src); err == nil {
			t.Fatalf("expected parse failure for body %q", bad)
		}
	}
}

func TestUnseen(t *testing.T) {
	restore := forceParse([]byte(`
- version: "0.12.0"
  date: "2026-05-01"
  style: info
  title: "T"
  body: "x"
- version: "0.11.0"
  date: "2026-04-21"
  style: cta
  title: "U"
  body: "y"
- version: "0.10.0"
  date: "2026-01-01"
  style: info
  title: "V"
  body: "z"
`))
	defer restore()
	// Empty lastSeen returns everything (in CTA-first sort order).
	notes, err := Unseen("")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 3 {
		t.Fatalf("want 3, got %d", len(notes))
	}
	if notes[0].Version != "0.11.0" || notes[0].Style != "cta" {
		t.Fatalf("want CTA first: %+v", notes[0])
	}
	// Filtered by version: only strictly newer notes.
	notes, err = Unseen("0.11.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 1 || notes[0].Version != "0.12.0" {
		t.Fatalf("want 0.12.0 only, got %+v", notes)
	}
	// Exceeding highest version: empty.
	notes, err = Unseen("0.99.0")
	if err != nil {
		t.Fatal(err)
	}
	if len(notes) != 0 {
		t.Fatalf("want none, got %d", len(notes))
	}
}

func TestParseErrorIsSticky(t *testing.T) {
	restore := forceParse([]byte(`:::not-yaml`))
	defer restore()
	if _, err := All(); err == nil {
		t.Fatal("expected parse error")
	}
	// Call again: still fails with the same error.
	if _, err := All(); err == nil {
		t.Fatal("expected sticky parse error on second call")
	}
	if _, err := Unseen(""); err == nil {
		t.Fatal("expected sticky error from Unseen")
	}
}

func TestCompareSemver(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "0.0.0", -1},
		{"0.0.0", "", 1},
		{"0.10.11", "0.11.0", -1},
		{"0.11.0", "0.10.11", 1},
		{"0.11.0", "0.11.0", 0},
		{"1.0.0", "0.99.99", 1},
		{"0.2.0", "0.10.0", -1},
		// Beta-suffix stripping: both collapse to 0.11.0.
		{"0.11.0-beta.3", "0.11.0", 0},
		{"0.11.0-beta.1", "0.11.0-beta.9", 0},
		// Dev build trimming: "dev" has no leading digits, strips to "".
		{"dev", "", 0},
		// Compare ignores anything after first non-[0-9.] character.
		{"0.11.0-abc1234-dirty", "0.11.0", 0},
		{"0.11.0-abc1234-dirty", "0.10.11", 1},
	}
	for _, tc := range cases {
		got := Compare(tc.a, tc.b)
		if got != tc.want {
			t.Errorf("Compare(%q,%q)=%d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestRendererXSSCorpus feeds the parser the classic XSS evasion
// vectors. Every output must either fail to parse (link format
// violation) or escape the unsafe bytes — never emit an unsafe tag or
// attribute.
func TestRendererXSSCorpus(t *testing.T) {
	// Cases split into "must reject at parse time" (unsafe hrefs)
	// and "must escape" (anything else).
	mustReject := []string{
		`[x](javascript:alert(1))`,
		`[x](JAVASCRIPT:alert(1))`,
		`[x](java&#x09;script:alert(1))`,
		`[x](data:text/html,<script>alert(1)</script>)`,
		`[x](DATA:text/html,...)`,
		`[x](//evil.com)`,
		`[x](http://evil.com)`,
		`[x](https://evil.com)`,
		`[x](mailto:x@y)`,
		`[x](vbscript:msgbox(1))`,
		`[x](file:///etc/passwd)`,
		`[x]( #/callsign)`, // leading space before href
	}
	for _, body := range mustReject {
		src := []byte(`
- version: "0.11.0"
  date: "2026-04-21"
  style: info
  title: "T"
  body: "` + body + `"
`)
		if _, err := parseAndRender(src); err == nil {
			t.Errorf("renderer accepted unsafe href in %q", body)
		}
	}

	// For these we render directly and assert the output only ever
	// contains the whitelisted tag set. Substrings like "onerror=" or
	// "javascript:" may legitimately appear as escaped text inside a
	// paragraph (where they can't execute); the invariant is that the
	// raw HTML bytes contain no '<' except the ones that open an
	// approved tag.
	mustEscape := []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`"><script>alert(1)</script>`,
		`' onerror='alert(1)`,
		`&lt;script&gt;alert(1)&lt;/script&gt;`,
		`**<img src=x>** emphasis with tag`,
		`*<a href="javascript:alert(1)">x</a>* em with raw anchor`,
		`[<script>alert(1)</script>](#/callsign)`, // link text has HTML
		`normal text with a < and a > and a " and a '`,
	}
	for _, body := range mustEscape {
		html, err := renderMarkdown(body)
		if err != nil {
			t.Errorf("renderMarkdown(%q) error: %v", body, err)
			continue
		}
		assertOnlyAllowedTags(t, body, html)
	}
}

// assertOnlyAllowedTags walks the rendered HTML and fails the test
// if any '<' opens anything other than one of the whitelisted tags:
// <p>, </p>, <strong>, </strong>, <em>, </em>, <a href="#/...">, or
// </a>. Any other '<' indicates a sanitization leak.
func assertOnlyAllowedTags(t *testing.T, input, html string) {
	t.Helper()
	allowedOpen := []string{
		"<p>",
		"</p>",
		"<strong>",
		"</strong>",
		"<em>",
		"</em>",
		"</a>",
	}
	i := 0
	for i < len(html) {
		c := html[i]
		if c != '<' {
			i++
			continue
		}
		rest := html[i:]
		// Check simple allowed tags by prefix.
		matched := false
		for _, tag := range allowedOpen {
			if strings.HasPrefix(rest, tag) {
				i += len(tag)
				matched = true
				break
			}
		}
		if matched {
			continue
		}
		// Check anchor open: must be `<a href="#/...">`.
		if strings.HasPrefix(rest, `<a href="#/`) {
			end := strings.Index(rest, `">`)
			if end < 0 {
				t.Fatalf("unterminated anchor in rendered html: input=%q output=%q", input, html)
			}
			// Verify the href value (between `<a href="` and `">`)
			// has no quote, angle bracket, or whitespace. The parser
			// rejected unsafe hrefs before we got here, so this is a
			// belt-and-suspenders check.
			hrefVal := rest[len(`<a href="`):end]
			for k := 0; k < len(hrefVal); k++ {
				ch := hrefVal[k]
				if ch == '"' || ch == '<' || ch == '>' || ch == ' ' || ch == '\t' {
					t.Fatalf("unsafe char %q in rendered href %q (input=%q)", ch, hrefVal, input)
				}
			}
			i += end + 2 // past `">`
			continue
		}
		t.Fatalf("unapproved '<' at offset %d in rendered html for input %q: %s",
			i, input, html)
	}
}

// TestRendererAllowedTags sanity: the renderer emits only <p>,
// <strong>, <em>, and <a href="#/..."> elements.
func TestRendererAllowedTags(t *testing.T) {
	html, err := renderMarkdown("Hello **bold** and *italic* and [link](#/callsign).")
	if err != nil {
		t.Fatal(err)
	}
	want := `<p>Hello <strong>bold</strong> and <em>italic</em> and <a href="#/callsign">link</a>.</p>`
	if html != want {
		t.Fatalf("unexpected html:\n got: %s\nwant: %s", html, want)
	}
}

// TestRendererParagraphs confirms blank-line-separated blocks become
// <p> blocks.
func TestRendererParagraphs(t *testing.T) {
	html, err := renderMarkdown("First para.\n\nSecond para.")
	if err != nil {
		t.Fatal(err)
	}
	want := "<p>First para.</p><p>Second para.</p>"
	if html != want {
		t.Fatalf("got %q, want %q", html, want)
	}
}

// TestRendererHtmlEntityTricks ensures entity-encoded attempts don't
// revive dangerous constructs. `&lt;script&gt;` must remain escaped
// (double-escaped in fact) rather than being decoded into a real tag.
func TestRendererHtmlEntityTricks(t *testing.T) {
	html, err := renderMarkdown(`&lt;script&gt;alert(1)&lt;/script&gt;`)
	if err != nil {
		t.Fatal(err)
	}
	// The ampersand must have been escaped to &amp;, not left raw,
	// so the browser can't interpret the entity.
	if !strings.Contains(html, "&amp;lt;script&amp;gt;") {
		t.Fatalf("expected escaped entities, got %s", html)
	}
}
