package igate

import (
	"strings"
	"testing"
)

func TestRFToISBlocklistParsing(t *testing.T) {
	normalized, err := ParseRFToISBlocklist(" k1abc, W5XYZ-07 \nw5xyz-7\nK1ABC ")
	if err != nil { t.Fatal(err) }
	if want := "K1ABC\nW5XYZ-7"; normalized != want {
		t.Fatalf("canonical = %q, want %q", normalized, want)
	}
	t.Run("blank is no filtering", func(t *testing.T) {
		l, err := ParseRFToISBlocklist("")
		if err != nil || l != "" { t.Fatalf("empty list = %q, %v", l, err) }
		if newRFToISBlocklist("").blocks("K1ABC") {
			t.Fatal("empty list excludes any station")
		}
	})
	for _, input := range []string{"K1ABC-16", "K1ABC-*", "LONGCALL7", "K1ABC/B", "?", strings.Repeat("K1ABC ", 101)} {
		t.Run(input[:min(len(input), 25)], func(t *testing.T) {
			if _, err := ParseRFToISBlocklist(input); err == nil {
				t.Fatalf("invalid blocklist %q accepted", input)
			}
		})
	}
}

func TestRFToISBlocklistSSIDMatching(t *testing.T) {
	l := newRFToISBlocklist("K1ABC\nW5XYZ-7\nN0CALL-0")
	for _, tc := range []struct {
		source string
		blocked bool
	}{
		{"K1ABC", true},
		{"K1ABC-9", true},
		{"k1abc-7", true},
		{"W5XYZ", false},
		{"W5XYZ-7", true},
		{"W5XYZ-8", false},
		{"N0CALL", true},
		{"N0CALL-3", false},
		{"ON4MAX", false},
	} {
		if got := l.blocks(tc.source); got != tc.blocked {
			t.Errorf("%s: blocked=%t want %t", tc.source, got, tc.blocked)
		}
	}
}
