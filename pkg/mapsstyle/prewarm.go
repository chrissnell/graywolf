package mapsstyle

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
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
// Best-effort: returns the first error encountered after all in-flight
// fetches complete; callers (the map-download piggyback) should log
// and move on.
//
// Idempotent — files already on disk are skipped by Get's disk-first
// fast path.
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

	sem := make(chan struct{}, c.prewarmConcurrency)
	var wg sync.WaitGroup
	var fetched atomic.Int64
	var firstErr atomic.Pointer[error]
	for _, fs := range fonts {
		fs := fs
		consecutive404 := 0
		for r := 0; r <= c.prewarmMaxRange; r++ {
			if ctx.Err() != nil {
				wg.Wait()
				return ctx.Err()
			}
			rel := glyphRel(fs, r)
			// Skip if already on disk to keep the prewarm idempotent.
			if _, err := os.Stat(filepath.Join(c.cacheDir, filepath.FromSlash(rel))); err == nil {
				consecutive404 = 0
				continue
			}
			select {
			case <-ctx.Done():
				wg.Wait()
				return ctx.Err()
			case sem <- struct{}{}:
			}
			wg.Add(1)
			ok := c.prewarmOne(ctx, rel, sem, &wg, &fetched, &firstErr)
			if ok {
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
	wg.Wait()
	c.logger.Info("mapsstyle: glyph pre-warm complete",
		"fontstacks", len(fonts), "ranges_fetched", fetched.Load())
	if perr := firstErr.Load(); perr != nil {
		return *perr
	}
	return nil
}

// prewarmOne fetches a single glyph asset and writes it to disk. Returns
// true on success (HTTP 200, body written). False on any failure
// (404, network, write); the first error is captured in firstErr.
//
// Synchronous: the caller waits for the result so the consecutive-404
// counter is updated correctly. The sem and wg accounting still apply
// for symmetry with the goroutine-pool pattern, but the actual fetch
// runs inline.
func (c *Cache) prewarmOne(
	ctx context.Context,
	rel string,
	sem chan struct{},
	wg *sync.WaitGroup,
	fetched *atomic.Int64,
	firstErr *atomic.Pointer[error],
) bool {
	defer wg.Done()
	defer func() { <-sem }()
	body, _, err := c.fetchUpstream(ctx, rel)
	if err != nil {
		// 404s are part of normal walking (we don't know the highest
		// valid range up front); only capture genuine errors.
		if !strings.Contains(err.Error(), "HTTP 404") && firstErr.Load() == nil {
			perr := fmt.Errorf("prewarm %s: %w", rel, err)
			firstErr.CompareAndSwap(nil, &perr)
		}
		return false
	}
	if werr := c.writeDisk(rel, body); werr != nil {
		c.logger.Warn("mapsstyle: write glyph failed", "rel", rel, "err", werr)
		return false
	}
	fetched.Add(1)
	return true
}
