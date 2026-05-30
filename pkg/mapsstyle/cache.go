// Package mapsstyle is a server-side pull-through cache for MapLibre
// style assets served by the graywolf-maps Worker. The browser hits
// /api/maps/style/{path}; on disk-miss, graywolf fetches from upstream
// once, writes the body to <TileCacheDir>/style/, and serves it. The
// next request comes off disk — including after a reboot, from a
// different browser, or from a LAN guest.
//
// No startup network: graywolf does NOT pre-fetch on boot. The first
// online browser request is the trigger. A new user with internet sees
// a working map immediately; a never-online user sees a broken map
// (which is the correct visible failure mode — they have no tiles
// either). See docs/wiki/system-topology.md for the offline maps story.
package mapsstyle

import (
	"fmt"
	"net/url"
	"path"
	"strings"
)

// cleanRelPath validates an inbound relative path against the allowed
// upstream surface and returns the cleaned form. Path traversal,
// absolute paths, and prefixes outside the allowed set are rejected.
//
// Allowed: `americana-roboto/style.json`,
// `americana/shields.json`, `americana/sprites/<any single segment>`,
// `roboto-glyphs/<fontstack>/<range>.pbf`, and the literal `tiles.json`.
func cleanRelPath(rel string) (string, bool) {
	if rel == "" || strings.HasPrefix(rel, "/") {
		return "", false
	}
	cleaned := path.Clean(rel)
	if cleaned != rel {
		return "", false
	}
	if strings.Contains(cleaned, "..") {
		return "", false
	}
	if cleaned == "tiles.json" {
		return cleaned, true
	}
	switch {
	case cleaned == "americana-roboto/style.json":
		return cleaned, true
	case cleaned == "americana/shields.json":
		return cleaned, true
	case strings.HasPrefix(cleaned, "americana/sprites/") && strings.Count(cleaned, "/") == 2:
		return cleaned, true
	case strings.HasPrefix(cleaned, "roboto-glyphs/") && strings.Count(cleaned, "/") == 2:
		return cleaned, true
	}
	return "", false
}

// upstreamURL maps a cleaned relative path to its absolute upstream
// URL. tiles.json lives at the worker root; everything else is under
// /style/. Fontstack directory names are URL-escaped path-segment-wise
// (space -> %20) since the worker is HTTP and disk uses literal spaces.
func upstreamURL(baseURL, rel string) (string, error) {
	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("parse baseURL: %w", err)
	}
	if rel == "tiles.json" {
		u.Path = "/tiles.json"
		return u.String(), nil
	}
	segs := strings.Split(rel, "/")
	escaped := make([]string, 0, len(segs)+1)
	escaped = append(escaped, "style")
	for _, s := range segs {
		escaped = append(escaped, url.PathEscape(s))
	}
	// Set both Path (unescaped) and RawPath (escaped) so url.String()
	// emits the pre-escaped form instead of double-escaping %20 -> %2520.
	u.Path = "/" + path.Join(append([]string{"style"}, segs...)...)
	u.RawPath = "/" + strings.Join(escaped, "/")
	return u.String(), nil
}

// contentTypeFor derives the Content-Type from the path extension.
// Unknown extensions fall back to application/octet-stream.
func contentTypeFor(rel string) string {
	switch path.Ext(rel) {
	case ".json":
		return "application/json"
	case ".png":
		return "image/png"
	case ".pbf":
		return "application/x-protobuf"
	}
	return "application/octet-stream"
}
