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
  vectorTileUrl,
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
  assert.match(p.source.tiles[0], /radar\/\{z\}\/\{x\}\/\{y\}\.pbf$/);
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

test('vectorTileUrl is the cycle-less .pbf route and appends ?v= when busting', () => {
  assert.equal(vectorTileUrl(), 'https://maps.nw5w.com/radar/{z}/{x}/{y}.pbf');
  assert.equal(vectorTileUrl(42), 'https://maps.nw5w.com/radar/{z}/{x}/{y}.pbf?v=42');
});

test('vector provider exposes a cacheBust that swaps in a ?v= template', () => {
  const p = radarProvider(RADAR_BACKEND_VECTOR);
  assert.equal(typeof p.cacheBust, 'function');
  assert.deepEqual(p.cacheBust(7), ['https://maps.nw5w.com/radar/{z}/{x}/{y}.pbf?v=7']);
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
