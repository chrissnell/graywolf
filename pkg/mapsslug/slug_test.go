package mapsslug

import "testing"

func TestParseWorld(t *testing.T) {
	kind, a, b, ok := Parse("world")
	if !ok || kind != "world" || a != "" || b != "" {
		t.Fatalf("Parse(\"world\") = (%q,%q,%q,%v), want (\"world\",\"\",\"\",true)", kind, a, b, ok)
	}
	for _, bad := range []string{"world/", "world/base", "world/x/y", "World", "worldx"} {
		if _, _, _, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) = ok, want rejected", bad)
		}
	}
}

func TestParseExisting(t *testing.T) {
	cases := []struct {
		in         string
		kind, a, b string
		ok         bool
	}{
		{"state/colorado", "state", "colorado", "", true},
		{"country/de", "country", "de", "", true},
		{"country/cn", "", "", "", false},
		{"country/ru", "", "", "", false},
		{"province/ca/british-columbia", "province", "ca", "british-columbia", true},
		{"", "", "", "", false},
		{"bogus/x", "", "", "", false},
	}
	for _, tc := range cases {
		k, a, b, ok := Parse(tc.in)
		if ok != tc.ok || k != tc.kind || a != tc.a || b != tc.b {
			t.Errorf("Parse(%q) = (%q,%q,%q,%v), want (%q,%q,%q,%v)",
				tc.in, k, a, b, ok, tc.kind, tc.a, tc.b, tc.ok)
		}
	}
}
