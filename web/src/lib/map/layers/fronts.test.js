import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountFrontsLayer, FRONT_LAYER_IDS, FRONT_WORLD_LAYER_IDS, smoothLine, pipSpans } from './fronts.js';

// Sharpest turn (radians) between consecutive segments of a polyline.
function maxTurn(pts) {
  let m = 0;
  for (let i = 1; i < pts.length - 1; i++) {
    const a = Math.atan2(pts[i][1] - pts[i - 1][1], pts[i][0] - pts[i - 1][0]);
    const b = Math.atan2(pts[i + 1][1] - pts[i][1], pts[i + 1][0] - pts[i][0]);
    let d = Math.abs(b - a);
    if (d > Math.PI) d = 2 * Math.PI - d;
    m = Math.max(m, d);
  }
  return m;
}

test('smoothLine rounds corners (sharpest turn shrinks) and keeps endpoints', () => {
  const coarse = [[0, 0], [2, 0], [2, 2], [4, 2]]; // two ~90deg corners
  const smooth = smoothLine(coarse);
  assert.ok(smooth.length > coarse.length, 'densified');
  assert.deepEqual(smooth[0], [0, 0]);
  assert.deepEqual(smooth.at(-1), [4, 2]);
  assert.ok(maxTurn(smooth) < maxTurn(coarse) * 0.6, 'sharpest turn meaningfully reduced');
});

test('smoothLine passes through lines too short to curve', () => {
  assert.deepEqual(smoothLine([[0, 0], [1, 1]]), [[0, 0], [1, 1]]);
});

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

test('FRONT_LAYER_IDS lists the overlay layers (incl. the stationary set)', () => {
  assert.deepEqual(FRONT_LAYER_IDS, [
    'fronts-line',
    'fronts-stationary-line',
    'fronts-stationary-dash',
    'fronts-pips',
    'fronts-stationary-pips',
    'fronts-centers',
    'fronts-center-labels',
  ]);
});

test('mount adds the source and all layers behind the first symbol layer', () => {
  const map = fakeMap();
  mountFrontsLayer(map, { visible: true });
  assert.ok(map._sources.fronts, 'geojson source added');
  assert.equal(map._sources.fronts.type, 'geojson');
  for (const id of FRONT_LAYER_IDS) {
    assert.ok(map._layers[id], `${id} added`);
    assert.equal(map._layers[id].layout.visibility, 'visible');
  }
});

test('stationary fronts render via their own dedicated layers, not the base ones', () => {
  // Proper WMO stationary symbology: a two-tone line + alternating cold/warm
  // pips. The base line and the single-type pip layer must EXCLUDE stationary
  // (it would double-draw / mis-color), and the dedicated stationary layers
  // must filter to ONLY stationary.
  const map = fakeMap();
  mountFrontsLayer(map, { visible: true });
  const lineFilter = JSON.stringify(map._layers['fronts-line'].filter);
  const pipFilter = JSON.stringify(map._layers['fronts-pips'].filter);
  assert.match(lineFilter, /"!=".*"front_type".*"stationary"/s, 'base line excludes stationary');
  assert.match(pipFilter, /"!=".*"stationary"/s, 'base pips exclude stationary');
  for (const id of ['fronts-stationary-line', 'fronts-stationary-dash', 'fronts-stationary-pips']) {
    const f = JSON.stringify(map._layers[id].filter);
    assert.match(f, /"==".*"front_type".*"stationary"/s, `${id} filters to stationary only`);
  }
  // The dashed overlay is what creates the alternating red/blue line.
  assert.ok(map._layers['fronts-stationary-dash'].paint['line-dasharray'], 'dash overlay present');
  assert.equal(map._layers['fronts-stationary-pips'].layout['icon-image'], 'front-stationary');
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

test('setWorldData smooths and pushes the object into the world source', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  const raw = {
    type: 'FeatureCollection',
    features: [
      { type: 'Feature', properties: { feature: 'front', front_type: 'cold' },
        geometry: { type: 'LineString', coordinates: [[0, 0], [3, 1], [6, 0], [9, 2]] } },
    ],
  };
  layer.setWorldData(raw);
  const pushed = map._sources['fronts-world'].data;
  assert.equal(pushed.type, 'FeatureCollection');
  const front = pushed.features.find((f) => f.properties.feature === 'front');
  assert.ok(front.geometry.coordinates.length > 4, 'world line densified');
  assert.ok(pushed.features.some((f) => f.properties.feature === 'pipline'), 'world pipline emitted');
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

test('setData smooths front lines and pushes the object into the source', () => {
  const map = fakeMap();
  const layer = mountFrontsLayer(map, { visible: true });
  const raw = {
    type: 'FeatureCollection',
    features: [
      { type: 'Feature', properties: { feature: 'front', front_type: 'cold' },
        geometry: { type: 'LineString', coordinates: [[0, 0], [3, 1], [6, 0], [9, 2]] } },
      { type: 'Feature', properties: { feature: 'center', kind: 'H', pressure_mb: 1020 },
        geometry: { type: 'Point', coordinates: [4, 4] } },
    ],
  };
  layer.setData(raw);
  const pushed = map._sources.fronts.data;
  assert.equal(pushed.type, 'FeatureCollection');
  const front = pushed.features.find((f) => f.properties.feature === 'front');
  // The front line is densified (more points than the 4 raw), endpoints kept.
  assert.ok(front.geometry.coordinates.length > 4, 'line densified');
  assert.deepEqual(front.geometry.coordinates[0], [0, 0]);
  assert.deepEqual(front.geometry.coordinates.at(-1), [9, 2]);
  // A companion pipline (gentle-span geometry the pips follow) is emitted.
  const pipline = pushed.features.find((f) => f.properties.feature === 'pipline');
  assert.ok(pipline, 'pipline feature emitted');
  assert.equal(pipline.geometry.type, 'MultiLineString');
  // The pressure-center Point is passed through untouched.
  const center = pushed.features.find((f) => f.properties.feature === 'center');
  assert.deepEqual(center.geometry.coordinates, [4, 4]);
});

test('pipSpans keeps gentle lines whole but cuts out a tight hairpin', () => {
  // A nearly-straight line is one span, unbroken.
  const gentle = [[0, 0], [1, 0.02], [2, 0], [3, 0.02], [4, 0]];
  assert.equal(pipSpans(gentle).length, 1);
  // A hairpin (doubles back) gets the tight section excluded, so pips won't be
  // placed across the bend -- either split into spans or dropped entirely there.
  const hairpin = [];
  for (let i = 0; i <= 20; i++) hairpin.push([i * 0.05, 0]); // run out
  for (let i = 1; i <= 20; i++) hairpin.push([1 - i * 0.05, 0.04]); // sharp U back
  const spans = pipSpans(hairpin);
  const totalPts = spans.reduce((n, s) => n + s.length, 0);
  assert.ok(totalPts < hairpin.length, 'tight bend vertices were excluded from pip spans');
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
