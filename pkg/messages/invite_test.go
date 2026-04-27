package messages

import "testing"

// TestParseInvite covers the strict `!GW1 INVITE <TAC>` wire grammar.
// The goal of the table is to pin every rejection reason we care about
// (missing sigil, trailing noise, bad casing, out-of-range tactical) so
// a future regex edit that loosens one axis lights up here immediately.
func TestParseInvite(t *testing.T) {
	tests := []struct {
		name     string
		in       string
		wantTac  string
		wantOk   bool
	}{
		// ---- positive cases ------------------------------------------
		{name: "simple", in: "!GW1 INVITE TAC", wantTac: "TAC", wantOk: true},
		{name: "with_hyphen", in: "!GW1 INVITE TAC-NET", wantTac: "TAC-NET", wantOk: true},
		{name: "numeric_tac", in: "!GW1 INVITE NET1", wantTac: "NET1", wantOk: true},
		{name: "single_char_tac", in: "!GW1 INVITE A", wantTac: "A", wantOk: true},
		{name: "nine_char_tac_max", in: "!GW1 INVITE ABCDEFGHI", wantTac: "ABCDEFGHI", wantOk: true},
		{name: "hyphens_allowed_everywhere", in: "!GW1 INVITE A-B-C-D-E", wantTac: "A-B-C-D-E", wantOk: true},

		// ---- rejected: missing or wrong sigil ------------------------
		{name: "legacy_no_sigil", in: "INVITE TAC-NET", wantOk: false},
		{name: "wrong_sigil_version", in: "!GW2 INVITE TAC", wantOk: false},
		{name: "missing_bang", in: "GW1 INVITE TAC", wantOk: false},
		{name: "empty", in: "", wantOk: false},

		// ---- rejected: casing ----------------------------------------
		{name: "lowercase_sigil", in: "!gw1 INVITE TAC", wantOk: false},
		{name: "lowercase_verb", in: "!GW1 invite TAC", wantOk: false},
		{name: "lowercase_tactical", in: "!GW1 INVITE tac", wantOk: false},
		{name: "mixed_case_tactical", in: "!GW1 INVITE Tac", wantOk: false},

		// ---- rejected: length bounds ---------------------------------
		{name: "empty_tactical", in: "!GW1 INVITE ", wantOk: false},
		{name: "ten_char_tactical", in: "!GW1 INVITE ABCDEFGHIJ", wantOk: false},

		// ---- rejected: invalid chars in tactical ---------------------
		{name: "underscore_in_tactical", in: "!GW1 INVITE TAC_NET", wantOk: false},
		{name: "space_in_tactical", in: "!GW1 INVITE TAC NET", wantOk: false},
		{name: "slash_in_tactical", in: "!GW1 INVITE TAC/1", wantOk: false},
		{name: "dot_in_tactical", in: "!GW1 INVITE TAC.NET", wantOk: false},

		// ---- rejected: trailing / leading / interior whitespace ------
		{name: "trailing_space", in: "!GW1 INVITE TAC ", wantOk: false},
		{name: "trailing_newline", in: "!GW1 INVITE TAC\n", wantOk: false},
		{name: "leading_space", in: " !GW1 INVITE TAC", wantOk: false},
		{name: "double_space_between_verb_and_tac", in: "!GW1 INVITE  TAC", wantOk: false},
		{name: "missing_space_between_verb_and_tac", in: "!GW1 INVITETAC", wantOk: false},

		// ---- rejected: trailing text / notes -------------------------
		{name: "trailing_note", in: "!GW1 INVITE TAC hello", wantOk: false},
		{name: "extra_verb", in: "!GW1 INVITE TAC INVITE TAC", wantOk: false},

		// ---- rejected: other verbs -----------------------------------
		{name: "leave_verb", in: "!GW1 LEAVE TAC", wantOk: false},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			gotTac, gotOk := ParseInvite(tt.in)
			if gotOk != tt.wantOk {
				t.Fatalf("ParseInvite(%q) ok = %v, want %v", tt.in, gotOk, tt.wantOk)
			}
			if gotTac != tt.wantTac {
				t.Fatalf("ParseInvite(%q) tactical = %q, want %q", tt.in, gotTac, tt.wantTac)
			}
		})
	}
}
