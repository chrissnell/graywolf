import test from 'node:test';
import assert from 'node:assert/strict';
import { mountRadarLayer } from './radar.js';

function fakeMap() {
  const sources = {}, layers = {}, paint = {}, layout = {};
  return {
    addSource: (id, s) => { sources[id] = s; },
    getSource: (id) => sources[id] ? { setTiles: (t) => { sources[id].tiles = t; } } : undefined,
    addLayer: (l) => { layers[l.id] = l; },
    getLayer: (id) => layers[id],
    setPaintProperty: (id, k, v) => { paint[`${id}.${k}`] = v; },
    setLayoutProperty: (id, k, v) => { layout[`${id}.${k}`] = v; },
    removeLayer: (id) => { delete layers[id]; },
    removeSource: (id) => { delete sources[id]; },
    _sources: sources, _layers: layers, _paint: paint, _layout: layout,
  };
}

test('adds a vector source and a fill layer', async () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { fetchLatest: async () => ({ ts: 'T1' }) });
  await layer.refresh();
  assert.equal(map._sources.radar.type, 'vector');
  assert.equal(map._layers['radar-fill'].type, 'fill');
});

test('setOpacity drives fill-opacity', async () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { fetchLatest: async () => ({ ts: 'T1' }) });
  await layer.refresh();
  layer.setOpacity(0.5);
  assert.equal(map._paint['radar-fill.fill-opacity'], 0.5);
});

test('setVisible toggles layout visibility', async () => {
  const map = fakeMap();
  const layer = mountRadarLayer(map, { fetchLatest: async () => ({ ts: 'T1' }) });
  await layer.refresh();
  layer.setVisible(false);
  assert.equal(map._layout['radar-fill.visibility'], 'none');
});
