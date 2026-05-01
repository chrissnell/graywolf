package webapi

import "github.com/chrissnell/graywolf/pkg/mapscatalog"

// slugInCatalog returns true if slug names a downloadable archive that
// is currently published in the catalog. Caller must have already
// validated the slug grammar via parseSlug; this is a closed-set
// membership check.
func slugInCatalog(cat mapscatalog.Catalog, slug string) bool {
	kind, a, b, ok := parseSlug(slug)
	if !ok {
		return false
	}
	switch kind {
	case "state":
		for _, s := range cat.States {
			if s.Slug == a {
				return true
			}
		}
	case "country":
		for _, c := range cat.Countries {
			if c.ISO2 == a {
				return true
			}
		}
	case "province":
		for _, p := range cat.Provinces {
			if p.ISO2 == a && p.Slug == b {
				return true
			}
		}
	}
	return false
}
