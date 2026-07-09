package messages

import "testing"

func TestBlocklistSet_EmptyBlocksNothing(t *testing.T) {
	s := NewBlocklistSet()
	if s.Blocked("N0CALL") {
		t.Fatal("empty set should block nothing")
	}
	if s.Blocked("") {
		t.Fatal("empty source should never be blocked")
	}
}

func TestBlocklistSet_BareCallsignBlocksAllSSIDs(t *testing.T) {
	s := NewBlocklistSet()
	s.Store(map[string]struct{}{"N0CALL": {}})

	cases := map[string]bool{
		"N0CALL":     true,
		"N0CALL-7":   true,
		"n0call-13":  true, // case-insensitive
		"N0CALLX":    false,
		"N0CALL-A":   true, // any SSID of the base call
		"OTHER":      false,
		"OTHER-1":    false,
	}
	for src, want := range cases {
		if got := s.Blocked(src); got != want {
			t.Errorf("Blocked(%q) = %v, want %v", src, got, want)
		}
	}
}

func TestBlocklistSet_SSIDQualifiedBlocksOnlyThatStation(t *testing.T) {
	s := NewBlocklistSet()
	s.Store(map[string]struct{}{"N0CALL-7": {}})

	if !s.Blocked("N0CALL-7") {
		t.Error("exact SSID match should be blocked")
	}
	if s.Blocked("N0CALL") {
		t.Error("bare base call should NOT be blocked by an SSID-qualified entry")
	}
	if s.Blocked("N0CALL-3") {
		t.Error("a different SSID should NOT be blocked")
	}
}

func TestBlocklistSet_StoreNormalizesAndClears(t *testing.T) {
	s := NewBlocklistSet()
	s.Store(map[string]struct{}{"  n0call ": {}, "": {}})
	if !s.Blocked("N0CALL") {
		t.Error("stored key should be trimmed+uppercased for matching")
	}
	// Empty keys are dropped, and a later Store replaces the snapshot.
	s.Store(nil)
	if s.Blocked("N0CALL") {
		t.Error("Store(nil) should clear the set")
	}
}
