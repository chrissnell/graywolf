package ax25

import "testing"

func TestParseVia(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    []string // rendered addresses; nil means direct
		wantErr bool
	}{
		{name: "empty is direct", in: "", want: nil},
		{name: "whitespace is direct", in: "  ,  ", want: nil},
		{name: "single", in: "WIDE1-1", want: []string{"WIDE1-1"}},
		{name: "multi with spaces", in: " WIDE1-1 , WIDE2-1 ", want: []string{"WIDE1-1", "WIDE2-1"}},
		{name: "explicit digi", in: "N0CALL-3", want: []string{"N0CALL-3"}},
		{name: "rejects repeated marker", in: "WIDE1-1*", wantErr: true},
		{name: "rejects bad ssid", in: "WIDE1-99", wantErr: true},
		{name: "rejects bad call", in: "wide1-1,!!!", wantErr: true},
		{name: "rejects too many hops", in: "A,B,C,D,E,F,G,H,I", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseVia(tc.in)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("ParseVia(%q) = %v, want error", tc.in, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseVia(%q): %v", tc.in, err)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("ParseVia(%q) len = %d, want %d (%v)", tc.in, len(got), len(tc.want), got)
			}
			for i, a := range got {
				if a.String() != tc.want[i] {
					t.Errorf("ParseVia(%q)[%d] = %q, want %q", tc.in, i, a.String(), tc.want[i])
				}
			}
		})
	}
}
