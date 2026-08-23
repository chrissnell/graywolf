import { test } from 'node:test';
import assert from 'node:assert/strict';
import { mountHoverPathLayer } from './hover-path.js';

// Minimal MapLibre stand-in: records source data set via setData so tests can
// assert what the hover-path layer drew. No DOM, no GL.
function fakeMap() {
  const sources = {}, layers = {};
  return {
    addSource: (id, s) => { sources[id] = { ...s }; },
    getSource: (id) => (sources[id] ? { setData: (d) => { sources[id].data = d; } } : undefined),
    addLayer: (l) => { layers[l.id] = l; },
    getLayer: (id) => layers[id],
    setLayoutProperty: () => {},
    removeLayer: (id) => { delete layers[id]; },
    removeSource: (id) => { delete sources[id]; },
    _sources: sources,
  };
}

const PATH_SRC = 'gw-hover-path';
const NODES_SRC = 'gw-hover-path-nodes';

function pathCoords(map) {
  const f = map._sources[PATH_SRC].data.features;
  return f.length ? f[0].geometry.coordinates : null;
}
function nodeCount(map) {
  return map._sources[NODES_SRC].data.features.length;
}

test('draws the signal path through H-bit digis with known positions', () => {
  const map = fakeMap();
  const layer = mountHoverPathLayer(map, () => null);
  layer.show({
    callsign: 'W2XYZ',
    via: 'is',
    path: ['DIGI1*', 'WIDE2-1'],
    path_positions: [[36.5, -95.5], [0, 0]],
    positions: [{ lat: 35.5, lon: -95.5 }],
  });
  // station -> DIGI1; [lat,lon] pairs flipped to [lon,lat]
  assert.deepEqual(pathCoords(map), [[-95.5, 35.5], [-95.5, 36.5]]);
  assert.equal(nodeCount(map), 1);
});

test('extends an RF station path to the own position', () => {
  const map = fakeMap();
  const layer = mountHoverPathLayer(map, () => ({ lat: 36, lon: -96 }));
  layer.show({ callsign: 'RF1', via: 'rf', path: [], path_positions: [], positions: [{ lat: 35, lon: -95 }] });
  assert.deepEqual(pathCoords(map), [[-95, 35], [-96, 36]]);
});

test('falls back to the beacon trail when no signal path is drawable (GH #506)', () => {
  const map = fakeMap();
  // No own position and a direct RF station with no digis: the signal path
  // is a single point, so the trail fallback should kick in.
  const layer = mountHoverPathLayer(map, () => null);
  layer.show({
    callsign: 'MOBILE',
    via: 'rf',
    positions: [
      { lat: 35.2, lon: -95.2 }, // newest
      { lat: 35.1, lon: -95.1 },
      { lat: 35.0, lon: -95.0 }, // oldest
    ],
  });
  // Oldest-first line through every beacon, plus a node per fix.
  assert.deepEqual(pathCoords(map), [[-95.0, 35.0], [-95.1, 35.1], [-95.2, 35.2]]);
  assert.equal(nodeCount(map), 3);
});

test('collapses consecutive duplicate beacons in the trail fallback', () => {
  const map = fakeMap();
  const layer = mountHoverPathLayer(map, () => null);
  layer.show({
    callsign: 'STILL',
    via: 'is',
    positions: [
      { lat: 35.0, lon: -95.0 },
      { lat: 35.0, lon: -95.0 },
      { lat: 35.1, lon: -95.1 },
    ],
  });
  assert.deepEqual(pathCoords(map), [[-95.1, 35.1], [-95.0, 35.0]]);
});

test('drops the line but keeps a single node when only one fix is drawable', () => {
  const map = fakeMap();
  const layer = mountHoverPathLayer(map, () => null);
  // Seed a line, then show a station that can draw no path: the line clears
  // but a lone node stays on the station so the hover is still acknowledged.
  layer.show({ callsign: 'A', via: 'rf', positions: [{ lat: 1, lon: 1 }, { lat: 2, lon: 2 }] });
  layer.show({ callsign: 'STATIONARY', via: 'is', positions: [{ lat: 35, lon: -95 }] });
  assert.equal(pathCoords(map), null);
  assert.equal(nodeCount(map), 1);
  assert.deepEqual(map._sources[NODES_SRC].data.features[0].geometry.coordinates, [-95, 35]);
});
