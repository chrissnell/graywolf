package diagcollect

import "testing"

func TestParseSystemctlIsActive(t *testing.T) {
	cases := []struct {
		stdout string
		want   bool
	}{
		{"active\n", true},
		{"inactive\n", false},
		{"failed\n", false},
		{"", false},
	}
	for _, c := range cases {
		if got := parseSystemctlIsActive(c.stdout); got != c.want {
			t.Fatalf("parseSystemctlIsActive(%q) = %v, want %v", c.stdout, got, c.want)
		}
	}
}

func TestParseSystemctlNRestarts(t *testing.T) {
	cases := []struct {
		stdout string
		want   int
	}{
		{"NRestarts=0\n", 0},
		{"NRestarts=42\n", 42},
		{"NRestarts=\n", 0},
		{"unrelated", 0},
	}
	for _, c := range cases {
		if got := parseSystemctlNRestarts(c.stdout); got != c.want {
			t.Fatalf("parseSystemctlNRestarts(%q) = %d, want %d", c.stdout, got, c.want)
		}
	}
}

func TestCollectServiceStatus_AlwaysParseable(t *testing.T) {
	// On any host the function must return without panicking and
	// produce either Manager populated or a not_supported issue.
	got := CollectServiceStatus()
	if got.Manager == "" && len(got.Issues) == 0 {
		t.Fatalf("got %+v: empty Manager AND no issues", got)
	}
}
