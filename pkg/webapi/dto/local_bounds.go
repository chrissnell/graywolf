package dto

// LocalBoundsEntry is the render-path coverage for one downloaded
// slug: its bounding box plus the archive's max zoom. MaxZoom is 0 for
// regional (full-detail) archives and >0 for the capped world archive,
// letting the offline render path overzoom the world archive's top
// tile instead of requesting zooms it does not contain.
type LocalBoundsEntry struct {
	BBox    [4]float64 `json:"bbox"`
	MaxZoom int        `json:"maxZoom"`
}

// LocalBounds is the wire shape for GET /api/maps/local-bounds.
// Keyed by namespaced slug ("state/colorado", "country/de",
// "province/ca/british-columbia", "world"); value is the coverage
// entry. Empty map (not 503) when no downloads are complete.
type LocalBounds map[string]LocalBoundsEntry
