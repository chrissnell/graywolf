# Downloadable World Map Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a single low-zoom (z1–7, ~300 MB) worldwide basemap that operators can download for offline use anywhere on Earth, complementing the existing per-state/country/province downloads (GH #209).

**Architecture:** The basemap is already generated and served worldwide as OpenMapTiles/Americana vector tiles from the `graywolf-maps` Cloudflare Worker. This plan adds one extra PMTiles archive — a z0–7 slice of the planet — to that pipeline (`graywolf-maps` repo, Phase A), then extends the graywolf client (Phase B) so the catalog, slug grammar, downloader, offline render path, and region-picker UI all understand a new `world` slug. Two client behaviours are new: the world archive's globe-spanning bbox must rank **below** higher-detail regional archives in the federated tile dispatcher, and the archive's max zoom (7) must be threaded through so MapLibre overzooms the z7 tile instead of requesting (and failing) z8+ offline.

**Tech Stack:** Phase A — Planetiler (Java), PMTiles, Cloudflare R2 + Workers (wrangler), Node manifest generator. Phase B — Go 1.22+ (`http.ServeMux` wildcard routes, GORM/SQLite), Svelte 5 / MapLibre GL JS v5, `pmtiles` JS library.

**Repos:**
- `graywolf-maps` (`~/dev/graywolf-maps`, private — **not** checked out in this workspace): tile generation, R2 sync, manifest publishing, origin Worker. Phase A.
- `graywolf` (this repo, https://github.com/chrissnell/graywolf): the client. Phase B.

---

## Design constraints (recorded; do not violate)

1. **Reuse the existing pipeline.** The world archive is a maxzoom-capped Planetiler run of the same planet data already built; no new schema, style, or auth path.
2. **One archive, ~a few hundred MiB.** Target z1–7 ≈ 300 MB (per GH #209). z0 is one tile and harmless to include; the slug stays `world`.
3. **Regional downloads always win where they overlap.** A user with `state/colorado` AND `world` must render Colorado at full z14 detail, not the coarse world layer.
4. **Offline-safe render path.** The render path reads `/api/maps/local-bounds` (the SQLite snapshot), never the remote catalog — same invariant as today (`pkg/webapi/local_bounds.go`).
5. **No new auth/registration.** The world archive is served behind the same bearer-token Worker gate as every other download.
6. **CN/RU forbidden list is irrelevant here** — `world` is a single global archive, not a country.

---

## Slug & route contract (the seam between the two repos)

Agreed shape both repos implement against:

| Concept | Value |
|---|---|
| Namespaced slug | `world` (single segment, no `/`) |
| Worker download route | `GET /download/world.pmtiles` (bearer-token gated, edge-cached) |
| On-disk client path | `<TileCacheDir>/world.pmtiles` (from `PathFor`, already correct) |
| Manifest field | top-level `"world": { name, sizeBytes, sha256, bbox, maxZoom }` |
| World bbox | `[-180, -85.0511, 180, 85.0511]` (Web Mercator lat clamp) |
| World maxZoom | `7` |

---

## File Structure

**Phase A — `graywolf-maps` (separate repo):**
- Modify: the Planetiler build script/Makefile target — add a `world.pmtiles` build at `--maxzoom=7`.
- Modify: the manifest generator — emit the top-level `world` object with `maxZoom`.
- Modify: the R2 sync step — upload `world.pmtiles`.
- Modify: the Worker router — add `GET /download/world.pmtiles`.

**Phase B — `graywolf` (this repo):**
- `pkg/mapsslug/slug.go` + `slug_test.go` — accept `world` in the grammar.
- `pkg/mapscatalog/catalog.go` + `catalog_test.go` — `World *WorldMap` field, index, `HasSlug`.
- `pkg/mapscache/manager.go` + `manager_test.go` — `urlForSlug` world case; thread `maxZoom` into `Start`.
- `pkg/configstore/models.go` — `MaxZoom` column on `MapsDownload`.
- `pkg/configstore/seed_downloads.go` + test — persist `max_zoom`.
- `pkg/webapi/downloads.go` + test — `lookupCatalogBBox`/`lookupCatalogEntry` world case; pass maxZoom to `Start`.
- `pkg/webapi/local_bounds.go` + `pkg/webapi/dto/*` — return `maxZoom` per slug.
- `web/src/lib/maps/local-bounds-store.svelte.js` — expose `maxZoomBySlug`.
- `web/src/lib/map/sources/gw-federated-protocol.js` + (new) `gw-federated-protocol.test.js` — area-ranked dispatch + maxZoom-aware overzoom.
- `web/src/lib/map/maplibre-map.svelte` — dynamic source maxzoom when world-only.
- `web/src/lib/maps/catalog-tree.js` + test — surface the `world` entry to the picker.
- `web/src/lib/maps/region-picker.svelte` — render the World row.
- Regen artifacts: `pkg/webapi/docs/{docs.go,swagger.json,swagger.yaml}`, `web/src/api/generated/*`.
- Docs: `docs/wiki/system-topology.md`, `docs/handbook/maps.html`.

---

## Architecture decisions baked into this plan

1. **`world` is a single optional catalog object**, not a fourth array. It carries a `maxZoom` field the others lack (regions are full z14). `HasSlug`/`indexSlugs` add the literal `"world"` when present.
2. **maxZoom is snapshotted at download time** into `maps_downloads.max_zoom` (mirroring how bbox is snapshotted), so the offline render path knows the cap without consulting the catalog. Regional rows store `0`, meaning "no cap / upstream max".
3. **Dispatch ranks candidates by bbox area, ascending.** Smaller bbox = more specific = tried first. The world bbox (whole globe) is therefore always last, satisfying constraint 3 without hard-coding the slug name. The first archive that actually holds the tile wins; network is the final fallback.
4. **Overzoom is handled by setting the MapLibre source `maxzoom`.** When the *only* installed coverage is the world archive (no regional downloads), the gw-tile vector source is registered with `maxzoom: 7` so MapLibre reuses the z7 tile at higher screen zooms instead of requesting z8+ (which would miss offline → blank). When any regional (full-detail) download is present, the source keeps the upstream max (14) so detail is available there; world-only areas at high zoom rely on MapLibre's parent-tile retention. This is the documented, acceptable limitation.

---

# Phase A — graywolf-maps (tile generation + serving)

> This repo is not checked out in this workspace and is not on public GitHub. The steps below are at build/deploy granularity with concrete commands; adapt paths to the repo's actual layout. Do these first — Phase B's integration test needs the manifest to advertise `world` and the Worker to serve `/download/world.pmtiles`.

### Task A1: Generate the z0–7 world PMTiles archive

**Files:**
- Modify: the Planetiler invocation (Makefile target or build script, e.g. `Makefile` / `scripts/build-tiles.sh`).

- [ ] **Step 1: Add a world build target**

Add a target that runs Planetiler against the same planet input used for the full basemap, capped at zoom 7:

```bash
java -Xmx12g -jar planetiler.jar \
  --download \
  --area=planet \
  --minzoom=0 --maxzoom=7 \
  --output=build/world.pmtiles
```

A maxzoom-7 planet build is light (skips the explosive z13–14 data) — minutes-to-tens-of-minutes on a 16 GB box. Expected output ~250–400 MB.

- [ ] **Step 2: Verify size and zoom range**

Run:

```bash
pmtiles show build/world.pmtiles
```

Expected: `max zoom: 7`, `min zoom: 0`, bounds ≈ `-180,-85.05,180,85.05`, archive size a few hundred MB. If it exceeds ~450 MB, drop to `--maxzoom=6` and re-verify (constraint 2).

- [ ] **Step 3: Commit**

```bash
git add Makefile scripts/build-tiles.sh
git commit -m "build: generate z0-7 world.pmtiles archive"
```

### Task A2: Publish `world` in the manifest

**Files:**
- Modify: the manifest generator (e.g. `scripts/gen-manifest.js` or Go equivalent).

- [ ] **Step 1: Compute the world entry**

After building `world.pmtiles`, compute its size, SHA-256, and read its bbox/maxzoom from the PMTiles header, then add a top-level `world` object to `manifest.json`:

```jsonc
{
  "schemaVersion": 1,
  "generatedAt": "2026-06-13T00:00:00Z",
  "world": {
    "name": "World (low detail)",
    "sizeBytes": 314572800,
    "sha256": "<sha256 of world.pmtiles>",
    "bbox": [-180, -85.0511, 180, 85.0511],
    "maxZoom": 7
  },
  "countries": [ /* unchanged */ ],
  "provinces": [ /* unchanged */ ],
  "states":    [ /* unchanged */ ]
}
```

`schemaVersion` stays `1`: the field is purely additive, and the graywolf client tolerates unknown/missing fields (`World` is a pointer, omitted when absent).

- [ ] **Step 2: Validate the emitted manifest**

Run the generator and confirm:

```bash
node scripts/gen-manifest.js && jq '.world' build/manifest.json
```

Expected: the `world` object with the five fields, non-zero `sizeBytes`, `maxZoom: 7`.

- [ ] **Step 3: Commit**

```bash
git add scripts/gen-manifest.js
git commit -m "feat: advertise world archive in manifest"
```

### Task A3: Sync the archive to R2 and serve it from the Worker

**Files:**
- Modify: the R2 sync step (wrangler/rclone script).
- Modify: the Worker router (e.g. `src/worker.ts` / `src/index.js`).

- [ ] **Step 1: Upload world.pmtiles to R2**

```bash
wrangler r2 object put graywolf-maps/download/world.pmtiles \
  --file build/world.pmtiles
```

(Match the existing bucket name and `download/` key prefix used by the other archives.)

- [ ] **Step 2: Add the Worker download route**

In the Worker's router, handle `GET /download/world.pmtiles` exactly like the existing `/download/state/<slug>.pmtiles` route: validate the bearer token, fetch the R2 object, stream it back, and set the same edge-cache headers with the `?t=<token>` stripped from the cache key. If routes are matched by regex, extend the download matcher to also accept the literal `world.pmtiles`. No new auth logic.

- [ ] **Step 3: Deploy and smoke-test**

```bash
wrangler deploy
curl -sI "https://maps.nw5w.com/download/world.pmtiles?t=$TOKEN" | head
curl -sf "https://maps.nw5w.com/manifest.json?t=$TOKEN" | jq '.world.maxZoom'
```

Expected: `200 OK` with `content-type: application/octet-stream` and a `content-length` of a few hundred MB on the download; `7` from the manifest.

- [ ] **Step 4: Commit**

```bash
git add src/worker.ts scripts/sync-r2.sh
git commit -m "feat: serve /download/world.pmtiles from R2"
```

---

# Phase B — graywolf client

## Task B1: Accept `world` in the slug grammar

**Files:**
- Modify: `pkg/mapsslug/slug.go`
- Test: `pkg/mapsslug/slug_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/mapsslug/slug_test.go`:

```go
func TestParseWorld(t *testing.T) {
	kind, a, b, ok := Parse("world")
	if !ok || kind != "world" || a != "" || b != "" {
		t.Fatalf("Parse(\"world\") = (%q,%q,%q,%v), want (\"world\",\"\",\"\",true)", kind, a, b, ok)
	}
	for _, bad := range []string{"world/", "world/base", "world/x/y", "World", "worldx"} {
		if _, _, _, ok := Parse(bad); ok {
			t.Errorf("Parse(%q) = ok, want rejected", bad)
		}
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/mapsslug/ -run TestParseWorld -v`
Expected: FAIL — `Parse("world")` returns `ok=false`.

- [ ] **Step 3: Add the `world` case**

In `pkg/mapsslug/slug.go`, inside `Parse`'s `switch parts[0]`, add a case before the closing brace:

```go
	case "world":
		if len(parts) != 1 {
			return "", "", "", false
		}
		return "world", "", "", true
```

Also update the package doc grammar comment to list `world`:

```go
//	world                    (single global archive, z0-7)
```

- [ ] **Step 4: Run the test to confirm it passes**

Run: `go test ./pkg/mapsslug/ -v`
Expected: PASS (all cases, including the existing ones).

- [ ] **Step 5: Commit**

```bash
git add pkg/mapsslug/slug.go pkg/mapsslug/slug_test.go
git commit -m "feat(mapsslug): accept world slug"
```

## Task B2: Add the `world` entry to the catalog type

**Files:**
- Modify: `pkg/mapscatalog/catalog.go`
- Test: `pkg/mapscatalog/catalog_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/mapscatalog/catalog_test.go`:

```go
func TestHasSlugWorld(t *testing.T) {
	c := Catalog{World: &WorldMap{Name: "World", SizeBytes: 1, MaxZoom: 7}}
	c.indexSlugs()
	if !c.HasSlug("world") {
		t.Fatal("HasSlug(\"world\") = false, want true")
	}
	empty := Catalog{}
	empty.indexSlugs()
	if empty.HasSlug("world") {
		t.Fatal("HasSlug(\"world\") = true on catalog without world, want false")
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/mapscatalog/ -run TestHasSlugWorld -v`
Expected: FAIL — `undefined: WorldMap` and `Catalog has no field World`.

- [ ] **Step 3: Add the type, field, and index/lookup support**

In `pkg/mapscatalog/catalog.go`, add the type near the other entry structs:

```go
type WorldMap struct {
	Name      string      `json:"name"`
	SizeBytes int64       `json:"sizeBytes"`
	SHA256    string      `json:"sha256"`
	BBox      *[4]float64 `json:"bbox,omitempty"`
	MaxZoom   int         `json:"maxZoom"`
}
```

Add the field to `Catalog` (right after `States`):

```go
	World *WorldMap `json:"world,omitempty"`
```

In `indexSlugs`, after the provinces loop and before `c.slugIndex = idx`:

```go
	if c.World != nil {
		idx["world"] = struct{}{}
	}
```

In `HasSlug`'s linear fallback, before the final `return false`:

```go
	if c.World != nil && slug == "world" {
		return true
	}
```

- [ ] **Step 4: Run the test to confirm it passes**

Run: `go test ./pkg/mapscatalog/ -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapscatalog/catalog.go pkg/mapscatalog/catalog_test.go
git commit -m "feat(mapscatalog): add optional world entry"
```

## Task B3: Build the world download URL

**Files:**
- Modify: `pkg/mapscache/manager.go`
- Test: `pkg/mapscache/manager_test.go`

- [ ] **Step 1: Write the failing test**

Add to `pkg/mapscache/manager_test.go` (use the same construction the existing `urlForSlug`/`PathFor` tests use; if `urlForSlug` is unexported, this test lives in `package mapscache`):

```go
func TestURLForSlugWorld(t *testing.T) {
	m := &Manager{mapsBaseURL: "https://maps.nw5w.com"}
	got, err := m.urlForSlug("world", "")
	if err != nil {
		t.Fatalf("urlForSlug(world) error: %v", err)
	}
	const want = "https://maps.nw5w.com/download/world.pmtiles"
	if got != want {
		t.Fatalf("urlForSlug(world) = %q, want %q", got, want)
	}
	if p := m.PathFor("world"); !strings.HasSuffix(p, "world.pmtiles") {
		t.Fatalf("PathFor(world) = %q, want suffix world.pmtiles", p)
	}
}
```

(If the `Manager` base-URL field has a different name than `mapsBaseURL`, match the struct in `manager.go`; the existing `urlForSlug` reads it into `base`.)

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/mapscache/ -run TestURLForSlugWorld -v`
Expected: FAIL — `urlForSlug("world")` returns `invalid slug "world"` only if B1 not done; with B1 done it returns an error because the `switch kind` has no `world` case (falls through to the error path).

- [ ] **Step 3: Add the `world` case**

In `pkg/mapscache/manager.go`, inside `urlForSlug`'s `switch kind`, add:

```go
	case "world":
		raw = fmt.Sprintf("%s/download/world.pmtiles", base)
```

`PathFor("world")` already yields `<cache>/world.pmtiles` via `filepath.FromSlash` — no change needed there.

- [ ] **Step 4: Run the test to confirm it passes**

Run: `go test ./pkg/mapscache/ -run TestURLForSlugWorld -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/mapscache/manager.go pkg/mapscache/manager_test.go
git commit -m "feat(mapscache): world download URL"
```

## Task B4: Snapshot maxZoom into the downloads table

**Files:**
- Modify: `pkg/configstore/models.go`
- Modify: `pkg/configstore/seed_downloads.go`
- Test: `pkg/configstore/seed_downloads_test.go` (or the existing downloads store test file)

- [ ] **Step 1: Write the failing test**

Add to the downloads store test file (mirror the existing `UpsertMapsDownload` round-trip test):

```go
func TestUpsertMapsDownloadMaxZoom(t *testing.T) {
	s := newTestStore(t) // existing helper used by the other tests in this file
	ctx := context.Background()
	if err := s.UpsertMapsDownload(ctx, MapsDownload{
		Slug: "world", Status: "complete", MaxZoom: 7,
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetMapsDownload(ctx, "world")
	if err != nil {
		t.Fatal(err)
	}
	if got.MaxZoom != 7 {
		t.Fatalf("MaxZoom = %d, want 7", got.MaxZoom)
	}
}
```

(If the existing tests construct the store differently, reuse that exact setup helper instead of `newTestStore`.)

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/configstore/ -run TestUpsertMapsDownloadMaxZoom -v`
Expected: FAIL — `MapsDownload has no field MaxZoom`.

- [ ] **Step 3: Add the column and persist it**

In `pkg/configstore/models.go`, add to the `MapsDownload` struct:

```go
	MaxZoom int `gorm:"not null;default:0" json:"max_zoom"` // 0 = no cap (regional, full-detail); >0 = world archive max zoom
```

GORM's `AutoMigrate` (run at startup) adds the column to existing DBs with the `0` default — no manual migration file needed. Confirm `AutoMigrate(&MapsDownload{})` is already in the migration list (it is — `maps_downloads` exists today).

In `pkg/configstore/seed_downloads.go`, add `max_zoom` to the `cols` map in `UpsertMapsDownload`:

```go
	cols := map[string]any{
		"slug":             d.Slug,
		"status":           d.Status,
		"bytes_total":      d.BytesTotal,
		"bytes_downloaded": d.BytesDownloaded,
		"downloaded_at":    d.DownloadedAt,
		"error_message":    d.ErrorMessage,
		"max_zoom":         d.MaxZoom,
	}
```

`max_zoom` is written unconditionally (unlike `bbox`, which is guarded): status-transition upserts pass `MaxZoom: 0`, which is correct for every regional row and harmless until the world row is set at Start. The Start path (Task B5) writes the real value before any status transition.

- [ ] **Step 4: Run the test to confirm it passes**

Run: `go test ./pkg/configstore/ -run TestUpsertMapsDownloadMaxZoom -v`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add pkg/configstore/models.go pkg/configstore/seed_downloads.go pkg/configstore/seed_downloads_test.go
git commit -m "feat(configstore): persist max_zoom on downloads"
```

## Task B5: Thread maxZoom through the download Start path

**Files:**
- Modify: `pkg/mapscache/manager.go` (`Start` signature + the initial row write)
- Modify: `pkg/webapi/downloads.go` (`startDownload`, `lookupCatalogBBox` → also return maxZoom)
- Test: `pkg/webapi/downloads_test.go`

- [ ] **Step 1: Write the failing test**

In `pkg/webapi/downloads_test.go`, add a unit test for the catalog lookup helper (rename target — see Step 3):

```go
func TestLookupCatalogEntryWorld(t *testing.T) {
	c := mapscatalog.Catalog{World: &mapscatalog.WorldMap{
		BBox: &[4]float64{-180, -85.05, 180, 85.05}, MaxZoom: 7,
	}}
	bbox, maxZoom, found := lookupCatalogEntry(c, "world")
	if !found || maxZoom != 7 || bbox == nil {
		t.Fatalf("lookupCatalogEntry(world) = (%v,%d,%v), want (non-nil,7,true)", bbox, maxZoom, found)
	}
}
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/webapi/ -run TestLookupCatalogEntryWorld -v`
Expected: FAIL — `undefined: lookupCatalogEntry`.

- [ ] **Step 3: Extend the lookup to return maxZoom and handle world**

In `pkg/mapscache/manager.go`, change `Start` to accept a maxZoom:

```go
func (m *Manager) Start(ctx context.Context, slug string, bbox *[4]float64, maxZoom int) error {
```

and set it on the initial row written inside `Start` (the `MapsDownload{...}` literal that records `Slug`, `Status`, `BBox`):

```go
		MaxZoom: maxZoom,
```

In `pkg/webapi/downloads.go`, rename `lookupCatalogBBox` to `lookupCatalogEntry`, returning maxZoom too, and add the `world` case:

```go
// lookupCatalogEntry returns the bbox and maxZoom for a namespaced
// slug in the catalog, or (nil, 0, false) if the slug names no
// published archive. Regional entries have no zoom cap and report
// maxZoom 0; the world archive reports its real cap (e.g. 7).
func lookupCatalogEntry(c mapscatalog.Catalog, slug string) (*[4]float64, int, bool) {
	kind, a, b, ok := parseSlug(slug)
	if !ok {
		return nil, 0, false
	}
	switch kind {
	case "world":
		if c.World == nil {
			return nil, 0, false
		}
		return c.World.BBox, c.World.MaxZoom, true
	case "state":
		for _, st := range c.States {
			if st.Slug == a {
				return st.BBox, 0, true
			}
		}
	case "country":
		for _, x := range c.Countries {
			if x.ISO2 == a {
				return x.BBox, 0, true
			}
		}
	case "province":
		for _, p := range c.Provinces {
			if p.ISO2 == a && p.Slug == b {
				return p.BBox, 0, true
			}
		}
	}
	return nil, 0, false
}
```

(Preserve the exact existing branch bodies for state/country/province — only the return arity and the new `world` case change. Match the existing helper's `parseSlug` call.)

Update `startDownload` to use the new helper and pass maxZoom:

```go
	bbox, maxZoom, found := lookupCatalogEntry(cat, slug)
	if !found {
		badRequest(w, "unknown slug")
		return
	}
	if err := s.mapsCache.Start(r.Context(), slug, bbox, maxZoom); err != nil {
```

- [ ] **Step 4: Fix all other `Start` / `lookupCatalogBBox` callers**

Run: `grep -rn "lookupCatalogBBox\|\.Start(" pkg/ | grep -i "maps\|catalog\|cache"`
Update every remaining caller (e.g. any in `manager_test.go`, `downloads_test.go`, app wiring) to the new signatures. For regional/test calls pass `0` as maxZoom.

- [ ] **Step 5: Run the tests to confirm they pass**

Run: `go test ./pkg/webapi/ ./pkg/mapscache/ -v`
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add pkg/mapscache/manager.go pkg/webapi/downloads.go pkg/webapi/downloads_test.go
git commit -m "feat: snapshot world maxZoom at download start"
```

## Task B6: Return maxZoom from /api/maps/local-bounds

**Files:**
- Modify: `pkg/webapi/dto/` (the `LocalBounds` type)
- Modify: `pkg/webapi/local_bounds.go`
- Test: `pkg/webapi/local_bounds_test.go` (create if absent)

- [ ] **Step 1: Write the failing test**

Create/extend `pkg/webapi/local_bounds_test.go` to assert the handler emits maxZoom. Mirror the construction style of the other handler tests in `pkg/webapi`:

```go
func TestLocalBoundsIncludesMaxZoom(t *testing.T) {
	s := newTestServer(t) // existing webapi test helper
	ctx := context.Background()
	bbox := `[-180,-85.05,180,85.05]`
	must(t, s.store.UpsertMapsDownload(ctx, configstore.MapsDownload{
		Slug: "world", Status: "complete", MaxZoom: 7, BBox: &bbox,
	}))
	rec := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/api/maps/local-bounds", nil)
	s.getLocalBounds(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	var out map[string]struct {
		BBox    [4]float64 `json:"bbox"`
		MaxZoom int        `json:"maxZoom"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["world"].MaxZoom != 7 {
		t.Fatalf("world maxZoom = %d, want 7", out["world"].MaxZoom)
	}
}
```

(If the existing webapi tests use a different server constructor/helpers, reuse those exactly.)

- [ ] **Step 2: Run it to confirm it fails**

Run: `go test ./pkg/webapi/ -run TestLocalBoundsIncludesMaxZoom -v`
Expected: FAIL — the response is currently a bare `slug -> [4]float64` map, so `MaxZoom` is `0`.

- [ ] **Step 3: Change the DTO shape and handler**

Today `dto.LocalBounds` is `map[string]([4]float64)`. Change it to a per-slug object. In `pkg/webapi/dto/` (find the file defining `LocalBounds`):

```go
// LocalBoundsEntry is the render-path coverage for one downloaded slug.
type LocalBoundsEntry struct {
	BBox    [4]float64 `json:"bbox"`
	MaxZoom int        `json:"maxZoom"` // 0 = no cap (regional); >0 = world archive cap
}

// LocalBounds maps each completed download's slug to its coverage.
type LocalBounds map[string]LocalBoundsEntry
```

In `pkg/webapi/local_bounds.go`, populate the new shape:

```go
		out[row.Slug] = dto.LocalBoundsEntry{BBox: bbox, MaxZoom: row.MaxZoom}
```

- [ ] **Step 4: Run the test to confirm it passes**

Run: `go test ./pkg/webapi/ -run TestLocalBoundsIncludesMaxZoom -v`
Expected: PASS.

- [ ] **Step 5: Regenerate API docs + TS client**

Run: `make docs-check api-client-check` (or the repo's regen target — check `Makefile`). Commit the regenerated `pkg/webapi/docs/*` and `web/src/api/generated/*`.

- [ ] **Step 6: Commit**

```bash
git add pkg/webapi/dto pkg/webapi/local_bounds.go pkg/webapi/local_bounds_test.go pkg/webapi/docs web/src/api/generated
git commit -m "feat(webapi): local-bounds returns per-slug maxZoom"
```

## Task B7: Expose maxZoom in the local-bounds store

**Files:**
- Modify: `web/src/lib/maps/local-bounds-store.svelte.js`

- [ ] **Step 1: Update the store to the new payload shape**

The endpoint now returns `{ [slug]: { bbox: [w,s,e,n], maxZoom: N } }`. Update the `boundsBySlug` getter to read `.bbox`, and add a `maxZoomBySlug` getter:

```js
  return {
    load,
    refresh,
    get boundsBySlug() {
      const out = new Map();
      if (!raw) return out;
      for (const [slug, entry] of Object.entries(raw)) {
        const bbox = entry && entry.bbox;
        if (Array.isArray(bbox) && bbox.length === 4) out.set(slug, bbox);
      }
      return out;
    },
    get maxZoomBySlug() {
      const out = new Map();
      if (!raw) return out;
      for (const [slug, entry] of Object.entries(raw)) {
        if (entry && Number.isFinite(entry.maxZoom)) out.set(slug, entry.maxZoom);
      }
      return out;
    },
  };
```

- [ ] **Step 2: Verify the build typechecks**

Run: `cd web && npm run check` (or the repo's svelte-check target).
Expected: no new errors.

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/maps/local-bounds-store.svelte.js
git commit -m "feat(web): expose maxZoomBySlug from local-bounds store"
```

## Task B8: Area-ranked dispatch + maxZoom-aware miss in the federated protocol

**Files:**
- Modify: `web/src/lib/map/sources/gw-federated-protocol.js`
- Test: `web/src/lib/map/sources/gw-federated-protocol.test.js` (create)

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/map/sources/gw-federated-protocol.test.js`. The module currently has no exported pure helper for ranking; this test drives extracting one. Use the repo's JS test runner (check `web/package.json` `scripts.test` — likely `vitest`):

```js
import { describe, it, expect } from 'vitest';
import { rankCoveringSlugs } from './gw-federated-protocol.js';

describe('rankCoveringSlugs', () => {
  const tileBBox = [38, -105, 40, -103]; // [sLat, wLon, nLat, eLon] inside Colorado
  const bounds = new Map([
    ['world', [-180, -85, 180, 85]],
    ['state/colorado', [-109, 37, -102, 41]],
  ]);

  it('ranks the smaller (more specific) bbox first, world last', () => {
    const ranked = rankCoveringSlugs(tileBBox, new Set(['world', 'state/colorado']), bounds);
    expect(ranked).toEqual(['state/colorado', 'world']);
  });

  it('omits non-intersecting slugs', () => {
    const farTile = [-40, 100, -38, 102]; // southern hemisphere, far from CO
    const ranked = rankCoveringSlugs(farTile, new Set(['state/colorado', 'world']), bounds);
    expect(ranked).toEqual(['world']);
  });
});
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `cd web && npx vitest run src/lib/map/sources/gw-federated-protocol.test.js`
Expected: FAIL — `rankCoveringSlugs` is not exported.

- [ ] **Step 3: Implement area-ranked dispatch**

In `gw-federated-protocol.js`, replace `findCoveringSlug` with an exported `rankCoveringSlugs` that returns ALL intersecting slugs sorted by ascending bbox area (smallest/most-specific first; the global world bbox sorts last):

```js
// bboxArea: lon-span * lat-span of a [w, s, e, n] tuple. Used only to
// rank overlapping archives by specificity, so a plain rectangular
// area (no spherical correction) is sufficient and monotonic.
function bboxArea([w, s, e, n]) {
  return Math.max(0, e - w) * Math.max(0, n - s);
}

// rankCoveringSlugs returns every slug whose bbox intersects the tile,
// ordered smallest-bbox-first. A more specific regional archive is
// therefore tried before the globe-spanning world archive, so a user
// who has both renders the region at full detail.
export function rankCoveringSlugs(tileBBox, completedSlugs, boundsBySlug) {
  const hits = [];
  for (const slug of completedSlugs) {
    const bbox = boundsBySlug.get(slug);
    if (!bbox) continue;
    if (bboxIntersects(tileBBox, bbox)) hits.push({ slug, area: bboxArea(bbox) });
  }
  hits.sort((a, b) => a.area - b.area);
  return hits.map((h) => h.slug);
}
```

Rewrite the handler body to walk the ranked list, trying each archive until one yields the tile, falling back to the network only after all miss:

```js
      const tileBBox = tileToBBox(z, x, y);
      const completed = completedSlugsProvider();
      const bounds = boundsBySlugProvider();
      const maxZoomBySlug =
        typeof maxZoomBySlugProvider === 'function' ? maxZoomBySlugProvider() : new Map();

      const ranked = rankCoveringSlugs(tileBBox, completed, bounds);
      const fallback = () =>
        fetchOnline(z, x, y, abortController.signal).then((data) => ({ data }));

      if (ranked.length === 0) return fallback();

      const tryNext = (i) => {
        if (i >= ranked.length) return fallback();
        const slug = ranked[i];
        const cap = maxZoomBySlug.get(slug);
        // Skip archives that provably cannot hold this zoom (world
        // archive at z>cap). Avoids a guaranteed-miss range read; the
        // source maxzoom (Task B9) makes MapLibre overzoom instead.
        if (typeof cap === 'number' && cap > 0 && z > cap) return tryNext(i + 1);
        return getArchive(slug)
          .getZxy(z, x, y, abortController.signal)
          .then((tile) => {
            if (tile && tile.data) return { data: new Uint8Array(tile.data) };
            return tryNext(i + 1);
          })
          .catch(() => tryNext(i + 1));
      };
      return tryNext(0);
```

Add `maxZoomBySlugProvider` to the destructured params of `createFederatedProtocol`:

```js
export function createFederatedProtocol({
  completedSlugsProvider,
  boundsBySlugProvider,
  maxZoomBySlugProvider,
  fetchOnline,
}) {
```

Keep `tileToBBox` and `bboxIntersects` unchanged.

- [ ] **Step 4: Run the test to confirm it passes**

Run: `cd web && npx vitest run src/lib/map/sources/gw-federated-protocol.test.js`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/map/sources/gw-federated-protocol.js web/src/lib/map/sources/gw-federated-protocol.test.js
git commit -m "feat(web): area-ranked federated tile dispatch with maxZoom skip"
```

## Task B9: Wire maxZoom provider + dynamic source maxzoom in the map component

**Files:**
- Modify: `web/src/lib/map/maplibre-map.svelte`

- [ ] **Step 1: Pass the new provider into the protocol**

In the `createFederatedProtocol({ ... })` call (around line 49), add:

```js
      maxZoomBySlugProvider: () => localBoundsStore.maxZoomBySlug,
```

- [ ] **Step 2: Cap the gw-tile source maxzoom when coverage is world-only**

Where the style's vector source is rewritten to `gw-tile://{z}/{x}/{y}` (around line 101, `src.tiles = [...]`), set the source `maxzoom` to the world cap **only when** the world archive is the sole completed download — otherwise leave the upstream max so regional detail still loads:

```js
        src.tiles = ['gw-tile://{z}/{x}/{y}'];
        // When the user's ONLY offline coverage is the world archive,
        // cap the source so MapLibre overzooms the z7 tile instead of
        // requesting z8+ (which would miss offline -> blank). With any
        // full-detail regional download present, keep the upstream max
        // so that region renders at z14; world-only areas at high zoom
        // then rely on MapLibre's parent-tile retention.
        const completed = downloadsState.completed;
        const worldCap = localBoundsStore.maxZoomBySlug.get('world');
        if (completed.size === 1 && completed.has('world') && worldCap) {
          src.maxzoom = worldCap;
        }
```

- [ ] **Step 3: Manual verification (online + offline)**

Build and run the app (`make build` / the repo's dev target). Then:
1. Online, no downloads: map renders worldwide from the network as before (no regression).
2. Download `world`: confirm the request hits `/api/maps/downloads/world` → `202`, file lands at `<TileCacheDir>/world.pmtiles`, status reaches `complete`.
3. Disconnect network; reload: the world basemap renders globally at low zoom and overzooms (stays rendered, coarse) past z7.
4. With `world` + a state download, offline: the state area renders at full z14 detail; outside it, the coarse world layer shows. Confirm the state is not replaced by the world layer (area-ranking working).

- [ ] **Step 4: Commit**

```bash
git add web/src/lib/map/maplibre-map.svelte
git commit -m "feat(web): world-only source maxzoom + maxZoom provider"
```

## Task B10: Surface the World entry in the region picker

**Files:**
- Modify: `web/src/lib/maps/catalog-tree.js`
- Test: `web/src/lib/maps/catalog-tree.test.js` (extend if present, else create)
- Modify: `web/src/lib/maps/region-picker.svelte`

- [ ] **Step 1: Write the failing test**

Add to `catalog-tree.test.js` a test that the world entry is projected to a downloadable node:

```js
import { describe, it, expect } from 'vitest';
import { buildWorldNode } from './catalog-tree.js';

describe('buildWorldNode', () => {
  it('returns a node for a catalog with world', () => {
    const node = buildWorldNode({ world: { name: 'World (low detail)', sizeBytes: 314572800, bbox: [-180, -85, 180, 85], maxZoom: 7 } });
    expect(node).toEqual({ slug: 'world', name: 'World (low detail)', sizeBytes: 314572800 });
  });
  it('returns null when the catalog has no world', () => {
    expect(buildWorldNode({})).toBeNull();
  });
});
```

- [ ] **Step 2: Run it to confirm it fails**

Run: `cd web && npx vitest run src/lib/maps/catalog-tree.test.js`
Expected: FAIL — `buildWorldNode` is not exported.

- [ ] **Step 3: Implement `buildWorldNode`**

Add to `catalog-tree.js`:

```js
// buildWorldNode projects the optional top-level world archive into a
// flat picker node, or null when the catalog has no world entry. Kept
// separate from buildCountryTree because the world archive is not a
// country and has no children.
export function buildWorldNode(catalog) {
  const w = catalog && catalog.world;
  if (!w) return null;
  return { slug: 'world', name: w.name || 'World (low detail)', sizeBytes: w.sizeBytes || 0 };
}
```

- [ ] **Step 4: Run the test to confirm it passes**

Run: `cd web && npx vitest run src/lib/maps/catalog-tree.test.js`
Expected: PASS.

- [ ] **Step 5: Render the World row in the picker**

In `region-picker.svelte`, import the helper and compute the node:

```js
  import { buildCountryTree, /* keep existing */ } from './catalog-tree.js';
  import { buildWorldNode } from './catalog-tree.js';
```

Add a derived value alongside `filteredTree`:

```js
  const worldNode = $derived(catalogStore.catalog ? buildWorldNode(catalogStore.catalog) : null);
```

In the markup, render a dedicated World row above the country list (inside the same results container, after the loading/error guards and before `{#each filteredTree ...}`). Mirror the existing country-row button logic (`statusOf`, `downloadsState.start/remove/cancel`, `formatBytes`):

```svelte
        {#if worldNode}
          {@const wItem = statusOf(worldNode.slug)}
          <div class="region-row region-row-world">
            <span class="region-name">{worldNode.name}</span>
            {#if worldNode.sizeBytes > 0}
              <span class="region-size">{formatBytes(worldNode.sizeBytes)}</span>
            {/if}
            {#if !wItem}
              <Button onclick={() => downloadsState.start(worldNode.slug)}>Download</Button>
            {:else if wItem.state === 'complete'}
              <Button variant="danger" onclick={() => downloadsState.remove(worldNode.slug)}>Delete</Button>
            {:else}
              <Button variant="danger" onclick={() => downloadsState.cancel(worldNode.slug)}>Cancel</Button>
            {/if}
          </div>
        {/if}
```

(Match the exact class names / button structure used by the existing country rows in this file — the snippet above is the pattern, not necessarily the verbatim class set. Reuse `formatBytes` and `statusOf` already in scope.)

- [ ] **Step 6: Verify build + manual check**

Run: `cd web && npm run check && npx vitest run`
Then load the Offline maps picker: the "World (low detail) — ~300 MB" row appears at the top with a Download button, and downloading/deleting works end to end.

- [ ] **Step 7: Commit**

```bash
git add web/src/lib/maps/catalog-tree.js web/src/lib/maps/catalog-tree.test.js web/src/lib/maps/region-picker.svelte
git commit -m "feat(web): World row in the offline region picker"
```

## Task B11: Docs

**Files:**
- Modify: `docs/wiki/system-topology.md`
- Modify: `docs/handbook/maps.html`

- [ ] **Step 1: Update the wiki**

In `docs/wiki/system-topology.md`, in the offline-maps section, note the new `world` slug (single global z0–7 archive, ~300 MB, served from `/download/world.pmtiles`) and that `local-bounds` now carries a per-slug `maxZoom`. Add a sentence that the federated dispatcher ranks archives by bbox area so the world archive never shadows a regional download.

- [ ] **Step 2: Update the handbook**

In `docs/handbook/maps.html`, add a short paragraph under the offline-maps coverage description: a single low-detail world map (~300 MB) is available for portable/HF/cross-border use, renders worldwide at low zoom, and coexists with full-detail regional downloads (regions win where they overlap).

- [ ] **Step 3: Commit**

```bash
git add docs/wiki/system-topology.md docs/handbook/maps.html
git commit -m "docs: document downloadable world map"
```

## Task B12: Full verification pass

- [ ] **Step 1: Go tests + vet + lint**

Run: `go test ./... && go vet ./...`
Expected: PASS. (Then the repo's lint target if `make lint` exists.)

- [ ] **Step 2: Frontend tests + check + build**

Run: `cd web && npx vitest run && npm run check && npm run build`
Expected: PASS, clean build.

- [ ] **Step 3: Docs/API regen guard**

Run: `make docs-check api-client-check`
Expected: no diff (artifacts already committed in Task B6).

- [ ] **Step 4: End-to-end against the live Worker (requires Phase A deployed)**

With a registered device token: open the picker → Download World → confirm `complete` and a ~300 MB `world.pmtiles` on disk → go offline → confirm worldwide low-zoom render. This is the GH #209 acceptance criterion.

---

## Self-Review notes

- **Spec coverage:** GH #209 asks for a z1→"whatever fits in a few hundred MiB" world map ≈ size of existing downloads — Phase A (z0–7 ≈ 300 MB) + B10 (picker entry) + B12 step 4 (offline render) cover it. Portable/HF/cross-border use cases are served by the global low-zoom coverage. The "regions still win" and "doesn't go blank above z7 offline" concerns from the GRA-72 assessment are Tasks B8/B9.
- **Type consistency:** `WorldMap.MaxZoom` (Go) → `maps_downloads.max_zoom` → `dto.LocalBoundsEntry.MaxZoom` (`json:"maxZoom"`) → store `maxZoomBySlug` → protocol `maxZoomBySlugProvider`. Slug literal `"world"` is identical across `mapsslug.Parse`, `mapscatalog` index, `manager.urlForSlug`, `lookupCatalogEntry`, `buildWorldNode`, and the source-maxzoom check. `lookupCatalogBBox` is renamed to `lookupCatalogEntry` everywhere (B5 step 4 sweeps callers).
- **Cross-repo seam:** the slug/route/manifest contract table is the single source of truth both phases build against; Phase A must land (or be mockable) before B12 step 4.
