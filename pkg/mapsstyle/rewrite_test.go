package mapsstyle

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRewriteStyleJSON_GlyphsSpriteSources(t *testing.T) {
	in := []byte(`{
		"name": "Graywolf Maps",
		"glyphs": "https://maps.nw5w.com/style/roboto-glyphs/{fontstack}/{range}.pbf",
		"sprite": "https://maps.nw5w.com/style/americana/sprites/sprite",
		"sources": {
			"openmaptiles": {"type": "vector", "url": "https://maps.nw5w.com/tiles.json"},
			"dem": {"type": "raster-dem", "tiles": ["https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png"]}
		},
		"layers": [{"id":"x","type":"background"}]
	}`)
	out, err := RewriteStyleJSON(in, "/api/maps/style")
	if err != nil {
		t.Fatalf("RewriteStyleJSON: %v", err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(out, &parsed); err != nil {
		t.Fatalf("parse rewritten: %v", err)
	}
	if g, _ := parsed["glyphs"].(string); g != "/api/maps/style/roboto-glyphs/{fontstack}/{range}.pbf" {
		t.Errorf("glyphs not rewritten: %q", g)
	}
	if sp, _ := parsed["sprite"].(string); sp != "/api/maps/style/americana/sprites/sprite" {
		t.Errorf("sprite not rewritten: %q", sp)
	}
	srcs, _ := parsed["sources"].(map[string]any)
	omt, _ := srcs["openmaptiles"].(map[string]any)
	if u, _ := omt["url"].(string); u != "/api/maps/style/tiles.json" {
		t.Errorf("openmaptiles.url not rewritten: %q", u)
	}
	dem, _ := srcs["dem"].(map[string]any)
	demTiles, _ := dem["tiles"].([]any)
	demURL, _ := demTiles[0].(string)
	if !strings.Contains(demURL, "s3.amazonaws.com") {
		t.Errorf("dem URL should be untouched, got %q", demURL)
	}
	if n, _ := parsed["name"].(string); n != "Graywolf Maps" {
		t.Errorf("name not preserved: %q", n)
	}
}

func TestRewriteStyleJSON_NoMatchingFields(t *testing.T) {
	in := []byte(`{"name":"x","layers":[]}`)
	out, err := RewriteStyleJSON(in, "/api/maps/style")
	if err != nil {
		t.Fatalf("RewriteStyleJSON: %v", err)
	}
	if !strings.Contains(string(out), `"name":"x"`) {
		t.Errorf("out: %s", out)
	}
}

func TestRewriteStyleJSON_MalformedJSON(t *testing.T) {
	if _, err := RewriteStyleJSON([]byte(`{not json`), "/api/maps/style"); err == nil {
		t.Fatalf("expected error on malformed input")
	}
}
