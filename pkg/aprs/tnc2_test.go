package aprs

import "testing"

func TestFormatTNC2(t *testing.T) {
	tests := []struct {
		name   string
		source string
		dest   string
		path   []string
		info   []byte
		want   string
	}{
		{
			name:   "originated message (TCPIP* convention)",
			source: "NW5W-13",
			dest:   "APGRWO",
			path:   []string{"TCPIP*"},
			info:   []byte(":NW5W-5   :TEST{015"),
			want:   "NW5W-13>APGRWO,TCPIP*::NW5W-5   :TEST{015",
		},
		{
			name:   "gated RF packet (qAR construct)",
			source: "K1ABC",
			dest:   "APRS",
			path:   []string{"WIDE1-1", "WIDE2-1", "qAR", "NW5W-13"},
			info:   []byte("!4012.34N/10500.56W>"),
			want:   "K1ABC>APRS,WIDE1-1,WIDE2-1,qAR,NW5W-13:!4012.34N/10500.56W>",
		},
		{
			name:   "no path",
			source: "K1ABC",
			dest:   "APRS",
			path:   nil,
			info:   []byte(">status"),
			want:   "K1ABC>APRS:>status",
		},
		{
			name:   "empty path elements skipped",
			source: "K1ABC",
			dest:   "APRS",
			path:   []string{"", "WIDE1-1", ""},
			info:   []byte("hello"),
			want:   "K1ABC>APRS,WIDE1-1:hello",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FormatTNC2(tc.source, tc.dest, tc.path, tc.info)
			if got != tc.want {
				t.Fatalf("FormatTNC2 = %q, want %q", got, tc.want)
			}
		})
	}
}
