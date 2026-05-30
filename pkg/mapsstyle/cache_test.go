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

func TestUpstreamURL(t *testing.T) {
	cases := []struct {
		rel  string
		want string
	}{
		{"americana-roboto/style.json", "https://maps.nw5w.com/style/americana-roboto/style.json"},
		{"americana/shields.json", "https://maps.nw5w.com/style/americana/shields.json"},
		{"americana/sprites/sprite.png", "https://maps.nw5w.com/style/americana/sprites/sprite.png"},
		{"roboto-glyphs/Roboto Regular/0-255.pbf", "https://maps.nw5w.com/style/roboto-glyphs/Roboto%20Regular/0-255.pbf"},
		{"tiles.json", "https://maps.nw5w.com/tiles.json"},
	}
	for _, tc := range cases {
		got, err := upstreamURL("https://maps.nw5w.com", tc.rel)
		if err != nil {
			t.Errorf("upstreamURL(%q) err: %v", tc.rel, err)
			continue
		}
		if got != tc.want {
			t.Errorf("upstreamURL(%q) = %q want %q", tc.rel, got, tc.want)
		}
	}
}

func TestContentTypeFor(t *testing.T) {
	cases := map[string]string{
		"americana-roboto/style.json":        "application/json",
		"americana/shields.json":             "application/json",
		"americana/sprites/sprite.json":      "application/json",
		"americana/sprites/sprite.png":       "image/png",
		"americana/sprites/sprite@2x.png":    "image/png",
		"roboto-glyphs/Roboto Regular/0.pbf": "application/x-protobuf",
		"tiles.json":                         "application/json",
	}
	for in, want := range cases {
		if got := contentTypeFor(in); got != want {
			t.Errorf("contentTypeFor(%q) = %q want %q", in, got, want)
		}
	}
}
