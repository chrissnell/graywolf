package webapi

import "github.com/chrissnell/graywolf/pkg/mapsslug"

// parseSlug delegates to pkg/mapsslug. Kept as a thin local wrapper so
// the existing handler/test call sites read naturally.
func parseSlug(s string) (kind, a, b string, ok bool) {
	return mapsslug.Parse(s)
}
