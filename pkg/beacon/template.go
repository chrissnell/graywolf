package beacon

import (
	"strings"
	"text/template"
)

// ExpandComment renders an APRS beacon comment through text/template so
// operators can embed dynamic tags like {{version}} in their static
// comment strings. Supported tags:
//
//	{{version}}  → the running graywolf version string
//
// Comments without "{{" are returned unchanged to avoid parsing overhead
// on the common case. On parse or execution error the original comment
// is returned so a typo can't silently blank every beacon.
func ExpandComment(comment, version string) string {
	if !strings.Contains(comment, "{{") {
		return comment
	}
	tmpl, err := template.New("beacon-comment").Funcs(template.FuncMap{
		"version": func() string { return version },
	}).Parse(comment)
	if err != nil {
		return comment
	}
	var sb strings.Builder
	if err := tmpl.Execute(&sb, nil); err != nil {
		return comment
	}
	return sb.String()
}
