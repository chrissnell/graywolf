package webapi

import (
	"regexp"
	"strings"
)

// Slug grammar:
//   state/<state-slug>
//   country/<iso2>           (iso2 != cn|ru)
//   province/<iso2>/<slug>   (iso2 != cn|ru)
//
// state-slug and province-slug: ^[a-z][a-z0-9-]{0,49}$
// iso2: ^[a-z]{2}$
//
// Returned tuple is (kind, partA, partB, ok). For state/country, partB
// is "". The grammar is deliberately strict so we never reach R2 with a
// path-traversal payload; catalog membership is checked separately
// against the live catalog.

var (
	reSlugLeaf = regexp.MustCompile(`^[a-z][a-z0-9-]{0,49}$`)
	reISO2     = regexp.MustCompile(`^[a-z]{2}$`)
)

const maxSlugLen = 80

func parseSlug(s string) (kind, a, b string, ok bool) {
	if s == "" || len(s) > maxSlugLen {
		return "", "", "", false
	}
	parts := strings.Split(s, "/")
	switch parts[0] {
	case "state":
		if len(parts) != 2 || !reSlugLeaf.MatchString(parts[1]) {
			return "", "", "", false
		}
		return "state", parts[1], "", true
	case "country":
		if len(parts) != 2 || !reISO2.MatchString(parts[1]) {
			return "", "", "", false
		}
		if parts[1] == "cn" || parts[1] == "ru" {
			return "", "", "", false
		}
		return "country", parts[1], "", true
	case "province":
		if len(parts) != 3 || !reISO2.MatchString(parts[1]) || !reSlugLeaf.MatchString(parts[2]) {
			return "", "", "", false
		}
		if parts[1] == "cn" || parts[1] == "ru" {
			return "", "", "", false
		}
		return "province", parts[1], parts[2], true
	default:
		return "", "", "", false
	}
}
