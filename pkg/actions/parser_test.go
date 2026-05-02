package actions

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseOTPPresent(t *testing.T) {
	got, err := Parse("@@482910#TurnOnGarageLights room=garage state=on")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := &ParsedInvocation{
		OTPDigits: "482910",
		Action:    "TurnOnGarageLights",
		Args: []KeyValue{
			{Key: "room", Value: "garage"},
			{Key: "state", Value: "on"},
		},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("\n got: %+v\nwant: %+v", got, want)
	}
}

func TestParseNoOTP(t *testing.T) {
	got, err := Parse("@@#Weather sta=KSFO")
	if err != nil {
		t.Fatal(err)
	}
	if got.OTPDigits != "" {
		t.Fatalf("expected empty OTP, got %q", got.OTPDigits)
	}
	if got.Action != "Weather" || len(got.Args) != 1 {
		t.Fatalf("bad parse: %+v", got)
	}
}

func TestParseRejectsMissingPrefix(t *testing.T) {
	if _, err := Parse("482910#Foo"); err == nil {
		t.Fatal("expected error: missing @@")
	}
}

func TestParseRejectsMissingHash(t *testing.T) {
	if _, err := Parse("@@482910NoHash"); err == nil {
		t.Fatal("expected error: missing #")
	}
}

func TestParseRejectsEmptyAction(t *testing.T) {
	if _, err := Parse("@@482910# room=garage"); err == nil {
		t.Fatal("expected error: empty action name")
	}
}

func TestParseRejectsBadOTPDigits(t *testing.T) {
	if _, err := Parse("@@4829AB#Foo"); err == nil {
		t.Fatal("expected error: non-digit OTP")
	}
	if _, err := Parse("@@1234567#Foo"); err == nil {
		t.Fatal("expected error: OTP not 6 digits")
	}
}

func TestParseKeyOnlyTokenIsArgError(t *testing.T) {
	if _, err := Parse("@@#Foo bareword"); err == nil {
		t.Fatal("expected error on bareword arg")
	}
}

func TestParseRejectsNonASCIIOTPDigits(t *testing.T) {
	// Devanagari digits encode to multi-byte UTF-8 that unicode.IsDigit
	// would otherwise accept; spec mandates ASCII digits only.
	if _, err := Parse("@@०१२३४५#Foo"); err == nil {
		t.Fatal("expected error: non-ASCII OTP digits")
	}
}

func TestParseRejectsInvalidActionNameChars(t *testing.T) {
	cases := []string{
		"@@#Foo$bar",      // dollar sign
		"@@#Foo!",         // bang
		"@@#Foo\x00bar",   // NUL byte
		"@@#Foo\tbar",     // tab inside name (no space split)
		"@@#Foo+bar k=v",  // plus
		"@@#Foo bar=value k=v", // OK case for control - note Foo passes
	}
	// Negative cases (all but the last should fail).
	for _, c := range cases[:len(cases)-1] {
		if _, err := Parse(c); err == nil {
			t.Errorf("expected reject for %q", c)
		}
	}
	// Last is the positive control.
	if _, err := Parse(cases[len(cases)-1]); err != nil {
		t.Errorf("control case unexpectedly rejected: %v", err)
	}
}

func TestParseRejectsOversizeActionName(t *testing.T) {
	long := strings.Repeat("A", MaxActionNameLen+1)
	if _, err := Parse("@@#" + long); err == nil {
		t.Fatalf("expected reject of %d-char action name", len(long))
	}
	maxOK := strings.Repeat("A", MaxActionNameLen)
	if _, err := Parse("@@#" + maxOK); err != nil {
		t.Fatalf("max-length action name unexpectedly rejected: %v", err)
	}
}

func TestParseEmptyArgValueAllowed(t *testing.T) {
	// Pins the parser/sanitizer divide: parser accepts empty values;
	// Sanitize (Phase C) is the layer that enforces minlen.
	got, err := Parse("@@#Foo k=")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(got.Args) != 1 || got.Args[0].Key != "k" || got.Args[0].Value != "" {
		t.Fatalf("expected single empty-value arg, got %+v", got.Args)
	}
}

func TestParseDuplicateKeysPreserved(t *testing.T) {
	// Plan note says "last wins per APRS convention"; the parser itself
	// preserves both entries in order so audit logs see what arrived.
	// The runner/sanitizer is responsible for last-wins collapsing.
	got, err := Parse("@@#Foo k=a k=b")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	want := []KeyValue{{Key: "k", Value: "a"}, {Key: "k", Value: "b"}}
	if !reflect.DeepEqual(got.Args, want) {
		t.Fatalf("got %+v, want %+v", got.Args, want)
	}
}
