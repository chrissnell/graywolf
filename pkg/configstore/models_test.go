package configstore

import "testing"

// ValidKissMode is the only gatekeeper between a user-supplied string
// and a stored row, so its rejection behavior — in particular on case
// variants and surrounding whitespace — is part of the API contract.
func TestValidKissMode(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"modem", true},
		{"tnc", true},
		{"", false},
		{"Modem", false},
		{"MODEM", false},
		{"Tnc", false},
		{"TNC", false},
		{"tnc ", false},
		{" tnc", false},
		{"foo", false},
		{"modem\n", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := ValidKissMode(c.in); got != c.want {
				t.Fatalf("ValidKissMode(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// The exported constants are referenced by the DTO and kiss packages;
// pin their string forms so a rename here fails loudly at the test
// boundary rather than silently breaking downstream consumers.
func TestKissModeConstants(t *testing.T) {
	if KissModeModem != "modem" {
		t.Errorf("KissModeModem = %q, want %q", KissModeModem, "modem")
	}
	if KissModeTnc != "tnc" {
		t.Errorf("KissModeTnc = %q, want %q", KissModeTnc, "tnc")
	}
}
