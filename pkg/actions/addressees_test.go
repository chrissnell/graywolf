package actions

import "testing"

func TestAddresseeSetEmptyByDefault(t *testing.T) {
	s := NewAddresseeSet()
	if s.Contains("GWACT") {
		t.Fatal("empty set should not contain anything")
	}
}

func TestAddresseeSetReplaceAndContains(t *testing.T) {
	s := NewAddresseeSet()
	s.Replace([]string{"gwact", "  cabin  ", ""})
	if !s.Contains("GWACT") {
		t.Fatal("expected GWACT after Replace")
	}
	if !s.Contains("cabin") {
		t.Fatal("expected case-insensitive match for 'cabin'")
	}
	if s.Contains("OTHER") {
		t.Fatal("unexpected member OTHER")
	}
}

func TestAddresseeSetReplaceClears(t *testing.T) {
	s := NewAddresseeSet()
	s.Replace([]string{"gwact"})
	s.Replace(nil)
	if s.Contains("GWACT") {
		t.Fatal("Replace(nil) should clear")
	}
	s.Replace([]string{"gwact"})
	s.Replace([]string{})
	if s.Contains("GWACT") {
		t.Fatal("Replace([]) should clear")
	}
}

func TestAddresseeSetContainsEmptyString(t *testing.T) {
	s := NewAddresseeSet()
	s.Replace([]string{"gwact"})
	if s.Contains("") {
		t.Fatal("empty string should never be a member")
	}
	if s.Contains("   ") {
		t.Fatal("whitespace-only should never be a member")
	}
}
