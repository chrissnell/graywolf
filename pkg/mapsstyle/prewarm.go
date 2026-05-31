package mapsstyle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
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

// SetPrewarmLimits overrides the defaults for PrewarmGlyphs. Tests use
// it to avoid running through 1000+ ranges per case.
func (c *Cache) SetPrewarmLimits(maxRange, stopAfterConsecutive404 int) {
	if maxRange > 0 {
		c.prewarmMaxRange = maxRange
	}
	if stopAfterConsecutive404 > 0 {
		c.prewarmStop404 = stopAfterConsecutive404
	}
}

// PrewarmGlyphs reads the cached style.json from disk, discovers
// fontstacks via `text-font` layer keys, and fetches every glyph range
// 0..prewarmMaxRange for each fontstack. Per fontstack, after
// prewarmStop404 consecutive 404s the walk bails on that stack.
//
// Serial by design: each range is fetched in order so the
// consecutive-404 counter advances correctly. The total wall-clock is
// bounded by the caller's context (the map-download piggyback uses a
// 5-minute deadline). Best-effort — returns the first genuine error,
// callers log and move on.
//
// Idempotent — files already on disk are skipped.
func (c *Cache) PrewarmGlyphs(ctx context.Context) error {
	stylePath := filepath.Join(c.cacheDir, "americana-roboto", "style.json")
	body, err := os.ReadFile(stylePath)
	if err != nil {
		return fmt.Errorf("read style.json for glyph discovery: %w", err)
	}
	fonts, err := discoverFontstacks(body)
	if err != nil {
		return fmt.Errorf("discover fontstacks: %w", err)
	}
	if len(fonts) == 0 {
		c.logger.Info("mapsstyle: no fontstacks in style.json; skipping glyph pre-warm")
		return nil
	}

	var fetched int64
	var firstErr error
	for _, fs := range fonts {
		consecutive404 := 0
		for r := 0; r <= c.prewarmMaxRange; r++ {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			rel := glyphRel(fs, r)
			// Skip if already on disk to keep the prewarm idempotent.
			if _, err := os.Stat(filepath.Join(c.cacheDir, filepath.FromSlash(rel))); err == nil {
				consecutive404 = 0
				continue
			}
			ok, ferr := c.prewarmOne(ctx, rel)
			if ferr != nil && firstErr == nil {
				firstErr = ferr
			}
			if ok {
				fetched++
				consecutive404 = 0
				continue
			}
			consecutive404++
			if consecutive404 >= c.prewarmStop404 {
				c.logger.Debug("mapsstyle: stopping glyph warm",
					"fontstack", fs, "after_range", r,
					"reason", "consecutive 404s")
				break
			}
		}
	}
	c.logger.Info("mapsstyle: glyph pre-warm complete",
		"fontstacks", len(fonts), "ranges_fetched", fetched)
	return firstErr
}

// prewarmOne fetches a single glyph asset and writes it to disk. Returns
// (true, nil) on success, (false, nil) on a 404 (expected during walking),
// or (false, err) on any other failure.
func (c *Cache) prewarmOne(ctx context.Context, rel string) (bool, error) {
	body, _, err := c.fetchUpstream(ctx, rel)
	if err != nil {
		// 404s are part of normal walking (we don't know the highest
		// valid range up front); they're not surfaced as errors.
		if strings.Contains(err.Error(), "HTTP 404") {
			return false, nil
		}
		return false, fmt.Errorf("prewarm %s: %w", rel, err)
	}
	if werr := c.writeDisk(rel, body); werr != nil {
		c.logger.Warn("mapsstyle: write glyph failed", "rel", rel, "err", werr)
		return false, nil
	}
	return true, nil
}
