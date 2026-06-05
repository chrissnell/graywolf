package dto

// LocalBounds is the wire shape for GET /api/maps/local-bounds.
// Keyed by namespaced slug ("state/colorado", "country/de",
// "province/ca/british-columbia"); value is [west, south, east, north]
// in degrees. Empty map (not 503) when no downloads are complete.
type LocalBounds map[string][4]float64
