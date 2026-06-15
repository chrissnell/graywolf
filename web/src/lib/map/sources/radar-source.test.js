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
  // Per-frame provider: no static tile template -- radar.js injects it per ts.
  assert.equal(p.source.tiles, undefined);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'fill');
  assert.equal(p.layers[0]['source-layer'], 'radar');
  assert.deepEqual(p.layers[0].paint['fill-color'], buildDbzFillColor());
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
