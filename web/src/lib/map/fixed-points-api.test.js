import test from 'node:test';
import assert from 'node:assert/strict';
import { fixedPointFromApi, fixedPointToApi } from './fixed-points-api.js';

test('fixedPointFromApi maps server fields to the layer shape', () => {
  const p = fixedPointFromApi({
    id: 7, name: 'Aid 3', symbol_table: '/', symbol: 'a', overlay: '',
    latitude: 37.5, longitude: -122.0,
  });
  assert.deepEqual(p, {
    id: 7, name: 'Aid 3', table: '/', symbol: 'a', overlay: '', lat: 37.5, lon: -122.0,
  });
});

test('fixedPointFromApi drops malformed records (null)', () => {
  assert.equal(fixedPointFromApi(null), null);
  assert.equal(fixedPointFromApi({ id: 1, name: 'x', latitude: 'nope', longitude: 2 }), null);
});

test('fixedPointToApi maps the dialog result to the request body', () => {
  const body = fixedPointToApi({ name: 'Aid 3', table: '/', symbol: 'a', overlay: '', lat: 37.5, lon: -122.0 });
  assert.deepEqual(body, {
    name: 'Aid 3', symbol_table: '/', symbol: 'a', overlay: '', latitude: 37.5, longitude: -122.0,
  });
});
