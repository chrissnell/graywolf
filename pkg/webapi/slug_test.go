package webapi

import (
	"strings"
	"testing"
)

func TestParseSlug(t *testing.T) {
	cases := []struct {
		in       string
		wantKind string
		wantA    string
		wantB    string
		wantOK   bool
	}{
		{"state/colorado", "state", "colorado", "", true},
		{"state/new-hampshire", "state", "new-hampshire", "", true},
		{"country/de", "country", "de", "", true},
		{"province/ca/british-columbia", "province", "ca", "british-columbia", true},

		{"colorado", "", "", "", false},                  // legacy bare
		{"state/", "", "", "", false},                    // empty leaf
		{"state/Colorado", "", "", "", false},            // uppercase
		{"country/usa", "", "", "", false},               // 3 chars
		{"country/cn", "", "", "", false},                // forbidden
		{"country/ru", "", "", "", false},                // forbidden
		{"province/ca/", "", "", "", false},              // empty slug
		{"province/ca/cn", "province", "ca", "cn", true}, // "cn" only banned at country level
		{"province/cn/foo", "", "", "", false},
		{"province/ru/foo", "", "", "", false},
		{"unknown/xyz", "", "", "", false},
		{"state/colorado/extra", "", "", "", false},
		{"", "", "", "", false},
		{"state/" + strings.Repeat("a", 100), "", "", "", false}, // length cap
	}
	for _, tc := range cases {
		k, a, b, ok := parseSlug(tc.in)
		if ok != tc.wantOK || k != tc.wantKind || a != tc.wantA || b != tc.wantB {
			t.Errorf("parseSlug(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				tc.in, k, a, b, ok, tc.wantKind, tc.wantA, tc.wantB, tc.wantOK)
		}
	}
}
