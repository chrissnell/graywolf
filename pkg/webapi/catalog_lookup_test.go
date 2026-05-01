package webapi

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/mapscatalog"
)

func sampleCat() mapscatalog.Catalog {
	return mapscatalog.Catalog{
		SchemaVersion: 1,
		Countries:     []mapscatalog.Country{{ISO2: "de"}},
		Provinces:     []mapscatalog.Province{{ISO2: "ca", Slug: "british-columbia"}},
		States:        []mapscatalog.State{{Slug: "colorado"}},
	}
}

func TestSlugInCatalog(t *testing.T) {
	cat := sampleCat()
	cases := []struct {
		slug string
		want bool
	}{
		{"state/colorado", true},
		{"state/wyoming", false},
		{"country/de", true},
		{"country/fr", false},
		{"province/ca/british-columbia", true},
		{"province/ca/ontario", false},
		{"garbage", false},
	}
	for _, tc := range cases {
		got := slugInCatalog(cat, tc.slug)
		if got != tc.want {
			t.Errorf("slugInCatalog(%q) = %v, want %v", tc.slug, got, tc.want)
		}
	}
}
