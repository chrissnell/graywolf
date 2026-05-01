// Package mapsslug owns the namespaced slug grammar for offline-map
// downloads. Both pkg/webapi and pkg/mapscache import this package, so
// the regex, length cap, and forbidden-country list live in exactly one
// place.
//
// Grammar:
//
//	state/<state-slug>
//	country/<iso2>           (iso2 != cn|ru)
//	province/<iso2>/<slug>   (iso2 != cn|ru)
//
// state-slug and province-slug: ^[a-z][a-z0-9-]{0,49}$
// iso2: ^[a-z]{2}$
//
// The grammar is deliberately strict so an attacker cannot reach R2 or
// the local cache filesystem with a path-traversal payload. Catalog
// membership is checked separately against the live catalog.
package mapsslug

import (
	"regexp"
	"strings"
)

const MaxLen = 80

var (
	reSlugLeaf = regexp.MustCompile(`^[a-z][a-z0-9-]{0,49}$`)
	reISO2     = regexp.MustCompile(`^[a-z]{2}$`)
)

// forbidden iso2 codes (China, Russia) — never publish or accept as a
// download target regardless of upstream catalog contents.
var forbiddenISO2 = map[string]bool{"cn": true, "ru": true}

// Parse validates s against the grammar. Returns (kind, partA, partB, ok).
// For state and country, partB is "".
func Parse(s string) (kind, a, b string, ok bool) {
	if s == "" || len(s) > MaxLen {
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
		if len(parts) != 2 || !reISO2.MatchString(parts[1]) || forbiddenISO2[parts[1]] {
			return "", "", "", false
		}
		return "country", parts[1], "", true
	case "province":
		if len(parts) != 3 || !reISO2.MatchString(parts[1]) || !reSlugLeaf.MatchString(parts[2]) {
			return "", "", "", false
		}
		if forbiddenISO2[parts[1]] {
			return "", "", "", false
		}
		return "province", parts[1], parts[2], true
	}
	return "", "", "", false
}

// LeafRegexp returns the slug-leaf regex for callers that need to apply
// it to a non-namespaced string (e.g. legacy on-disk filenames during
// migration).
func LeafRegexp() *regexp.Regexp { return reSlugLeaf }
