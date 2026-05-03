package remoteactions

import "testing"

func TestValidateBase32Secret(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"JBSWY3DPEHPK3PXP", true},
		{"jbswy3dpehpk3pxp", true},     // lowercase accepted, uppercased on store
		{"JBSWY3DPEHPK3PXP====", true}, // padding tolerated
		{"JBSWY 3DPEH PK3PX P", true},  // whitespace tolerated
		{"", false},
		{"!!!!!!!!", false},
		{"JBSW", false}, // too short for any sane TOTP
	}
	for _, tc := range cases {
		got := ValidateBase32Secret(tc.in)
		if (got == nil) != tc.want {
			t.Fatalf("ValidateBase32Secret(%q) err=%v want valid=%v", tc.in, got, tc.want)
		}
	}
}

func TestNormalizeBase32Secret(t *testing.T) {
	got, err := NormalizeBase32Secret(" jbswy 3dpehpk3pxp ")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if got != "JBSWY3DPEHPK3PXP" {
		t.Fatalf("got %q", got)
	}
}

func TestValidateTargetCall(t *testing.T) {
	cases := []struct {
		in    string
		want  string
		valid bool
	}{
		{"kk7xyz-9", "KK7XYZ-9", true},
		{"NW5W", "NW5W", true},
		{"", "", false},
		{"toolongcall-99", "", false},
		{"BAD/CHAR", "", false},
	}
	for _, tc := range cases {
		got, err := NormalizeTargetCall(tc.in)
		if (err == nil) != tc.valid {
			t.Fatalf("NormalizeTargetCall(%q) err=%v want valid=%v", tc.in, err, tc.valid)
		}
		if tc.valid && got != tc.want {
			t.Fatalf("NormalizeTargetCall(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
