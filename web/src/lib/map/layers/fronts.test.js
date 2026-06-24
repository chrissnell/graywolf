import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountFrontsLayer, FRONT_LAYER_IDS, smoothLine } from './fronts.js';

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
  return {
    addSource: (id, s) => { sources[id] = { ...s }; },
    getSource: (id) => (sources[id] ? { setData: (d) => { sources[id].data = d; } } : undefined),
    addLayer: (l) => { layers[l.id] = { ...l, paint: { ...(l.paint ?? {}) }, layout: { ...(l.layout ?? {}) } }; },
    getLayer: (id) => layers[id],
    setLayoutProperty: (id, k, v) => { if (layers[id]) layers[id].layout[k] = v; },
    setPaintProperty: (id, k, v) => { if (layers[id]) layers[id].paint[k] = v; },
    removeLayer: (id) => { delete layers[id]; },
    removeSource: (id) => { delete sources[id]; },
    getStyle: () => ({ layers: [] }),
    hasImage: (id) => Boolean(images[id]),
    addImage: (id, img) => { images[id] = img; },
    _sources: sources, _layers: layers, _images: images,
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
  for (const id of FRONT_LAYER_IDS) {
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
  const front = pushed.features[0];
  // The front line is densified (more points than the 4 raw), endpoints kept.
  assert.ok(front.geometry.coordinates.length > 4, 'line densified');
  assert.deepEqual(front.geometry.coordinates[0], [0, 0]);
  assert.deepEqual(front.geometry.coordinates.at(-1), [9, 2]);
  // The pressure-center Point is passed through untouched.
  assert.deepEqual(pushed.features[1].geometry.coordinates, [4, 4]);
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
