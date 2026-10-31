import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  DBZ_BANDS,
  DBZ_COLORS,
  RADAR_BACKEND_RASTER,
  RADAR_BACKEND_VECTOR,
  ACTIVE_RADAR_BACKEND,
  RADAR_REGION_US,
  RADAR_REGION_WORLD,
  radarTileUrl,
  radarProvider,
  radarProviderForRegion,
  worldRadarProvider,
  rainviewerTileUrl,
  rainviewerFrameTileUrl,
  rainviewerManifestUrl,
  vectorTileUrl,
  radarManifestUrl,
  radarManifestUrlForRegion,
  parseManifestFrames,
  parseRainviewerManifestFrames,
  parseManifestFramesForRegion,
} from './radar-source.js';

test('every dBZ band has a color', () => {
  assert.ok(DBZ_BANDS.length > 0);
  for (const dbz of DBZ_BANDS) {
    assert.match(DBZ_COLORS[dbz], /^#[0-9a-fA-F]{6}$/, `band ${dbz} needs a hex color`);
  }
});

test('active backend is the GRA-48 vector contours', () => {
  assert.equal(ACTIVE_RADAR_BACKEND, RADAR_BACKEND_VECTOR);
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

import { buildDbzFillColor, buildRadarFillColor } from './radar-source.js';

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
  // Per-frame provider: no static tile template -- radar.js injects it per ts.
  assert.equal(p.source.tiles, undefined);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'fill');
  assert.equal(p.layers[0]['source-layer'], 'radar');
  assert.deepEqual(p.layers[0].paint['fill-color'], buildRadarFillColor());
  assert.equal(p.opacity.property, 'fill-opacity');
  assert.deepEqual(p.opacity.layerIds, ['radar-fill']);
});

test('vector source is bounded to the generated z3-z8 archive', () => {
  const p = radarProvider(RADAR_BACKEND_VECTOR);
  assert.equal(p.source.minzoom, 3);
  assert.equal(p.source.maxzoom, 8);
});

test('vectorTileUrl is the per-frame .pbf route keyed by ts (no cache-bust)', () => {
  assert.equal(vectorTileUrl(1750020000), 'https://maps.nw5w.com/radar/1750020000/{z}/{x}/{y}.pbf');
});

test('vector provider is per-frame with a frameTiles template (no cacheBust)', () => {
  const p = radarProvider(RADAR_BACKEND_VECTOR);
  assert.equal(p.perFrame, true);
  assert.equal(p.cacheBust, undefined);
  assert.equal(typeof p.frameTiles, 'function');
  assert.deepEqual(p.frameTiles(1750020000), [
    'https://maps.nw5w.com/radar/1750020000/{z}/{x}/{y}.pbf',
  ]);
});

test('radarManifestUrl points at the loop manifest', () => {
  assert.equal(radarManifestUrl(), 'https://maps.nw5w.com/radar/manifest.json');
});

test('parseManifestFrames reverses to oldest-first and keeps ts/iso', () => {
  const manifest = {
    schema_version: 1,
    frames: [
      { ts: 1750020000, iso: '2026-06-13T18:00:00Z', size: 5 }, // newest
      { ts: 1750019700, iso: '2026-06-13T17:55:00Z' },          // older
    ],
  };
  const frames = parseManifestFrames(manifest);
  assert.deepEqual(frames, [
    { ts: 1750019700, iso: '2026-06-13T17:55:00Z' },
    { ts: 1750020000, iso: '2026-06-13T18:00:00Z' },
  ]);
});

test('parseManifestFrames returns [] for bad/empty/unknown-schema input', () => {
  assert.deepEqual(parseManifestFrames(null), []);
  assert.deepEqual(parseManifestFrames({}), []);
  assert.deepEqual(parseManifestFrames({ schema_version: 2, frames: [] }), []);
  assert.deepEqual(parseManifestFrames({ schema_version: 1, frames: 'nope' }), []);
  // Drops malformed entries but keeps valid ones.
  assert.deepEqual(
    parseManifestFrames({ schema_version: 1, frames: [{ ts: 'x', iso: 'y' }, { ts: 1, iso: 'a' }] }),
    [{ ts: 1, iso: 'a' }],
  );
});

test('raster provider has no cacheBust (latest-frame IEM URL is already live)', () => {
  const p = radarProvider(RADAR_BACKEND_RASTER);
  assert.equal(p.cacheBust, undefined);
});

test('fill-color step has strictly-ascending stops and lowest-band base color', () => {
  const expr = buildDbzFillColor();
  // expr = ['step', input, base, stop1, c1, stop2, c2, ...]
  assert.equal(expr[2], DBZ_COLORS[DBZ_BANDS[0]], 'base output is the lowest band color');
  let prev = -Infinity;
  for (let i = 3; i < expr.length; i += 2) {
    const stop = expr[i];
    assert.ok(stop > prev, `stop ${stop} must exceed previous ${prev}`);
    assert.equal(expr[i + 1], DBZ_COLORS[stop], `stop ${stop} maps to its palette color`);
    prev = stop;
  }
});

test('raster source caps maxzoom so tiles overzoom instead of 404ing', () => {
  const p = radarProvider(RADAR_BACKEND_RASTER);
  assert.equal(typeof p.source.maxzoom, 'number');
  assert.ok(p.source.maxzoom >= 7 && p.source.maxzoom <= 12);
});

test('world provider is a per-frame RainViewer raster loop driven by raster-opacity', () => {
  const p = worldRadarProvider();
  assert.equal(p.sourceId, 'radar-tiles');
  assert.equal(p.source.type, 'raster');
  assert.equal(p.source.tileSize, 256);
  // Per-frame: no static `tiles` on the source (radar.js injects frameTiles(ts)).
  assert.equal(p.source.tiles, undefined);
  // RainViewer tops out at native z7; cap so MapLibre overzooms instead of
  // requesting non-existent z8+ tiles.
  assert.equal(p.source.maxzoom, 7);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'raster');
  assert.equal(p.opacity.property, 'raster-opacity');
  assert.deepEqual(p.opacity.layerIds, ['radar-raster']);
});

test('world provider is per-frame with a rainviewer frameTiles template (no cacheBust)', () => {
  const p = worldRadarProvider();
  assert.equal(p.perFrame, true);
  assert.equal(p.cacheBust, undefined);
  assert.deepEqual(p.frameTiles(1700000000), [
    'https://maps.nw5w.com/radar/rainviewer/1700000000/{z}/{x}/{y}.png',
  ]);
});

test('rainviewerFrameTileUrl is the per-frame /radar/rainviewer/{ts}/ route (no query)', () => {
  const u = rainviewerFrameTileUrl(1700000000);
  assert.equal(u, 'https://maps.nw5w.com/radar/rainviewer/1700000000/{z}/{x}/{y}.png');
  assert.ok(!u.includes('?'));
});

test('rainviewerTileUrl (legacy latest) is the query-free /radar/rainviewer/ route', () => {
  assert.equal(rainviewerTileUrl(), 'https://maps.nw5w.com/radar/rainviewer/{z}/{x}/{y}.png');
  assert.ok(!rainviewerTileUrl().includes('?'));
});

test('rainviewerManifestUrl points at the RainViewer loop manifest', () => {
  assert.equal(rainviewerManifestUrl(), 'https://maps.nw5w.com/radar/rainviewer/manifest.json');
});

test('radarManifestUrlForRegion picks contour vs RainViewer manifest by region', () => {
  assert.equal(radarManifestUrlForRegion(RADAR_REGION_US), radarManifestUrl());
  assert.equal(radarManifestUrlForRegion(), radarManifestUrl());
  assert.equal(radarManifestUrlForRegion(RADAR_REGION_WORLD), rainviewerManifestUrl());
});

test('parseRainviewerManifestFrames reverses to oldest-first and synthesizes iso', () => {
  const manifest = {
    schema_version: 1,
    frames: [{ ts: 1700001200 }, { ts: 1700000600 }, { ts: 1700000000 }], // newest-first
    latest: { ts: 1700001200 },
    cadence_seconds: 600,
  };
  const frames = parseRainviewerManifestFrames(manifest);
  assert.deepEqual(
    frames.map((f) => f.ts),
    [1700000000, 1700000600, 1700001200],
  );
  // iso is synthesized from ts (the RainViewer manifest carries no iso).
  assert.equal(frames[0].iso, new Date(1700000000 * 1000).toISOString());
});

test('parseRainviewerManifestFrames returns [] for bad/empty/unknown-schema input', () => {
  assert.deepEqual(parseRainviewerManifestFrames(null), []);
  assert.deepEqual(parseRainviewerManifestFrames({}), []);
  assert.deepEqual(parseRainviewerManifestFrames({ schema_version: 2, frames: [] }), []);
  assert.deepEqual(parseRainviewerManifestFrames({ schema_version: 1, frames: 'nope' }), []);
  // Drops a frame with a non-numeric ts, keeps the good one.
  assert.deepEqual(
    parseRainviewerManifestFrames({ schema_version: 1, frames: [{ ts: 'x' }, { ts: 1 }] }).map((f) => f.ts),
    [1],
  );
});

test('parseManifestFramesForRegion dispatches to the right parser', () => {
  const us = { schema_version: 1, frames: [{ ts: 2, iso: 'b' }, { ts: 1, iso: 'a' }] };
  const world = { schema_version: 1, frames: [{ ts: 2 }, { ts: 1 }] };
  assert.deepEqual(
    parseManifestFramesForRegion(RADAR_REGION_US, us).map((f) => f.ts),
    [1, 2],
  );
  assert.deepEqual(
    parseManifestFramesForRegion(RADAR_REGION_WORLD, world).map((f) => f.ts),
    [1, 2],
  );
});

test('radarProviderForRegion: US delegates to the backend, world is per-frame RainViewer', () => {
  // US region uses the active US backend (vector contours today).
  const us = radarProviderForRegion(RADAR_REGION_US);
  assert.equal(us.source.type, radarProvider().source.type);
  assert.equal(us.perFrame, true);
  // Default region is US.
  assert.equal(radarProviderForRegion().source.type, radarProvider().source.type);
  // World region is the per-frame RainViewer raster overlay.
  const world = radarProviderForRegion(RADAR_REGION_WORLD);
  assert.equal(world.source.type, 'raster');
  assert.equal(world.perFrame, true);
  assert.match(world.frameTiles(1700000000)[0], /\/radar\/rainviewer\/1700000000\//);
});

// --- surface precipitation type palette -------------------------------------

import {
  PTYPE_BANDS,
  PTYPE_COLORS,
  PTYPE_FREEZING_RAIN,
  PTYPE_LABELS,
  PTYPE_MIXED,
  PTYPE_NONE,
  PTYPE_RAIN,
  PTYPE_SLEET,
  PTYPE_SNOW,
  buildPtypeFillColor,
} from './radar-source.js';

test('ptype codes match the generator contract and are never renumbered', () => {
  // Mirrors radar-contour/src/ptype.rs. Changing these silently remaps colors
  // on every tile already in R2.
  assert.equal(PTYPE_NONE, 0);
  assert.equal(PTYPE_RAIN, 1);
  assert.equal(PTYPE_SNOW, 2);
  assert.equal(PTYPE_SLEET, 3);
  assert.equal(PTYPE_FREEZING_RAIN, 4);
  assert.equal(PTYPE_MIXED, 5);
});

test('every frozen/mixed type has a full hex ramp', () => {
  for (const ptype of [PTYPE_SNOW, PTYPE_SLEET, PTYPE_FREEZING_RAIN, PTYPE_MIXED]) {
    const ramp = PTYPE_COLORS[ptype];
    assert.ok(ramp, `ptype ${ptype} needs a ramp`);
    for (const dbz of PTYPE_BANDS) {
      assert.match(ramp[dbz], /^#[0-9a-fA-F]{6}$/, `ptype ${ptype} band ${dbz} needs a hex color`);
    }
  }
});

test('every type has a label for the legend', () => {
  for (const ptype of [PTYPE_NONE, PTYPE_RAIN, PTYPE_SNOW, PTYPE_SLEET,
                       PTYPE_FREEZING_RAIN, PTYPE_MIXED]) {
    assert.equal(typeof PTYPE_LABELS[ptype], 'string');
    assert.ok(PTYPE_LABELS[ptype].length > 0);
  }
});

test('snow keeps the blue family and liquid gives it up', () => {
  // The whole point of the repaint: at the weak reflectivities where winter
  // precip lives, snow must be bluer than rain, or the two are indistinguishable.
  const blueness = (hex) => {
    const b = parseInt(hex.slice(5, 7), 16);
    const r = parseInt(hex.slice(1, 3), 16);
    return b - r;
  };
  for (const dbz of [10, 15, 20, 25, 30]) {
    assert.ok(blueness(PTYPE_COLORS[PTYPE_SNOW][dbz]) > 40,
      `snow at ${dbz} dBZ should read blue, got ${PTYPE_COLORS[PTYPE_SNOW][dbz]}`);
    assert.ok(blueness(DBZ_COLORS[dbz]) < 10,
      `liquid at ${dbz} dBZ must not be blue, got ${DBZ_COLORS[dbz]}`);
  }
});

// Small color helpers so the palette contract is checked numerically rather
// than by eye. Hue and chroma are what actually make two map areas tell apart;
// raw RGB distance alone is misleading near white.
const rgbOf = (hex) =>
  [0, 1, 2].map((i) => parseInt(hex.slice(1 + i * 2, 3 + i * 2), 16) / 255);
const lumOf = (hex) => {
  const [r, g, b] = rgbOf(hex);
  return 0.2126 * r + 0.7152 * g + 0.0722 * b;
};
const chromaOf = (hex) => {
  const c = rgbOf(hex);
  return Math.max(...c) - Math.min(...c);
};
const hueOf = (hex) => {
  const [r, g, b] = rgbOf(hex);
  const max = Math.max(r, g, b);
  const d = max - Math.min(r, g, b);
  if (d === 0) return 0;
  let h;
  if (max === r) h = ((g - b) / d) % 6;
  else if (max === g) h = (b - r) / d + 2;
  else h = (r - g) / d + 4;
  return (h * 60 + 360) % 360;
};
const hueGap = (a, b) => {
  const d = Math.abs(hueOf(a) - hueOf(b)) % 360;
  return Math.min(d, 360 - d);
};
const rgbGap = (a, b) => {
  const [x, y] = [rgbOf(a), rgbOf(b)];
  return (Math.abs(x[0] - y[0]) + Math.abs(x[1] - y[1]) + Math.abs(x[2] - y[2])) * 255;
};

const WINTER_TYPES = [PTYPE_SNOW, PTYPE_SLEET, PTYPE_FREEZING_RAIN, PTYPE_MIXED];
const HUED_TYPES = [PTYPE_SNOW, PTYPE_SLEET, PTYPE_FREEZING_RAIN];

test('every winter ramp darkens monotonically with intensity', () => {
  // Intensity has to read within a type, not only between types.
  for (const ptype of WINTER_TYPES) {
    let prev = Infinity;
    for (const dbz of PTYPE_BANDS) {
      const l = lumOf(PTYPE_COLORS[ptype][dbz]);
      assert.ok(l < prev, `ptype ${ptype} must darken at ${dbz} dBZ (${l} vs ${prev})`);
      prev = l;
    }
  }
});

test('mixed is the only desaturated ramp, so it never reads as a hue', () => {
  // Mixed is deliberately neutral and the other three deliberately are not.
  // That contrast is what separates mixed from all of them at once.
  for (const dbz of PTYPE_BANDS) {
    assert.ok(chromaOf(PTYPE_COLORS[PTYPE_MIXED][dbz]) < 0.12,
      `mixed at ${dbz} dBZ must stay neutral`);
    for (const ptype of HUED_TYPES) {
      assert.ok(chromaOf(PTYPE_COLORS[ptype][dbz]) > 0.15,
        `ptype ${ptype} at ${dbz} dBZ needs enough chroma to read as a hue`);
    }
  }
});

test('snow, sleet and freezing rain are separated in hue at every band', () => {
  // This is the check that caught a pale-blue snow and a pale-violet freezing
  // rain collapsing to the same near-white at the lightest bands.
  for (const dbz of PTYPE_BANDS) {
    for (let i = 0; i < HUED_TYPES.length; i++) {
      for (let j = i + 1; j < HUED_TYPES.length; j++) {
        const a = PTYPE_COLORS[HUED_TYPES[i]][dbz];
        const b = PTYPE_COLORS[HUED_TYPES[j]][dbz];
        assert.ok(hueGap(a, b) > 30,
          `ptype ${HUED_TYPES[i]} and ${HUED_TYPES[j]} share a hue at ${dbz} dBZ `
          + `(${a} vs ${b}, gap ${hueGap(a, b).toFixed(0)} deg)`);
      }
    }
  }
});

test('winter ramps are far apart in the bands that actually publish', () => {
  // The generator runs with a 20 dBZ floor, so these are the bands a reader
  // ever sees side by side. Below the floor the ramps may converge.
  for (const dbz of PTYPE_BANDS.filter((d) => d >= 20)) {
    for (let i = 0; i < WINTER_TYPES.length; i++) {
      for (let j = i + 1; j < WINTER_TYPES.length; j++) {
        const a = PTYPE_COLORS[WINTER_TYPES[i]][dbz];
        const b = PTYPE_COLORS[WINTER_TYPES[j]][dbz];
        assert.ok(rgbGap(a, b) > 60,
          `ptype ${WINTER_TYPES[i]} and ${WINTER_TYPES[j]} too close at ${dbz} dBZ `
          + `(${a} vs ${b}, gap ${rgbGap(a, b).toFixed(0)})`);
      }
    }
  }
});

test('buildPtypeFillColor ramps on dbz within a type and holds above 40', () => {
  const expr = buildPtypeFillColor(PTYPE_SNOW);
  assert.equal(expr[0], 'step');
  assert.deepEqual(expr[1], ['get', 'dbz']);
  assert.equal(expr[2], PTYPE_COLORS[PTYPE_SNOW][PTYPE_BANDS[0]]);
  // No stop above the last winter band, so stronger echo keeps the darkest color.
  const stops = [];
  for (let i = 3; i < expr.length; i += 2) stops.push(expr[i]);
  assert.deepEqual(stops, PTYPE_BANDS.slice(1));
  assert.equal(expr[expr.length - 1], PTYPE_COLORS[PTYPE_SNOW][40]);
});

test('buildPtypeFillColor falls back to the liquid ramp for an unknown type', () => {
  assert.deepEqual(buildPtypeFillColor(PTYPE_RAIN), buildDbzFillColor());
  assert.deepEqual(buildPtypeFillColor(PTYPE_NONE), buildDbzFillColor());
  assert.deepEqual(buildPtypeFillColor(99), buildDbzFillColor());
});

test('buildRadarFillColor matches on ptype with liquid as the default branch', () => {
  const expr = buildRadarFillColor();
  assert.equal(expr[0], 'match');
  assert.deepEqual(expr[1], ['get', 'ptype']);
  // Trailing element is the fallback, which must be the liquid ramp so that
  // ptype 0 (unknown) and 1 (rain) both render as plain precipitation.
  assert.deepEqual(expr[expr.length - 1], buildDbzFillColor());
  // Each frozen/mixed type has its own branch with its own ramp.
  for (const ptype of [PTYPE_SNOW, PTYPE_SLEET, PTYPE_FREEZING_RAIN, PTYPE_MIXED]) {
    const at = expr.indexOf(ptype, 2);
    assert.ok(at > 0, `ptype ${ptype} needs a match branch`);
    assert.deepEqual(expr[at + 1], buildPtypeFillColor(ptype));
  }
  // Rain is NOT its own branch -- it shares the default, so an old tile with no
  // ptype attribute at all cannot fall through to nothing.
  assert.equal(expr.indexOf(PTYPE_RAIN, 2), -1);
});
