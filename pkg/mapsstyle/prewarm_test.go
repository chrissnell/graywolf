package mapsstyle

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"sync/atomic"
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

func TestPrewarmGlyphs_HappyPath(t *testing.T) {
	var ranges atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// EscapedPath preserves %20 so the case strings can be written
		// in their on-the-wire form (matching what fetchUpstream sends).
		switch r.URL.EscapedPath() {
		case "/style/americana-roboto/style.json":
			w.Write([]byte(`{"layers":[{"id":"a","layout":{"text-font":["Roboto Regular"]}}]}`))
			return
		case "/style/roboto-glyphs/Roboto%20Regular/0-255.pbf",
			"/style/roboto-glyphs/Roboto%20Regular/256-511.pbf":
			ranges.Add(1)
			w.Write([]byte("pbf-bytes"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	dir := t.TempDir()
	c := New(Config{BaseURL: srv.URL, CacheDir: dir, LocalPrefix: "/api/maps/style"})
	// Style.json must be on disk for fontstack discovery; trigger a Get.
	if _, _, err := c.Get(context.Background(), "americana-roboto/style.json"); err != nil {
		t.Fatalf("seed style.json: %v", err)
	}
	c.SetPrewarmLimits(2, 1) // maxRange=2, stopAfter=1 consecutive 404
	if err := c.PrewarmGlyphs(context.Background()); err != nil {
		t.Fatalf("PrewarmGlyphs: %v", err)
	}
	if ranges.Load() < 2 {
		t.Errorf("expected at least 2 glyph fetches, got %d", ranges.Load())
	}
	if _, err := os.Stat(filepath.Join(dir, "roboto-glyphs", "Roboto Regular", "0-255.pbf")); err != nil {
		t.Errorf("expected 0-255.pbf on disk: %v", err)
	}
}

func TestPrewarmGlyphs_NoStyleOnDisk(t *testing.T) {
	c := New(Config{BaseURL: "http://127.0.0.1:1", CacheDir: t.TempDir()})
	if err := c.PrewarmGlyphs(context.Background()); err == nil {
		t.Fatalf("expected error when style.json not cached")
	}
}
