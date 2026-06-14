import { test, beforeEach, afterEach } from 'node:test';
import assert from 'node:assert/strict';
import { absolutizeStyleUrls } from './style-urls.js';

const ORIGIN = 'https://gw.example.com';

beforeEach(() => {
  globalThis.window = { location: { origin: ORIGIN } };
});

afterEach(() => {
  delete globalThis.window;
});

test('resolves a root-relative string sprite and glyphs to absolute', () => {
  const style = {
    sprite: '/api/maps/style/americana/sprites/sprite',
    glyphs: '/api/maps/style/americana/glyphs/{fontstack}/{range}.pbf',
  };
  absolutizeStyleUrls(style);
  assert.equal(style.sprite, `${ORIGIN}/api/maps/style/americana/sprites/sprite`);
  assert.equal(
    style.glyphs,
    `${ORIGIN}/api/maps/style/americana/glyphs/{fontstack}/{range}.pbf`,
  );
});

test('resolves the array form of sprite', () => {
  const style = {
    sprite: [
      { id: 'default', url: '/api/maps/style/americana/sprites/sprite' },
      { id: 'other', url: '/api/maps/style/other/sprites/sprite' },
    ],
  };
  absolutizeStyleUrls(style);
  assert.deepEqual(style.sprite, [
    { id: 'default', url: `${ORIGIN}/api/maps/style/americana/sprites/sprite` },
    { id: 'other', url: `${ORIGIN}/api/maps/style/other/sprites/sprite` },
  ]);
});

test('resolves source tilejson urls', () => {
  const style = {
    sources: {
      basemap: { type: 'vector', url: '/api/maps/style/tiles.json' },
      dem: { type: 'raster-dem', url: 'https://s3.amazonaws.com/dem.json' },
      tiled: { type: 'vector', tiles: ['gw-tile://{z}/{x}/{y}'] },
    },
  };
  absolutizeStyleUrls(style);
  assert.equal(style.sources.basemap.url, `${ORIGIN}/api/maps/style/tiles.json`);
  // Already-absolute and tiles-only sources are left untouched.
  assert.equal(style.sources.dem.url, 'https://s3.amazonaws.com/dem.json');
  assert.deepEqual(style.sources.tiled.tiles, ['gw-tile://{z}/{x}/{y}']);
});

test('leaves already-absolute and non-string fields untouched', () => {
  const style = {
    sprite: 'https://maps.nw5w.com/style/americana/sprites/sprite',
    glyphs: undefined,
    sources: { osm: { type: 'raster', tiles: ['https://tile/{z}/{x}/{y}.png'] } },
  };
  absolutizeStyleUrls(style);
  assert.equal(style.sprite, 'https://maps.nw5w.com/style/americana/sprites/sprite');
  assert.equal(style.glyphs, undefined);
});

test('is a no-op without a window (SSR) and tolerates null', () => {
  delete globalThis.window;
  const style = { sprite: '/api/maps/style/americana/sprites/sprite' };
  const out = absolutizeStyleUrls(style);
  assert.equal(out.sprite, '/api/maps/style/americana/sprites/sprite');
  assert.equal(absolutizeStyleUrls(null), null);
});
