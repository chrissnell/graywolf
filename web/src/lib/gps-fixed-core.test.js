import { test } from 'node:test';
import assert from 'node:assert/strict';
import { fixedCoordsFromPosition } from './gps-fixed-core.js';

test('fixedCoordsFromPosition fills lat, lon and alt from a GPS fix', () => {
  const pos = { valid: true, source: 'gps', lat: 35.0844, lon: -106.6504, alt_m: 1500, has_alt: true };
  assert.deepEqual(fixedCoordsFromPosition(pos), { lat: '35.0844', lon: '-106.6504', alt: '1500' });
});

test('fixedCoordsFromPosition leaves altitude unset when the fix has none', () => {
  const pos = { valid: true, source: 'gps', lat: 35.0844, lon: -106.6504, has_alt: false };
  assert.deepEqual(fixedCoordsFromPosition(pos), { lat: '35.0844', lon: '-106.6504', alt: null });
});

test('fixedCoordsFromPosition rejects the saved fixed coordinate reporting itself', () => {
  const pos = { valid: true, source: 'fixed', lat: 35.0844, lon: -106.6504, alt_m: 1500, has_alt: true };
  assert.equal(fixedCoordsFromPosition(pos), null);
});

test('fixedCoordsFromPosition rejects no position and an invalid fix', () => {
  assert.equal(fixedCoordsFromPosition({ valid: false, source: 'none' }), null);
  assert.equal(fixedCoordsFromPosition({ valid: false, source: 'gps', lat: 35, lon: -106 }), null);
});

test('fixedCoordsFromPosition tolerates missing or garbled input', () => {
  assert.equal(fixedCoordsFromPosition(null), null);
  assert.equal(fixedCoordsFromPosition(undefined), null);
  assert.equal(fixedCoordsFromPosition('nope'), null);
  assert.equal(fixedCoordsFromPosition({ valid: true, source: 'gps' }), null);
  assert.equal(fixedCoordsFromPosition({ valid: true, source: 'gps', lat: '35', lon: -106 }), null);
});
