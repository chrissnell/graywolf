package beacon

import "testing"

func TestExpandComment(t *testing.T) {
	cases := []struct {
		name, in, version, want string
	}{
		{"no tags", "Graywolf", "0.7.14", "Graywolf"},
		{"version tag", "Graywolf/{{version}}", "0.7.14", "Graywolf/0.7.14"},
		{"version tag only", "{{version}}", "0.7.14", "0.7.14"},
		{"empty", "", "0.7.14", ""},
		// Parse errors fall back to the literal comment so a typo
		// can't silently blank every outgoing beacon.
		{"malformed", "Graywolf/{{version", "0.7.14", "Graywolf/{{version"},
		{"unknown func", "Graywolf/{{bogus}}", "0.7.14", "Graywolf/{{bogus}}"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := ExpandComment(c.in, c.version)
			if got != c.want {
				t.Errorf("ExpandComment(%q, %q) = %q, want %q", c.in, c.version, got, c.want)
			}
		})
	}
}
