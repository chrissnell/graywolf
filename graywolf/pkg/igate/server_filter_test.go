package igate

import (
	"fmt"
	"strings"
	"testing"
)

func TestComposeServerFilter(t *testing.T) {
	tests := []struct {
		name      string
		base      string
		tacticals []string
		want      string
	}{
		{
			name:      "empty base and nil tacticals returns empty",
			base:      "",
			tacticals: nil,
			want:      "",
		},
		{
			name:      "empty base and empty slice returns empty",
			base:      "",
			tacticals: []string{},
			want:      "",
		},
		{
			name:      "base only with no tacticals returns base unchanged",
			base:      "m/50",
			tacticals: nil,
			want:      "m/50",
		},
		{
			name:      "empty base with one tactical emits a single g clause",
			base:      "",
			tacticals: []string{"BASECAMP"},
			want:      "g/BASECAMP",
		},
		{
			name:      "base with m/50 plus one tactical appends g clause",
			base:      "m/50",
			tacticals: []string{"BASECAMP"},
			want:      "m/50 g/BASECAMP",
		},
		{
			name:      "base with existing g/X dedupes exact-match tactical",
			base:      "g/BASECAMP",
			tacticals: []string{"BASECAMP"},
			want:      "g/BASECAMP",
		},
		{
			name:      "base with multi-arg g/X/Y dedupes both",
			base:      "g/X/Y",
			tacticals: []string{"X", "Y", "Z"},
			want:      "g/X/Y g/Z",
		},
		{
			name:      "negation -g/X is opaque and X is still appended",
			base:      "-g/X",
			tacticals: []string{"X"},
			want:      "-g/X g/X",
		},
		{
			name:      "trailing slash on g/ token is tolerated",
			base:      "g/X/",
			tacticals: []string{"X", "Y"},
			want:      "g/X/ g/Y",
		},
		{
			name:      "tabs and CR and LF all split tokens",
			base:      "m/50\tg/X\r\ng/Y",
			tacticals: []string{"X", "Y", "Z"},
			want:      "m/50\tg/X\r\ng/Y g/Z",
		},
		{
			name:      "mixed whitespace is preserved verbatim in base",
			base:      "  m/50   g/X  ",
			tacticals: []string{"Z"},
			want:      "  m/50   g/X   g/Z",
		},
		{
			name:      "many tacticals combine into one g clause",
			base:      "",
			tacticals: []string{"A", "B", "C", "D"},
			want:      "g/A/B/C/D",
		},
		{
			name:      "case insensitive dedup: base g/x covers tactical X",
			base:      "g/x",
			tacticals: []string{"X"},
			want:      "g/x",
		},
		{
			name:      "case insensitive dedup: base G/x covers tactical X",
			base:      "G/x",
			tacticals: []string{"X"},
			want:      "G/x",
		},
		{
			name:      "duplicate tacticals in input slice emit only once",
			base:      "",
			tacticals: []string{"A", "A", "B", "a"},
			want:      "g/A/B",
		},
		{
			name:      "empty strings in tacticals are skipped",
			base:      "",
			tacticals: []string{"", "A", ""},
			want:      "g/A",
		},
		{
			name:      "tactical already in base is not duplicated",
			base:      "m/50 g/OPX",
			tacticals: []string{"OPX", "NEW"},
			want:      "m/50 g/OPX g/NEW",
		},
		{
			name:      "negation does not remove dedup source; g/X covers but -g/Y does not",
			base:      "g/X -g/Y",
			tacticals: []string{"X", "Y"},
			want:      "g/X -g/Y g/Y",
		},
		{
			name:      "whitespace-only base with tacticals preserves whitespace and appends",
			base:      "   ",
			tacticals: []string{"A"},
			want:      "    g/A",
		},
		{
			name:      "output of tacticals preserves slice order after dedup",
			base:      "",
			tacticals: []string{"C", "A", "B"},
			want:      "g/C/A/B",
		},
		{
			name:      "g/ token alone with no args contributes nothing to covered",
			base:      "g/",
			tacticals: []string{"X"},
			want:      "g/ g/X",
		},
		{
			name:      "p/ and other non-g tokens are ignored for dedup",
			base:      "p/W g/X m/25",
			tacticals: []string{"W", "X", "Y"},
			want:      "p/W g/X m/25 g/W/Y",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ComposeServerFilter(tc.base, tc.tacticals)
			if got != tc.want {
				t.Fatalf("ComposeServerFilter(%q, %v)\n  got:  %q\n  want: %q", tc.base, tc.tacticals, got, tc.want)
			}
		})
	}
}

// TestComposeServerFilter_ChunkingByByteBudget verifies that when the
// tactical set would push a single g/ clause past the byte budget, the
// output is split into multiple g/ clauses separated by a single space.
// Each individual clause must stay within the budget; APRS-IS OR's
// keywords so splitting is semantically neutral.
func TestComposeServerFilter_ChunkingByByteBudget(t *testing.T) {
	// 8-char names. Per-arg cost after the first in a clause: 1 ("/")
	// + 8 = 9. First arg cost: 2 ("g/") + 8 = 10. With budget 512:
	// 10 + (N-1)*9 <= 512 → N <= 56.77, so 57+ names force a split.
	var tacticals []string
	for i := range 120 {
		tacticals = append(tacticals, fmt.Sprintf("T%07d", i))
	}

	got := ComposeServerFilter("", tacticals)

	// The output must contain at least two g/ clauses.
	clauses := strings.Split(got, " ")
	gClauses := 0
	for _, c := range clauses {
		if strings.HasPrefix(c, "g/") {
			gClauses++
		}
		if len(c) > composedFilterByteBudget {
			t.Fatalf("clause exceeds per-clause byte budget: len=%d budget=%d clause=%q", len(c), composedFilterByteBudget, c)
		}
	}
	if gClauses < 2 {
		t.Fatalf("expected chunking to produce >=2 g/ clauses, got %d (output=%q)", gClauses, got)
	}

	// Every tactical should appear exactly once in the output.
	for _, tac := range tacticals {
		if strings.Count(got, tac) != 1 {
			t.Fatalf("tactical %q: expected exactly 1 occurrence, got %d", tac, strings.Count(got, tac))
		}
	}
}

// TestComposeServerFilter_SingleOverlongCallStillEmitted ensures a
// single callsign that is longer than the budget still gets its own
// clause rather than silently dropped. (In practice the configstore
// validator prevents this, but the helper must degrade gracefully.)
func TestComposeServerFilter_SingleOverlongCallStillEmitted(t *testing.T) {
	long := strings.Repeat("X", composedFilterByteBudget+10)
	got := ComposeServerFilter("", []string{long})
	want := "g/" + long
	if got != want {
		t.Fatalf("oversize single call: got %q, want %q", got, want)
	}
}
