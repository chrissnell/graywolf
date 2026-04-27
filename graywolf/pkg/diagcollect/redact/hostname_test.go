package redact

import "testing"

func TestHashHostname_Deterministic(t *testing.T) {
	a := HashHostname("rosie-pi")
	b := HashHostname("rosie-pi")
	if a != b {
		t.Fatalf("hash not deterministic: %q vs %q", a, b)
	}
}

func TestHashHostname_TruncatedToEightHex(t *testing.T) {
	got := HashHostname("rosie-pi")
	if len(got) != 8 {
		t.Fatalf("len(%q) = %d, want 8", got, len(got))
	}
	for _, r := range got {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f')) {
			t.Fatalf("non-hex char %q in %q", r, got)
		}
	}
}

func TestHashHostname_DifferentInputs(t *testing.T) {
	if HashHostname("a") == HashHostname("b") {
		t.Fatal("collision on trivially-different hosts")
	}
}

func TestHashHostname_EmptyInput(t *testing.T) {
	// Empty hostname is a real possibility on Windows or in
	// chrooted containers where os.Hostname() may fail. The function
	// must return "" rather than the SHA-256 of empty (which would
	// inject a literal-looking hash where there's no host to match).
	if got := HashHostname(""); got != "" {
		t.Fatalf("HashHostname(\"\") = %q, want \"\"", got)
	}
}
