package blocklist

import (
	"testing"

	"github.com/chrissnell/graywolf/pkg/ax25"
)

func mustAddr(t *testing.T, s string) ax25.Address {
	t.Helper()
	a, err := ax25.ParseAddress(s)
	if err != nil {
		t.Fatalf("ParseAddress(%q): %v", s, err)
	}
	return a
}

func TestListMatches(t *testing.T) {
	cases := []struct {
		name    string
		entries []Entry
		src     string
		wantHit bool
		wantPat string // expected entry.Pattern on hit
	}{
		{
			name:    "bare matches SSID 0 only",
			entries: []Entry{{Pattern: "N1ROG"}},
			src:     "N1ROG",
			wantHit: true, wantPat: "N1ROG",
		},
		{
			name:    "bare does not match SSID > 0",
			entries: []Entry{{Pattern: "N1ROG"}},
			src:     "N1ROG-9",
			wantHit: false,
		},
		{
			name:    "CALL-0 matches bare source",
			entries: []Entry{{Pattern: "N1ROG-0"}},
			src:     "N1ROG",
			wantHit: true, wantPat: "N1ROG-0",
		},
		{
			name:    "CALL-N exact match",
			entries: []Entry{{Pattern: "N1ROG-9"}},
			src:     "N1ROG-9",
			wantHit: true, wantPat: "N1ROG-9",
		},
		{
			name:    "CALL-N does not match different SSID",
			entries: []Entry{{Pattern: "N1ROG-9"}},
			src:     "N1ROG-1",
			wantHit: false,
		},
		{
			name:    "wildcard matches any non-zero SSID",
			entries: []Entry{{Pattern: "N1ROG-*"}},
			src:     "N1ROG-1",
			wantHit: true, wantPat: "N1ROG-*",
		},
		{
			name:    "wildcard matches SSID 15",
			entries: []Entry{{Pattern: "N1ROG-*"}},
			src:     "N1ROG-15",
			wantHit: true, wantPat: "N1ROG-*",
		},
		{
			name:    "wildcard does NOT match bare callsign",
			entries: []Entry{{Pattern: "N1ROG-*"}},
			src:     "N1ROG",
			wantHit: false,
		},
		{
			name:    "wildcard does not match different base",
			entries: []Entry{{Pattern: "N1ROG-*"}},
			src:     "OTHER-9",
			wantHit: false,
		},
		{
			name: "first hit wins",
			entries: []Entry{
				{Pattern: "N1ROG-*", Reason: "first"},
				{Pattern: "N1ROG-9", Reason: "second"},
			},
			src:     "N1ROG-9",
			wantHit: true, wantPat: "N1ROG-*",
		},
		{
			name:    "case-insensitive match against source",
			entries: []Entry{{Pattern: "N1ROG-9"}},
			src:     "n1rog-9",
			wantHit: true, wantPat: "N1ROG-9",
		},
		{
			name:    "empty list never matches",
			entries: nil,
			src:     "N1ROG-9",
			wantHit: false,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			l := New(tc.entries)
			got, hit := l.Matches(mustAddr(t, tc.src))
			if hit != tc.wantHit {
				t.Fatalf("Matches(%q) hit=%v, want %v", tc.src, hit, tc.wantHit)
			}
			if hit && got.Pattern != tc.wantPat {
				t.Fatalf("Matches(%q) pattern=%q, want %q", tc.src, got.Pattern, tc.wantPat)
			}
		})
	}
}

func TestListSetReplacesEntries(t *testing.T) {
	l := New([]Entry{{Pattern: "N1ROG-9"}})
	if _, hit := l.Matches(mustAddr(t, "N1ROG-9")); !hit {
		t.Fatal("expected initial hit")
	}
	l.Set([]Entry{{Pattern: "OTHER-1"}})
	if _, hit := l.Matches(mustAddr(t, "N1ROG-9")); hit {
		t.Fatal("expected old entry to be gone after Set")
	}
	if _, hit := l.Matches(mustAddr(t, "OTHER-1")); !hit {
		t.Fatal("expected new entry to match after Set")
	}
	l.Set(nil)
	if _, hit := l.Matches(mustAddr(t, "OTHER-1")); hit {
		t.Fatal("Set(nil) should leave no matches")
	}
}
