import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountRadarLayer } from './radar.js';

// Minimal MapLibre stand-in: records sources/layers, paint/layout edits, and
// counts setTiles calls (the per-frame loop must never call it).
function fakeMap() {
  const sources = {}, layers = {};
  let setTilesCalls = 0;
  return {
    addSource: (id, s) => { sources[id] = { ...s }; },
    getSource: (id) => (sources[id] ? { setTiles: (t) => { sources[id].tiles = t; setTilesCalls++; } } : undefined),
    addLayer: (l) => { layers[l.id] = { ...l, paint: { ...(l.paint ?? {}) }, layout: { ...(l.layout ?? {}) } }; },
    getLayer: (id) => layers[id],
    setLayoutProperty: (id, k, v) => { if (layers[id]) layers[id].layout[k] = v; },
    setPaintProperty: (id, k, v) => { if (layers[id]) layers[id].paint[k] = v; },
    removeLayer: (id) => { delete layers[id]; },
    removeSource: (id) => { delete sources[id]; },
    getStyle: () => ({ layers: [] }),
    _sources: sources, _layers: layers,
    get _setTilesCalls() { return setTilesCalls; },
  };
}

const frameUrl = (ts) => `https://maps.nw5w.com/radar/${ts}/{z}/{x}/{y}.pbf`;
const srcId = (ts) => `radar-tiles-${ts}`;
const layerId = (ts) => `radar-fill-${ts}`;

test('per-frame vector overlay adds nothing until a frame is known', () => {
  const map = fakeMap();
  mountRadarLayer(map, { visible: true, opacity: 0.6 });
  // No manifest frame yet -> overlay is absent (mirrors the worker's pre-manifest 503).
  assert.equal(Object.keys(map._sources).length, 0);
  assert.equal(Object.keys(map._layers).length, 0);
});

test('an initial frameTs seeds the per-frame source at mount (no setFrameTs needed)', () => {
  // The manifest poll can resolve before the basemap style loads, so a frame ts
  // is often known by the time the layer mounts. Passing it as a mount option
  // (like visible/opacity) must render the overlay immediately.
  const map = fakeMap();
  mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020000 });
  assert.equal(map._sources[srcId(1750020000)].type, 'vector');
  assert.deepEqual(map._sources[srcId(1750020000)].tiles, [frameUrl(1750020000)]);
  assert.equal(map._layers[layerId(1750020000)].type, 'fill');
  // The seeded frame is the visible one, so it paints at full opacity.
  assert.equal(map._layers[layerId(1750020000)].paint['fill-opacity'], 0.6);
});

test('setFrames preloads every frame, each its own cached source at opacity 0', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020600 });
  layer.setFrames([1750020000, 1750020300, 1750020600]);
  // One source + one layer per frame, each pointing at its immutable tile URL.
  for (const ts of [1750020000, 1750020300, 1750020600]) {
    assert.equal(map._sources[srcId(ts)].type, 'vector');
    assert.deepEqual(map._sources[srcId(ts)].tiles, [frameUrl(ts)]);
    assert.equal(map._layers[layerId(ts)].type, 'fill');
  }
  // Only the current frame is visible; the rest are preloaded at opacity 0.
  assert.equal(map._layers[layerId(1750020600)].paint['fill-opacity'], 0.6);
  assert.equal(map._layers[layerId(1750020000)].paint['fill-opacity'], 0);
  assert.equal(map._layers[layerId(1750020300)].paint['fill-opacity'], 0);
  // Preloading never swaps tiles on an existing source.
  assert.equal(map._setTilesCalls, 0);
});

test('advancing the loop toggles opacity without refetching tiles', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020000 });
  layer.setFrames([1750020000, 1750020300, 1750020600]);
  const sourceCountBefore = Object.keys(map._sources).length;

  layer.setFrameTs(1750020300); // advance one frame
  assert.equal(map._layers[layerId(1750020000)].paint['fill-opacity'], 0);
  assert.equal(map._layers[layerId(1750020300)].paint['fill-opacity'], 0.6);

  layer.setFrameTs(1750020600); // and again
  assert.equal(map._layers[layerId(1750020300)].paint['fill-opacity'], 0);
  assert.equal(map._layers[layerId(1750020600)].paint['fill-opacity'], 0.6);

  // Wrap back to the oldest -- still cached, still no refetch.
  layer.setFrameTs(1750020000);
  assert.equal(map._layers[layerId(1750020000)].paint['fill-opacity'], 0.6);

  // The whole loop reused the preloaded sources: no setTiles, no new sources.
  assert.equal(map._setTilesCalls, 0);
  assert.equal(Object.keys(map._sources).length, sourceCountBefore);
});

test('setFrameTs ignores a repeated ts', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020000 });
  const layerCountBefore = Object.keys(map._layers).length;
  layer.setFrameTs(1750020000); // same ts: no-op
  assert.equal(Object.keys(map._layers).length, layerCountBefore);
});

test('setFrameTs mounts an unknown frame on demand (manifest race)', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6 });
  // No frames preloaded yet; the index effect fires first.
  layer.setFrameTs(1750020000);
  assert.equal(map._sources[srcId(1750020000)].type, 'vector');
  assert.equal(map._layers[layerId(1750020000)].paint['fill-opacity'], 0.6);
});

test('refresh re-adds every frame after a basemap style swap (visible frame at opacity)', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020300 });
  layer.setFrames([1750020000, 1750020300, 1750020600]);
  // A setStyle() rebuilds the style and drops user-added sources/layers.
  for (const k of Object.keys(map._sources)) delete map._sources[k];
  for (const k of Object.keys(map._layers)) delete map._layers[k];

  layer.refresh(); // mirrors the per-tick refresh() LiveMapV2 drives
  for (const ts of [1750020000, 1750020300, 1750020600]) {
    assert.ok(map._sources[srcId(ts)], `frame ${ts} source re-added`);
    assert.ok(map._layers[layerId(ts)], `frame ${ts} layer re-added`);
  }
  // The visible frame comes back at full opacity; the rest at 0.
  assert.equal(map._layers[layerId(1750020300)].paint['fill-opacity'], 0.6);
  assert.equal(map._layers[layerId(1750020000)].paint['fill-opacity'], 0);
});

test('setFrames tears down frames that rolled off the manifest, never the visible one', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020300 });
  layer.setFrames([1750020000, 1750020300, 1750020600]);
  // Manifest slides forward: oldest drops, a newer frame appears. The visible
  // frame (1750020300) is retained even though it left the new list.
  layer.setFrames([1750020600, 1750020900]);
  assert.equal(map._sources[srcId(1750020000)], undefined);
  assert.equal(map._layers[layerId(1750020000)], undefined);
  assert.ok(map._sources[srcId(1750020300)], 'visible frame is retained');
  assert.ok(map._sources[srcId(1750020600)]);
  assert.ok(map._sources[srcId(1750020900)]);
});

test('setOpacity drives only the visible frame; setVisible toggles all frames', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020300 });
  layer.setFrames([1750020000, 1750020300]);

  layer.setOpacity(0.3);
  assert.equal(map._layers[layerId(1750020300)].paint['fill-opacity'], 0.3); // visible frame
  assert.equal(map._layers[layerId(1750020000)].paint['fill-opacity'], 0); // stays hidden

  layer.setVisible(false);
  assert.equal(map._layers[layerId(1750020000)].layout.visibility, 'none');
  assert.equal(map._layers[layerId(1750020300)].layout.visibility, 'none');
  layer.setVisible(true);
  assert.equal(map._layers[layerId(1750020300)].layout.visibility, 'visible');
});

test('setRegion to world builds RainViewer raster; back to US restores the frame', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, region: 'us', now: () => 0 });
  layer.setFrameTs(1750020000);
  assert.equal(map._sources[srcId(1750020000)].type, 'vector');

  layer.setRegion('world');
  assert.equal(map._layers[layerId(1750020000)], undefined);
  assert.equal(map._sources[srcId(1750020000)], undefined);
  assert.equal(map._sources['radar-tiles'].type, 'raster');
  assert.match(map._sources['radar-tiles'].tiles[0], /\/radar\/rainviewer\//);

  // Switching back restores the US vector overlay at the last-known frame.
  layer.setRegion('us');
  assert.equal(map._sources[srcId(1750020000)].type, 'vector');
  assert.deepEqual(map._sources[srcId(1750020000)].tiles, [frameUrl(1750020000)]);
  assert.ok(map._layers[layerId(1750020000)]);
});

test('world raster still cache-busts on a time-bucket rollover', () => {
  let nowMs = 0;
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, region: 'world', now: () => nowMs });
  const initial = map._sources['radar-tiles'].tiles[0];
  assert.match(initial, /\?v=0$/);

  layer.refresh(); // same bucket -> unchanged
  assert.equal(map._sources['radar-tiles'].tiles[0], initial);

  nowMs = 300000; // next 5-minute bucket
  layer.refresh();
  assert.match(map._sources['radar-tiles'].tiles[0], /\?v=1$/);
});

test('destroy swallows errors when the map is already torn down', () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, frameTs: 1750020000, now: () => 0 });
  // After map.remove(), MapLibre's getLayer throws because internal state is
  // gone; teardown order can run a layer's destroy() against a removed map.
  map.getLayer = () => { throw new TypeError("Cannot read properties of undefined (reading 'getLayer')"); };
  assert.doesNotThrow(() => layer.destroy());
});
