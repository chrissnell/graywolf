package actions

import (
	"reflect"
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
