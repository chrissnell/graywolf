import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountFrontsLayer, FRONT_LAYER_IDS, FRONT_WORLD_LAYER_IDS } from './fronts.js';

// Minimal MapLibre stand-in: records sources/layers, layout/paint edits, and
// the image registry. No DOM, so rasterizeSvg resolves null and addImage is
// never reached -- the layer add path is what we exercise here.
function fakeMap() {
  const sources = {}, layers = {}, images = {};
  // `order` mirrors MapLibre's layer array (later = rendered on top) so tests
  // can assert beforeId placement / z-order.
  const order = [];
  return {
    addSource: (id, s) => { sources[id] = { ...s }; },
    getSource: (id) => (sources[id] ? { setData: (d) => { sources[id].data = d; } } : undefined),
    addLayer: (l, beforeId) => {
      layers[l.id] = { ...l, paint: { ...(l.paint ?? {}) }, layout: { ...(l.layout ?? {}) } };
      const at = beforeId ? order.indexOf(beforeId) : -1;
      if (at >= 0) order.splice(at, 0, l.id); else order.push(l.id);
    },
    getLayer: (id) => layers[id],
    setLayoutProperty: (id, k, v) => { if (layers[id]) layers[id].layout[k] = v; },
    setPaintProperty: (id, k, v) => { if (layers[id]) layers[id].paint[k] = v; },
    removeLayer: (id) => { delete layers[id]; const i = order.indexOf(id); if (i >= 0) order.splice(i, 1); },
    removeSource: (id) => { delete sources[id]; },
    getStyle: () => ({ layers: [] }),
    hasImage: (id) => Boolean(images[id]),
    addImage: (id, img) => { images[id] = img; },
    _sources: sources, _layers: layers, _images: images, _order: order,
  };
}

test('FRONT_LAYER_IDS lists the four overlay layers', () => {
  assert.deepEqual(FRONT_LAYER_IDS, [
    'fronts-line',
    'fronts-pips',
    'fronts-centers',
    'fronts-center-labels',
  ]);
});

test('mount adds the source and all four layers behind the first symbol layer', () => {
  const map = fakeMap();
  mountFrontsLayer(map, { visible: true });
  assert.ok(map._sources.fronts, 'geojson source added');
  assert.equal(map._sources.fronts.type, 'geojson');
  for (const id of FRONT_LAYER_IDS) {
    assert.ok(map._layers[id], `${id} added`);
    assert.equal(map._layers[id].layout.visibility, 'visible');
  }
});

test('world layer ids exist and are distinct from the WPC layer ids', () => {
  assert.ok(FRONT_WORLD_LAYER_IDS.includes('fronts-world-line'));
  assert.ok(FRONT_WORLD_LAYER_IDS.includes('fronts-world-pips'));
  for (const id of FRONT_WORLD_LAYER_IDS) {
    assert.ok(!FRONT_LAYER_IDS.includes(id), `${id} must not collide with a WPC id`);
  }
});

test('mount adds the world source + layers alongside WPC, under one toggle', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  assert.ok(map._sources['fronts-world'], 'world geojson source added');
  for (const id of FRONT_WORLD_LAYER_IDS) {
    assert.ok(map._layers[id], `${id} added`);
  }
  // The single toggle hides both layer sets.
  layer.setVisible(false);
  for (const id of [...FRONT_LAYER_IDS, ...FRONT_WORLD_LAYER_IDS]) {
    assert.equal(map._layers[id].layout.visibility, 'none');
  }
});

test('world layers are inserted beneath all WPC layers (z-order)', () => {
  const map = fakeMap();
  mountFrontsLayer(map, { visible: true });
  const idx = (id) => map._order.indexOf(id);
  const worldMax = Math.max(...FRONT_WORLD_LAYER_IDS.map(idx));
  const wpcMin = Math.min(...FRONT_LAYER_IDS.map(idx));
  for (const id of [...FRONT_LAYER_IDS, ...FRONT_WORLD_LAYER_IDS]) {
    assert.ok(idx(id) >= 0, `${id} present in layer order`);
  }
  assert.ok(worldMax < wpcMin, 'every world layer renders beneath every WPC layer');
});

test('reload pushes data urls into both geojson sources', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  layer.reload();
  assert.match(String(map._sources.fronts.data), /\/fronts\/latest\.geojson$/);
  assert.match(String(map._sources['fronts-world'].data), /\/fronts\/world\/latest\.geojson$/);
});

test('setVisible(false) sets every front layer visibility to none', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  layer.setVisible(false);
  for (const id of FRONT_LAYER_IDS) {
    assert.equal(map._layers[id].layout.visibility, 'none');
  }
  layer.setVisible(true);
  for (const id of FRONT_LAYER_IDS) {
    assert.equal(map._layers[id].layout.visibility, 'visible');
  }
});

test('refresh re-adds dropped layers after a style swap', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  for (const k of Object.keys(map._sources)) delete map._sources[k];
  for (const k of Object.keys(map._layers)) delete map._layers[k];

  layer.refresh();
  assert.ok(map._sources.fronts, 'source re-added');
  assert.ok(map._sources['fronts-world'], 'world source re-added');
  for (const id of [...FRONT_LAYER_IDS, ...FRONT_WORLD_LAYER_IDS]) {
    assert.ok(map._layers[id], `${id} re-added`);
  }
});

test('reload pushes the data url back into the geojson source', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  layer.reload();
  assert.match(String(map._sources.fronts.data), /\/fronts\/latest\.geojson$/);
});

test('destroy removes every layer and the source', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  layer.destroy();
  for (const id of FRONT_LAYER_IDS) {
    assert.equal(map._layers[id], undefined);
  }
  assert.equal(map._sources.fronts, undefined);
});

test('destroy swallows errors when the map is already torn down', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  map.getLayer = () => { throw new TypeError('map removed'); };
  assert.doesNotThrow(() => layer.destroy());
});
