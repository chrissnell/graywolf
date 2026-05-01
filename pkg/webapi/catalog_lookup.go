package webapi

import "github.com/chrissnell/graywolf/pkg/mapscatalog"

// slugInCatalog returns true if slug names a downloadable archive that
// is currently published in the catalog. Caller must have already
// validated the slug grammar via parseSlug; this is a closed-set
// membership check backed by a pre-built O(1) index on Catalog.
func slugInCatalog(cat mapscatalog.Catalog, slug string) bool {
	return cat.HasSlug(slug)
}
