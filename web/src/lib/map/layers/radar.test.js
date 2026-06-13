import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountRadarLayer } from './radar.js';

// Minimal MapLibre stand-in: records sources/layers and the last setTiles call.
function fakeMap() {
  const sources = {}, layers = {};
  return {
    addSource: (id, s) => { sources[id] = { ...s }; },
    getSource: (id) => (sources[id] ? { setTiles: (t) => { sources[id].tiles = t; } } : undefined),
    addLayer: (l) => { layers[l.id] = l; },
    getLayer: (id) => layers[id],
    setLayoutProperty: () => {},
    setPaintProperty: () => {},
    removeLayer: (id) => { delete layers[id]; },
    removeSource: (id) => { delete sources[id]; },
    getStyle: () => ({ layers: [] }),
    _sources: sources, _layers: layers,
  };
}

test('mounts the vector source already cache-busted to the current bucket', () => {
  const map = fakeMap();
  mountRadarLayer(map, { visible: true, opacity: 0.6, now: () => 300000 });
  assert.equal(map._sources['radar-tiles'].type, 'vector');
  assert.match(map._sources['radar-tiles'].tiles[0], /\/radar\/\{z\}\/\{x\}\/\{y\}\.pbf\?v=1$/);
  assert.equal(map._layers['radar-fill'].type, 'fill');
});

test('refresh busts the tile cache only when the time bucket rolls over', () => {
  let nowMs = 0;
  const map = fakeMap();
  const layer = mountRadarLayer(map, { visible: true, opacity: 0.6, now: () => nowMs });
  const initial = map._sources['radar-tiles'].tiles[0];
  assert.match(initial, /\?v=0$/);

  layer.refresh();                       // same bucket -> no new template
  assert.equal(map._sources['radar-tiles'].tiles[0], initial);

  nowMs = 300000;                        // next 5-minute bucket
  layer.refresh();
  assert.match(map._sources['radar-tiles'].tiles[0], /\?v=1$/);
  assert.notEqual(map._sources['radar-tiles'].tiles[0], initial);
});
