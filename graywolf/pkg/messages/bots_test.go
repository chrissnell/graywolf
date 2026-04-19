package messages

import "testing"

func TestIsWellKnownBot(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"SMS", true},
		{"sms", true},
		{" sms ", true},
		{"FIND", true},
		{"WLNK-1", true},
		{"wlnk-1", true},
		{"N0CALL", false},
		{"", false},
		{"UNKNOWN", false},
	}
	for _, tc := range tests {
		got := IsWellKnownBot(tc.in)
		if got != tc.want {
			t.Errorf("IsWellKnownBot(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestDefaultBotDirectory_List(t *testing.T) {
	list := DefaultBotDirectory.List()
	if len(list) != len(WellKnownBots) {
		t.Errorf("expected %d entries, got %d", len(WellKnownBots), len(list))
	}
}

func TestDefaultBotDirectory_Match(t *testing.T) {
	tests := []struct {
		prefix  string
		wantOne string // a bot we expect to find
	}{
		{"sm", "SMS"},
		{"W", "WXBOT"},
		{"wlnk", "WLNK-1"},
		{"", "SMS"}, // empty — returns everything
	}
	for _, tc := range tests {
		matches := DefaultBotDirectory.Match(tc.prefix)
		found := false
		for _, m := range matches {
			if m.Callsign == tc.wantOne {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Match(%q) did not include %q; got %+v", tc.prefix, tc.wantOne, matches)
		}
	}
}

func TestDefaultBotDirectory_MatchEmptyCase(t *testing.T) {
	matches := DefaultBotDirectory.Match("XYZZY-NOMATCH")
	if len(matches) != 0 {
		t.Errorf("expected no matches, got %+v", matches)
	}
}

func TestNewBotDirectory_IsolatedFromDefault(t *testing.T) {
	dir := NewBotDirectory([]BotAddress{{Callsign: "AAA", Description: "test"}})
	if len(dir.List()) != 1 {
		t.Errorf("expected 1 entry, got %+v", dir.List())
	}
	// DefaultBotDirectory must still be unchanged.
	if len(DefaultBotDirectory.List()) != len(WellKnownBots) {
		t.Errorf("DefaultBotDirectory was mutated")
	}
}
