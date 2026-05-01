package dto

// Catalog is the response shape for GET /api/maps/catalog. It mirrors
// the Worker's /manifest.json output 1:1 so the UI can reuse the
// arrays without translation. SchemaVersion is pinned to 1; bumps
// require a coordinated UI update.
type Catalog struct {
	SchemaVersion int               `json:"schemaVersion"`
	GeneratedAt   string            `json:"generatedAt"`
	Countries     []CatalogCountry  `json:"countries"`
	Provinces     []CatalogProvince `json:"provinces"`
	States        []CatalogState    `json:"states"`
}

type CatalogCountry struct {
	ISO2      string      `json:"iso2"`
	Name      string      `json:"name"`
	SizeBytes int64       `json:"sizeBytes"`
	SHA256    string      `json:"sha256"`
	BBox      *[4]float64 `json:"bbox,omitempty"`
}

type CatalogProvince struct {
	ISO2      string      `json:"iso2"`
	Slug      string      `json:"slug"`
	Name      string      `json:"name"`
	Code      string      `json:"code,omitempty"`
	SizeBytes int64       `json:"sizeBytes"`
	SHA256    string      `json:"sha256"`
	BBox      *[4]float64 `json:"bbox,omitempty"`
}

type CatalogState struct {
	Slug      string      `json:"slug"`
	Name      string      `json:"name"`
	Code      string      `json:"code,omitempty"`
	SizeBytes int64       `json:"sizeBytes"`
	SHA256    string      `json:"sha256"`
	BBox      *[4]float64 `json:"bbox,omitempty"`
}
