# Live Map NEXRAD Radar Overlay Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a NEXRAD reflectivity overlay to the Live Map — a national radar layer with a visibility toggle and opacity slider — built behind a pluggable "radar backend" seam so that when GRA-48's smoothed vector-contour tiles go live, switching from raster to vector is a one-constant change with no rewiring.

**Architecture:** A pure module (`radar-source.js`) owns all radar configuration — the dBZ band thresholds, the NWS color ramp, the tile base URL, and a `radarProvider()` that returns a uniform descriptor (`{ sourceId, source, layers, opacity }`) for either backend. A thin imperative layer module (`radar.js`) consumes that descriptor and performs the MapLibre `addSource`/`addLayer`/`setPaintProperty` calls behind a stable `{ setVisible, setOpacity, destroy }` interface. `LiveMapV2.svelte` mounts it with persisted `visible`/`opacity` settings and renders the toggle + slider. The raster backend ships now (IEM N0Q national composite, served via the origin Worker in production); the vector backend (GRA-48 MVT contour tiles + MapLibre `fill` layers) is implemented as a provider branch and exercised by tests, then activated later by flipping `ACTIVE_RADAR_BACKEND`.

**Tech Stack:** Svelte 5 (runes), MapLibre GL JS, `node:test` + `node:assert/strict` (the repo's runner: `node --test 'src/**/*.test.js'`), `localStorage` for per-browser persistence.

---

## File Structure

- **Create** `web/src/lib/map/sources/radar-source.js` — pure config + provider logic. dBZ bands, NWS palette, tile base URL, `buildDbzFillColor()`, `radarProvider(backend)`. No MapLibre/DOM imports → unit-testable under `node --test`. **This file is the GRA-48 integration seam.**
- **Create** `web/src/lib/map/sources/radar-source.test.js` — `node:test` unit tests for the pure module, including a test proving the vector (GRA-48) provider descriptor is well-formed before GRA-48 exists.
- **Create** `web/src/lib/map/layers/radar.js` — thin imperative layer module. `mountRadarLayer(map, { visible, opacity })` → `{ setVisible, setOpacity, destroy }`. Consumes a provider descriptor; backend-agnostic.
- **Modify** `web/src/routes/LiveMapV2.svelte` — import `mountRadarLayer`, add `radarSettings` state (persisted), mount the layer in `onMapReady` before trails, drive it from `$effect`s, tear it down in `onDestroy`, and add the "Radar" toggle + opacity slider to the layer card.

### Why this split

The repo's tests run under `node --test` on pure logic (see `web/src/lib/channelRefStatus.test.js`) — there is no jsdom/MapLibre test harness. So all branching logic (which backend, which paint property, the color expression, URL building) lives in `radar-source.js` and is fully tested; `radar.js` stays a dumb translator of a descriptor into MapLibre calls, mirroring the existing layer modules (`web/src/lib/map/layers/weather.js`).

---

## Task 1: Radar config + raster provider (pure module)

**Files:**
- Create: `web/src/lib/map/sources/radar-source.js`
- Test: `web/src/lib/map/sources/radar-source.test.js`

- [ ] **Step 1: Write the failing test**

Create `web/src/lib/map/sources/radar-source.test.js`:

```js
import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DBZ_BANDS,
  DBZ_COLORS,
  RADAR_BACKEND_RASTER,
  RADAR_BACKEND_VECTOR,
  ACTIVE_RADAR_BACKEND,
  radarTileUrl,
  radarProvider,
} from './radar-source.js';

test('every dBZ band has a color', () => {
  assert.ok(DBZ_BANDS.length > 0);
  for (const dbz of DBZ_BANDS) {
    assert.match(DBZ_COLORS[dbz], /^#[0-9a-fA-F]{6}$/, `band ${dbz} needs a hex color`);
  }
});

test('default active backend is raster for v1', () => {
  assert.equal(ACTIVE_RADAR_BACKEND, RADAR_BACKEND_RASTER);
});

test('raster provider yields one raster layer driven by raster-opacity', () => {
  const p = radarProvider(RADAR_BACKEND_RASTER);
  assert.equal(p.sourceId, 'radar-tiles');
  assert.equal(p.source.type, 'raster');
  assert.equal(p.source.tileSize, 256);
  assert.match(p.source.tiles[0], /nexrad-n0q\/\{z\}\/\{x\}\/\{y\}\.png$/);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'raster');
  assert.equal(p.layers[0].source, 'radar-tiles');
  assert.equal(p.opacity.property, 'raster-opacity');
  assert.deepEqual(p.opacity.layerIds, [p.layers[0].id]);
});

test('radarTileUrl builds an XYZ raster template under the base', () => {
  const url = radarTileUrl('nexrad-n0q', 'png');
  assert.ok(url.endsWith('/nexrad-n0q/{z}/{x}/{y}.png'));
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && node --test 'src/lib/map/sources/radar-source.test.js'`
Expected: FAIL — `Cannot find module './radar-source.js'`.

- [ ] **Step 3: Write the minimal implementation**

Create `web/src/lib/map/sources/radar-source.js`:

```js
// graywolf/web/src/lib/map/sources/radar-source.js
//
// Single source of truth for the Live Map radar overlay. Pure data + small
// builders only -- no MapLibre or DOM imports -- so it is unit-testable under
// `node --test` and so the raster (v1) and vector (GRA-48) backends share one
// palette and one tile-base.
//
// GRA-48 INTEGRATION SEAM: when the Rust contour generator's MVT tiles are
// live on the origin Worker, flip ACTIVE_RADAR_BACKEND to RADAR_BACKEND_VECTOR.
// Nothing else in the client changes -- radar.js and LiveMapV2 consume the
// descriptor returned by radarProvider() and are backend-agnostic.

// NWS reflectivity color ramp, keyed by the dBZ lower bound of each band.
// Used by the vector backend's fill-color expression and by any legend UI.
export const DBZ_BANDS = [5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75];
export const DBZ_COLORS = {
  5: '#04e9e7', 10: '#019ff4', 15: '#0300f4', 20: '#02fd02', 25: '#01c501',
  30: '#008e00', 35: '#fdf802', 40: '#e5bc00', 45: '#fd9500', 50: '#fd0000',
  55: '#d40000', 60: '#bc0000', 65: '#f800fd', 70: '#9854c6', 75: '#fdfdfd',
};

export const RADAR_BACKEND_RASTER = 'raster';
export const RADAR_BACKEND_VECTOR = 'vector';

// v1 ships raster. Flip to RADAR_BACKEND_VECTOR once GRA-48 tiles are live.
export const ACTIVE_RADAR_BACKEND = RADAR_BACKEND_RASTER;

// Tile base. In production this points at the origin Worker (R2-backed,
// edge-cached). For local dev you may point RADAR_TILE_BASE straight at IEM:
//   https://mesonet.agron.iastate.edu/cache/tile.py/1.0.0
// Production flips it to the Worker with no other code change (per GRA-42).
export const RADAR_TILE_BASE = 'https://mesonet.agron.iastate.edu/cache/tile.py/1.0.0';

const RADAR_ATTRIBUTION = 'NEXRAD via NWS / Iowa State Mesonet';
const RADAR_SOURCE_ID = 'radar-tiles';

// Build an XYZ tile-URL template under the configured base.
export function radarTileUrl(product, ext) {
  return `${RADAR_TILE_BASE}/${product}/{z}/{x}/{y}.${ext}`;
}

// Uniform descriptor consumed by radar.js. `layers` is ordered; `opacity`
// tells the layer module which paint property and which layer ids the opacity
// slider drives (raster-opacity for raster, fill-opacity for vector).
export function radarProvider(backend = ACTIVE_RADAR_BACKEND) {
  if (backend === RADAR_BACKEND_RASTER) {
    return {
      sourceId: RADAR_SOURCE_ID,
      source: {
        type: 'raster',
        tiles: [radarTileUrl('nexrad-n0q', 'png')],
        tileSize: 256,
        attribution: RADAR_ATTRIBUTION,
      },
      layers: [
        {
          id: 'radar-raster',
          type: 'raster',
          source: RADAR_SOURCE_ID,
          // Cheap browser bilinear -- harmless, marginal at native zoom.
          paint: { 'raster-resampling': 'linear' },
        },
      ],
      opacity: { property: 'raster-opacity', layerIds: ['radar-raster'] },
    };
  }
  throw new Error(`unsupported radar backend: ${backend}`);
}
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && node --test 'src/lib/map/sources/radar-source.test.js'`
Expected: PASS — 4 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/map/sources/radar-source.js web/src/lib/map/sources/radar-source.test.js
git commit -m "feat(map): radar config + raster provider (pure, testable)"
```

---

## Task 2: Vector (GRA-48) provider + fill-color expression

**Files:**
- Modify: `web/src/lib/map/sources/radar-source.js`
- Test: `web/src/lib/map/sources/radar-source.test.js`

This task implements the GRA-48 branch now so the seam is proven by tests before GRA-48 ships. It does NOT change `ACTIVE_RADAR_BACKEND`.

- [ ] **Step 1: Write the failing test**

Append to `web/src/lib/map/sources/radar-source.test.js`:

```js
import { buildDbzFillColor } from './radar-source.js';

test('buildDbzFillColor is a step expression over the dbz property', () => {
  const expr = buildDbzFillColor();
  assert.equal(expr[0], 'step');
  assert.deepEqual(expr[1], ['get', 'dbz']);
  // First element after input is the base output color, then alternating
  // stop/value pairs -- one stop per band.
  const stopCount = (expr.length - 3) / 2 + 1;
  assert.equal(stopCount, DBZ_BANDS.length);
  // Highest band maps to its palette color.
  assert.equal(expr[expr.length - 1], DBZ_COLORS[DBZ_BANDS[DBZ_BANDS.length - 1]]);
});

test('vector provider yields a fill layer driven by fill-opacity', () => {
  const p = radarProvider(RADAR_BACKEND_VECTOR);
  assert.equal(p.sourceId, 'radar-tiles');
  assert.equal(p.source.type, 'vector');
  assert.match(p.source.tiles[0], /radar\/\{z\}\/\{x\}\/\{y\}\.pbf$/);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'fill');
  assert.equal(p.layers[0]['source-layer'], 'radar');
  assert.deepEqual(p.layers[0].paint['fill-color'], buildDbzFillColor());
  assert.equal(p.opacity.property, 'fill-opacity');
  assert.deepEqual(p.opacity.layerIds, ['radar-fill']);
});
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `cd web && node --test 'src/lib/map/sources/radar-source.test.js'`
Expected: FAIL — `buildDbzFillColor is not a function` / `unsupported radar backend: vector`.

- [ ] **Step 3: Write the minimal implementation**

In `web/src/lib/map/sources/radar-source.js`, add the expression builder above `radarProvider`:

```js
// MapLibre `step` expression mapping a polygon's `dbz` property to the NWS
// ramp. Output below the first stop is the lowest band's color.
export function buildDbzFillColor() {
  const expr = ['step', ['get', 'dbz'], DBZ_COLORS[DBZ_BANDS[0]]];
  for (const dbz of DBZ_BANDS) {
    expr.push(dbz, DBZ_COLORS[dbz]);
  }
  return expr;
}
```

Then add the vector branch inside `radarProvider`, replacing the final `throw`:

```js
  if (backend === RADAR_BACKEND_VECTOR) {
    return {
      sourceId: RADAR_SOURCE_ID,
      source: {
        type: 'vector',
        // Origin Worker resolves the `latest` pointer GRA-48 publishes to R2.
        tiles: [`${RADAR_TILE_BASE}/radar/{z}/{x}/{y}.pbf`],
        attribution: RADAR_ATTRIBUTION,
      },
      layers: [
        {
          id: 'radar-fill',
          type: 'fill',
          source: RADAR_SOURCE_ID,
          'source-layer': 'radar', // MVT layer name produced by GRA-48
          paint: { 'fill-color': buildDbzFillColor(), 'fill-antialias': true },
        },
      ],
      opacity: { property: 'fill-opacity', layerIds: ['radar-fill'] },
    };
  }
  throw new Error(`unsupported radar backend: ${backend}`);
```

- [ ] **Step 4: Run the test to verify it passes**

Run: `cd web && node --test 'src/lib/map/sources/radar-source.test.js'`
Expected: PASS — 6 tests.

- [ ] **Step 5: Commit**

```bash
git add web/src/lib/map/sources/radar-source.js web/src/lib/map/sources/radar-source.test.js
git commit -m "feat(map): GRA-48 vector contour provider + dBZ fill-color expression"
```

---

## Task 3: Imperative radar layer module

**Files:**
- Create: `web/src/lib/map/layers/radar.js`

No unit test: this file only translates a (tested) descriptor into MapLibre side-effects, exactly like the other `layers/*.js` modules, which are not unit-tested either. It is exercised manually in Task 5.

- [ ] **Step 1: Write the implementation**

Create `web/src/lib/map/layers/radar.js`:

```js
// NEXRAD radar overlay layer for the Live Map.
//
// Backend-agnostic: it asks radar-source.js for a provider descriptor and
// performs the MapLibre source/layer calls. Raster today, GRA-48 vector
// tomorrow -- this file does not change when the backend flips; only
// ACTIVE_RADAR_BACKEND in radar-source.js does.
//
// Mirrors the other layer modules (stations.js, weather.js): mount returns
// control methods; LiveMapV2 persists settings and drives them via effects.

import { radarProvider } from '../sources/radar-source.js';

export function mountRadarLayer(map, { visible, opacity }) {
  const provider = radarProvider();

  // Insert below the first symbol layer so basemap labels stay readable above
  // the radar. DOM-based layers (stations, weather markers) always render
  // above the GL canvas regardless of GL layer order.
  const firstSymbolId = map.getStyle().layers.find((l) => l.type === 'symbol')?.id;

  map.addSource(provider.sourceId, provider.source);

  for (const layer of provider.layers) {
    const spec = {
      ...layer,
      layout: { ...(layer.layout ?? {}), visibility: visible ? 'visible' : 'none' },
      paint: { ...(layer.paint ?? {}), [provider.opacity.property]: opacity },
    };
    map.addLayer(spec, firstSymbolId);
  }

  function setVisible(v) {
    const value = v ? 'visible' : 'none';
    for (const layer of provider.layers) {
      map.setLayoutProperty(layer.id, 'visibility', value);
    }
  }

  function setOpacity(v) {
    for (const id of provider.opacity.layerIds) {
      map.setPaintProperty(id, provider.opacity.property, v);
    }
  }

  function destroy() {
    for (const layer of provider.layers) {
      if (map.getLayer(layer.id)) map.removeLayer(layer.id);
    }
    if (map.getSource(provider.sourceId)) map.removeSource(provider.sourceId);
  }

  return { setVisible, setOpacity, destroy };
}
```

- [ ] **Step 2: Verify it imports cleanly (lint/build smoke)**

Run: `cd web && npx vite build 2>&1 | tail -5`
Expected: build succeeds (module resolves; no syntax errors). The layer isn't wired yet, so there's nothing visual to check.

- [ ] **Step 3: Commit**

```bash
git add web/src/lib/map/layers/radar.js
git commit -m "feat(map): backend-agnostic radar layer module"
```

---

## Task 4: Wire the layer into LiveMapV2 (state + lifecycle)

**Files:**
- Modify: `web/src/routes/LiveMapV2.svelte`

- [ ] **Step 1: Add the import**

Find the layer imports block (near `import { mountMyPositionLayer } from '../lib/map/layers/my-position.js';`) and add below it:

```js
  import { mountRadarLayer } from '../lib/map/layers/radar.js';
```

- [ ] **Step 2: Add the layer handle and persisted settings**

Find the layer handle declarations (`let myPositionLayer = null;`) and add below them:

```js
  let radarLayer = null;

  // Radar overlay settings -- persisted per browser (not per account).
  const radarSettings = $state({
    visible: localStorage.getItem('gw_radar_visible') === '1',
    opacity: parseFloat(localStorage.getItem('gw_radar_opacity') ?? '0.6'),
  });
```

- [ ] **Step 3: Mount in `onMapReady`**

Find `function onMapReady(map) {` and its first line `mapRef = map;`. Insert immediately after that line:

```js
    // Radar first so the raster/fill sits below trails and station markers in
    // the GL stack. DOM layers (stations, weather) always render above the
    // canvas regardless, but GL line layers (trails) would otherwise cover it.
    radarLayer = mountRadarLayer(map, radarSettings);
```

- [ ] **Step 4: Add driving effects**

Find the block of `$effect(() => { ... })` calls that push `layerToggles` into the layer modules (after the `myPosition` effect). Add two new effects:

```js
  $effect(() => {
    const v = radarSettings.visible;
    localStorage.setItem('gw_radar_visible', v ? '1' : '0');
    radarLayer?.setVisible(v);
  });
  $effect(() => {
    const v = radarSettings.opacity;
    localStorage.setItem('gw_radar_opacity', String(v));
    radarLayer?.setOpacity(v);
  });
```

- [ ] **Step 5: Tear down in `onDestroy`**

Find the `onDestroy(() => {` block. Add the radar teardown alongside the other `?.destroy()` calls and null-out:

```js
    radarLayer?.destroy();
```

and, with the other `… = null;` assignments:

```js
    radarLayer = null;
```

- [ ] **Step 6: Build smoke check**

Run: `cd web && npx vite build 2>&1 | tail -5`
Expected: build succeeds. (No UI control yet — added in Task 5.)

- [ ] **Step 7: Commit**

```bash
git add web/src/routes/LiveMapV2.svelte
git commit -m "feat(map): mount radar layer in LiveMapV2 with persisted settings"
```

---

## Task 5: Radar toggle + opacity slider UI

**Files:**
- Modify: `web/src/routes/LiveMapV2.svelte`

- [ ] **Step 1: Add the toggle + slider to the layer card**

Find the layer-toggles markup (the `<label class="toggle-row">` entries inside the layer card snippet, e.g. the "Trails" toggle). After the existing toggle rows, add a Radar toggle and an opacity slider:

```svelte
      <label class="toggle-row">
        <input
          type="checkbox"
          checked={radarSettings.visible}
          onchange={(e) => (radarSettings.visible = e.currentTarget.checked)}
        />
        <span>Radar</span>
      </label>
    </div>

    <label class="timerange-label" for="radar-opacity-range">
      Radar opacity: {Math.round(radarSettings.opacity * 100)}%
    </label>
    <input
      id="radar-opacity-range"
      type="range"
      min="0.1"
      max="1.0"
      step="0.05"
      class="radar-opacity-range"
      bind:value={radarSettings.opacity}
    />
    <div class="layer-toggles">
```

(The `</div>` then `<div class="layer-toggles">` re-open keeps the slider visually between toggle groups; if the Radar toggle is the last row in the card, drop the trailing `</div>`/`<div…>` wrapper and place the slider after the toggles `</div>` instead. Match the surrounding structure exactly.)

- [ ] **Step 2: Add the slider style**

Find the `<style>` block and the existing range/label styles. Add:

```css
  .radar-opacity-range {
    width: 100%;
    cursor: pointer;
    accent-color: var(--color-accent, #4a9eff);
  }
```

- [ ] **Step 3: Build and run the dev server**

Run: `cd web && npx vite build 2>&1 | tail -5`
Expected: build succeeds.

- [ ] **Step 4: Manual verification (record result in the commit)**

Run the dev server (`cd web && npm run dev`) or load a built graywolf, open the Live Map, and confirm:
1. The layer card shows a **Radar** toggle and a **Radar opacity** slider.
2. Toggling Radar on overlays national reflectivity; basemap labels remain visible above it; station/weather markers remain above it.
3. The opacity slider visibly changes radar transparency.
4. Reload the page → the toggle and opacity persist (localStorage).

Expected: all four hold. If radar tiles 404, confirm `RADAR_TILE_BASE` and the `nexrad-n0q` product path in `radar-source.js`.

- [ ] **Step 5: Commit**

```bash
git add web/src/routes/LiveMapV2.svelte
git commit -m "feat(map): radar toggle + opacity slider on Live Map"
```

---

## Task 6: Full test + build gate

**Files:** none (verification only)

- [ ] **Step 1: Run the web test suite**

Run: `cd web && npm test`
Expected: PASS — all existing tests plus the 6 new `radar-source.test.js` tests.

- [ ] **Step 2: Production build**

Run: `cd web && npm run build`
Expected: build succeeds with no errors.

- [ ] **Step 3: Commit (if any lint/format fixups were needed)**

```bash
git add -A
git commit -m "chore(map): radar overlay test + build gate" || echo "nothing to commit"
```

---

## GRA-48 cutover (future, NOT part of this plan)

When [GRA-48] ships the Rust contour generator and the origin Worker serves
`radar/{z}/{x}/{y}.pbf` (with the `latest` pointer), the entire client switch is:

1. In `web/src/lib/map/sources/radar-source.js`, set
   `export const ACTIVE_RADAR_BACKEND = RADAR_BACKEND_VECTOR;`
2. Confirm `RADAR_TILE_BASE` points at the origin Worker (not IEM directly).
3. Confirm the MVT layer name matches `'source-layer': 'radar'` (Task 2) and the
   polygon `dbz` property matches `buildDbzFillColor()`'s `['get', 'dbz']`.

No changes to `radar.js` or `LiveMapV2.svelte` — they consume the provider
descriptor and are backend-agnostic. The vector provider and its fill-color
expression are already covered by `radar-source.test.js`.

---

## Self-Review

**Spec coverage:**
- National radar overlay → Tasks 1, 3, 4, 5. ✓
- Visibility toggle + opacity slider, persisted → Tasks 4 (state/effects), 5 (UI). ✓
- Layer ordering below labels/markers → Task 3 (insert below first symbol layer). ✓
- Smooth GRA-48 integration → Tasks 1–2 (provider seam + tested vector branch) and the cutover note. ✓
- Origin-Worker/production base-URL → `RADAR_TILE_BASE` (Task 1), cutover note. ✓
- Out of scope (velocity, per-site, animation loop) → intentionally omitted, tracked in GRA-42/GRA-48. ✓

**Placeholder scan:** No TBD/TODO/"handle edge cases"/"write tests for the above" — every code and test step shows complete content. ✓

**Type consistency:** `radarProvider()` descriptor shape (`sourceId`, `source`, `layers`, `opacity.{property,layerIds}`) is identical across Tasks 1, 2, and consumed verbatim in Task 3. `radarSettings.{visible,opacity}` consistent across Tasks 4–5. `mountRadarLayer` / `setVisible` / `setOpacity` / `destroy` names consistent across Tasks 3–4. localStorage keys (`gw_radar_visible`, `gw_radar_opacity`) consistent across Tasks 2-state/4/5. ✓

[GRA-48]: see issue GRA-48 — Smoothed NEXRAD radar: Rust contour-tile generator.
