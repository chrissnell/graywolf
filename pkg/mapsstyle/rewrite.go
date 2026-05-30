package mapsstyle

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RewriteStyleJSON rewrites absolute maps.nw5w.com URLs inside a style.json
// payload to point at the local proxy prefix (e.g. "/api/maps/style").
// Rewritten fields:
//   - top-level "glyphs" (URL template)
//   - top-level "sprite" (URL base)
//   - sources.*.url that points at maps.nw5w.com/tiles.json
//
// Other source types (raster-dem on s3.amazonaws.com) are left untouched.
func RewriteStyleJSON(in []byte, localPrefix string) ([]byte, error) {
	localPrefix = strings.TrimRight(localPrefix, "/")
	var doc map[string]any
	if err := json.Unmarshal(in, &doc); err != nil {
		return nil, fmt.Errorf("parse style.json: %w", err)
	}
	if g, ok := doc["glyphs"].(string); ok {
		doc["glyphs"] = rewriteOne(g, localPrefix)
	}
	if s, ok := doc["sprite"].(string); ok {
		doc["sprite"] = rewriteOne(s, localPrefix)
	}
	if srcs, ok := doc["sources"].(map[string]any); ok {
		for _, raw := range srcs {
			src, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			if u, ok := src["url"].(string); ok {
				src["url"] = rewriteOne(u, localPrefix)
			}
		}
	}
	return json.Marshal(doc)
}

// rewriteOne rewrites a single absolute maps.nw5w.com URL to the local
// prefix. Strings that don't start with the known upstream are returned
// unchanged.
func rewriteOne(u, localPrefix string) string {
	const stylePrefix = "https://maps.nw5w.com/style/"
	const tilesJSON = "https://maps.nw5w.com/tiles.json"
	if u == tilesJSON {
		return localPrefix + "/tiles.json"
	}
	if strings.HasPrefix(u, stylePrefix) {
		return localPrefix + "/" + strings.TrimPrefix(u, stylePrefix)
	}
	return u
}
