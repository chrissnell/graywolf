package mapsstyle

import "testing"

func TestCleanRelPath(t *testing.T) {
	cases := []struct {
		in      string
		wantOK  bool
		wantOut string
	}{
		{"americana-roboto/style.json", true, "americana-roboto/style.json"},
		{"americana/shields.json", true, "americana/shields.json"},
		{"americana/sprites/sprite.json", true, "americana/sprites/sprite.json"},
		{"americana/sprites/sprite.png", true, "americana/sprites/sprite.png"},
		{"americana/sprites/sprite@2x.png", true, "americana/sprites/sprite@2x.png"},
		{"roboto-glyphs/Roboto Regular/0-255.pbf", true, "roboto-glyphs/Roboto Regular/0-255.pbf"},
		{"tiles.json", true, "tiles.json"},
		{"../etc/passwd", false, ""},
		{"americana-roboto/../../../etc/passwd", false, ""},
		{"/absolute", false, ""},
		{"", false, ""},
		{"unknown-prefix/x.json", false, ""},
		{"americana-roboto/sub/style.json", false, ""}, // nested under americana-roboto not allowed
	}
	for _, tc := range cases {
		got, ok := cleanRelPath(tc.in)
		if ok != tc.wantOK {
			t.Errorf("cleanRelPath(%q) ok=%v want %v", tc.in, ok, tc.wantOK)
			continue
		}
		if ok && got != tc.wantOut {
			t.Errorf("cleanRelPath(%q) = %q want %q", tc.in, got, tc.wantOut)
		}
	}
}
