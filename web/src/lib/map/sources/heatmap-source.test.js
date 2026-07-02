import { test } from 'node:test';
import assert from 'node:assert/strict';
import { heatmapUrl, normalizeFeatureWeights } from './heatmap-source.js';

test('heatmapUrl builds bbox + timerange query', () => {
  const url = heatmapUrl({ swLat: 30, swLon: -100, neLat: 40, neLon: -90 }, 3600);
  assert.equal(url, '/api/heatmap?bbox=30.00000%2C-100.00000%2C40.00000%2C-90.00000&timerange=3600');
});

test('normalizeFeatureWeights adds w = count/maxCount', () => {
  const feats = [
    { type: 'Feature', geometry: { type: 'Point', coordinates: [-95, 35] }, properties: { count: 3 } },
    { type: 'Feature', geometry: { type: 'Point', coordinates: [-96, 36] }, properties: { count: 1 } },
  ];
  const out = normalizeFeatureWeights(feats, 3);
  assert.equal(out[0].properties.w, 1);
  assert.equal(out[1].properties.w, 1 / 3);
  // original count preserved
  assert.equal(out[0].properties.count, 3);
});

test('normalizeFeatureWeights is safe when maxCount is 0', () => {
  const feats = [{ type: 'Feature', geometry: { type: 'Point', coordinates: [0, 0] }, properties: { count: 0 } }];
  const out = normalizeFeatureWeights(feats, 0);
  assert.equal(out[0].properties.w, 0);
});

test('normalizeFeatureWeights tolerates empty input', () => {
  assert.deepEqual(normalizeFeatureWeights([], 0), []);
  assert.deepEqual(normalizeFeatureWeights(undefined, 5), []);
});
