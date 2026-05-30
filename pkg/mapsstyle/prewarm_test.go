package mapsstyle

import (
	"sort"
	"testing"
)

func TestDiscoverFontstacks(t *testing.T) {
	style := []byte(`{
		"layers":[
			{"id":"a","layout":{"text-font":["Roboto Regular","Roboto Italic"]}},
			{"id":"b","layout":{"text-font":["Roboto Bold"]}},
			{"id":"c","layout":{}},
			{"id":"d","type":"background"}
		]
	}`)
	got, err := discoverFontstacks(style)
	if err != nil {
		t.Fatalf("discoverFontstacks: %v", err)
	}
	sort.Strings(got)
	want := []string{"Roboto Bold", "Roboto Italic", "Roboto Regular"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i, w := range want {
		if got[i] != w {
			t.Fatalf("got[%d]=%q want %q", i, got[i], w)
		}
	}
}

func TestDiscoverFontstacks_Malformed(t *testing.T) {
	if _, err := discoverFontstacks([]byte(`{not json`)); err == nil {
		t.Fatalf("expected error")
	}
}

func TestGlyphRel(t *testing.T) {
	if got := glyphRel("Roboto Regular", 0); got != "roboto-glyphs/Roboto Regular/0-255.pbf" {
		t.Errorf("range 0: %q", got)
	}
	if got := glyphRel("Roboto Bold", 1); got != "roboto-glyphs/Roboto Bold/256-511.pbf" {
		t.Errorf("range 1: %q", got)
	}
	if got := glyphRel("Roboto Regular", 255); got != "roboto-glyphs/Roboto Regular/65280-65535.pbf" {
		t.Errorf("range 255: %q", got)
	}
}
