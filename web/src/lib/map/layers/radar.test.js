import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountRadarLayer } from './radar.js';

// Minimal MapLibre stand-in: records sources/layers and counts setTiles calls.
// It has no event interface (map.on), so the layer's cross-fade fires
// synchronously -- the fade timing is a renderer concern, but the buffer
// swap / tile-template logic is deterministic and is what we assert here.
function fakeMap() {
  const sources = {}, layers = {};
  let setTilesCalls = 0;
  return {
    addSource: (id, s) => { sources[id] = { ...s }; },
    getSource: (id) => (sources[id] ? { setTiles: (t) => { sources[id].tiles = t; setTilesCalls++; } } : undefined),
    addLayer: (l) => { layers[l.id] = l; },
    getLayer: (id) => layers[id],
    setLayoutProperty: () => {},
    setPaintProperty: () => {},
    removeLayer: (id) => { delete layers[id]; },
    removeSource: (id) => { delete sources[id]; },
    getStyle: () => ({ layers: [] }),
    _sources: sources, _layers: layers,
    get _setTilesCalls() { return setTilesCalls; },
  };
}

const frameUrl = (ts) => `https://maps.nw5w.com/radar/${ts}/{z}/{x}/{y}.pbf`;

test('per-frame vector overlay adds no source until a frame ts is set', () => {
  const map = fakeMap();
  mountRadarLayer(map, { visible: true, opacity: 0.6 });
  // No manifest frame yet -> overlay is absent (mirrors the worker's pre-manifest 503).
  assert.equal(map._sources['radar-tiles-a'], undefined);
  assert.equal(map._sources['radar-tiles-b'], undefined);
  assert.equal(map._layers['radar-fill-a'], undefined);
});

test('an initial frameTs seeds the first buffer at mount (no setFrameTs needed)', () => {
  // The manifest poll can resolve before the basemap style loads, so a frame ts
  // is often known by the time the layer mounts. Passing it as a mount option
  // (like visible/opacity) must render the overlay immediately -- otherwise it
  // stays blank until the next index change (e.g. pressing Play).
  const map = fakeMap();
  mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020000 });
  assert.equal(map._sources['radar-tiles-a'].type, 'vector');
  assert.deepEqual(map._sources['radar-tiles-a'].tiles, [frameUrl(1750020000)]);
  assert.equal(map._layers['radar-fill-a'].type, 'fill');
  // Layers carry an opacity transition so frame swaps animate (the cross-fade).
  assert.ok(map._layers['radar-fill-a'].paint['fill-opacity-transition']);
});

test('setFrameTs cross-fades into the alternate buffer on each advance', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6 });

  // First frame -> buffer 'a'.
  layer.setFrameTs(1750020000);
  assert.equal(map._sources['radar-tiles-a'].type, 'vector');
  assert.deepEqual(map._sources['radar-tiles-a'].tiles, [frameUrl(1750020000)]);
  assert.equal(map._layers['radar-fill-a'].type, 'fill');

  // Next frame loads into the idle buffer 'b' (so the old frame can fade out
  // while the new one fades in) rather than overwriting buffer 'a'.
  layer.setFrameTs(1750019700);
  assert.deepEqual(map._sources['radar-tiles-b'].tiles, [frameUrl(1750019700)]);
  assert.deepEqual(map._sources['radar-tiles-a'].tiles, [frameUrl(1750020000)]);

  // Third frame ping-pongs back to buffer 'a', reusing its source via setTiles.
  layer.setFrameTs(1750019400);
  assert.deepEqual(map._sources['radar-tiles-a'].tiles, [frameUrl(1750019400)]);
});

test('setFrameTs ignores a repeated ts', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6 });
  layer.setFrameTs(1750020000); // first ts: added via addSource (no setTiles)
  const before = map._setTilesCalls;
  layer.setFrameTs(1750020000); // same ts: no-op
  assert.equal(map._setTilesCalls, before);
});

test('setRegion to world builds RainViewer raster; back to US restores the frame', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, region: 'us', now: () => 0 });
  layer.setFrameTs(1750020000);
  assert.equal(map._sources['radar-tiles-a'].type, 'vector');

  layer.setRegion('world');
  assert.equal(map._layers['radar-fill-a'], undefined);
  assert.equal(map._sources['radar-tiles-a'].type, 'raster');
  assert.match(map._sources['radar-tiles-a'].tiles[0], /\/radar\/rainviewer\//);

  // Switching back restores the US vector overlay at the last-known frame.
  layer.setRegion('us');
  assert.equal(map._sources['radar-tiles-a'].type, 'vector');
  assert.deepEqual(map._sources['radar-tiles-a'].tiles, [frameUrl(1750020000)]);
  assert.ok(map._layers['radar-fill-a']);
});

test('world raster cross-fades to the new frame on a time-bucket rollover', () => {
  let nowMs = 0;
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, region: 'world', now: () => nowMs });
  assert.match(map._sources['radar-tiles-a'].tiles[0], /\?v=0$/);

  layer.refresh(); // same bucket -> unchanged, still on buffer 'a'
  assert.match(map._sources['radar-tiles-a'].tiles[0], /\?v=0$/);
  assert.equal(map._sources['radar-tiles-b'], undefined);

  nowMs = 300000; // next 5-minute bucket -> fade into buffer 'b'
  layer.refresh();
  assert.match(map._sources['radar-tiles-b'].tiles[0], /\?v=1$/);
});

test('destroy swallows errors when the map is already torn down', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, now: () => 0 });
  // After map.remove(), MapLibre's getLayer throws because internal state is
  // gone; teardown order can run a layer's destroy() against a removed map.
  map.getLayer = () => { throw new TypeError("Cannot read properties of undefined (reading 'getLayer')"); };
  assert.doesNotThrow(() => layer.destroy());
});
