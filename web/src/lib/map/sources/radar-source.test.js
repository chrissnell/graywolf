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
  vectorTileUrl,
  radarManifestUrl,
  parseManifestFrames,
  frameBucket,
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

test('frameBucket is a 5-minute floor', () => {
  assert.equal(frameBucket(0), 0);
  assert.equal(frameBucket(299999), 0);
  assert.equal(frameBucket(300000), 1);
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

test('world provider is a RainViewer raster overlay driven by raster-opacity', () => {
  const p = worldRadarProvider();
  assert.equal(p.sourceId, 'radar-tiles');
  assert.equal(p.source.type, 'raster');
  assert.equal(p.source.tileSize, 256);
  assert.match(p.source.tiles[0], /\/radar\/rainviewer\/\{z\}\/\{x\}\/\{y\}\.png$/);
  // RainViewer tops out at native z7; cap so MapLibre overzooms instead of
  // requesting non-existent z8+ tiles.
  assert.equal(p.source.maxzoom, 7);
  assert.equal(p.layers.length, 1);
  assert.equal(p.layers[0].type, 'raster');
  assert.equal(p.opacity.property, 'raster-opacity');
  assert.deepEqual(p.opacity.layerIds, ['radar-raster']);
});

test('rainviewerTileUrl is the /radar/rainviewer/ route and appends ?v= when busting', () => {
  assert.equal(rainviewerTileUrl(), 'https://maps.nw5w.com/radar/rainviewer/{z}/{x}/{y}.png');
  assert.equal(rainviewerTileUrl(9), 'https://maps.nw5w.com/radar/rainviewer/{z}/{x}/{y}.png?v=9');
});

test('world provider exposes a cacheBust that swaps in a ?v= template', () => {
  const p = worldRadarProvider();
  assert.equal(typeof p.cacheBust, 'function');
  assert.deepEqual(p.cacheBust(7), ['https://maps.nw5w.com/radar/rainviewer/{z}/{x}/{y}.png?v=7']);
});

test('radarProviderForRegion: US delegates to the backend, world is RainViewer', () => {
  // US region uses the active US backend (vector contours today).
  const us = radarProviderForRegion(RADAR_REGION_US);
  assert.deepEqual(us.source.tiles, radarProvider().source.tiles);
  assert.equal(us.source.type, radarProvider().source.type);
  // Default region is US.
  assert.deepEqual(radarProviderForRegion().source.tiles, radarProvider().source.tiles);
  // World region is the RainViewer raster overlay.
  const world = radarProviderForRegion(RADAR_REGION_WORLD);
  assert.equal(world.source.type, 'raster');
  assert.match(world.source.tiles[0], /\/radar\/rainviewer\//);
});
