package mapsstyle

import (
	"encoding/json"
	"fmt"
)

// discoverFontstacks parses style.json bytes and returns the union of
// all `text-font` arrays referenced by layers. Used by PrewarmGlyphs to
// decide which fontstack directories to populate.
func discoverFontstacks(styleJSON []byte) ([]string, error) {
	var doc struct {
		Layers []struct {
			Layout map[string]any `json:"layout"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(styleJSON, &doc); err != nil {
		return nil, fmt.Errorf("parse style.json: %w", err)
	}
	set := map[string]struct{}{}
	for _, lyr := range doc.Layers {
		f, ok := lyr.Layout["text-font"]
		if !ok {
			continue
		}
		arr, ok := f.([]any)
		if !ok {
			continue
		}
		for _, x := range arr {
			if s, ok := x.(string); ok {
				set[s] = struct{}{}
			}
		}
	}
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	return out, nil
}

// glyphRel returns the disk-relative path for a fontstack + range tuple.
// Spaces in the fontstack stay literal on disk; the upstream URL builder
// re-encodes them.
func glyphRel(fontstack string, rangeIdx int) string {
	lo := rangeIdx * 256
	hi := lo + 255
	return fmt.Sprintf("roboto-glyphs/%s/%d-%d.pbf", fontstack, lo, hi)
}
