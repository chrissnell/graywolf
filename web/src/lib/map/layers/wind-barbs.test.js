import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildWindBarb, hasWindBarb, quantizeKnots } from './wind-barb-glyph.js';

const count = (svg, frag) => svg.split(frag).length - 1;

test('missing data renders nothing', () => {
  assert.equal(buildWindBarb(null, 90), '');
  assert.equal(buildWindBarb(10, null), '');
  assert.equal(buildWindBarb(NaN, 90), '');
});

test('calm wind renders the open ring, no staff', () => {
  const svg = buildWindBarb(0, 0);
  assert.match(svg, /wb-calm/);
  assert.equal(count(svg, 'wb-staff'), 0);
});

test('sub-2.5kt rounds to calm', () => {
  // 2 mph ≈ 1.7 kt → rounds to 0
  assert.match(buildWindBarb(2, 180), /wb-calm/);
});

test('a single half barb is drawn and inset from the tip', () => {
  // 6 mph ≈ 5.2 kt → 5 kt → one half barb, no full, no pennant
  const svg = buildWindBarb(6, 270);
  assert.equal(count(svg, 'wb-pennant'), 0);
  assert.equal(count(svg, 'wb-barb'), 1);
  assert.equal(count(svg, 'wb-staff'), 1);
});

test('10 kt is one full barb', () => {
  // 11.5 mph ≈ 9.99 kt → 10 kt
  const svg = buildWindBarb(11.5, 0);
  assert.equal(count(svg, 'wb-barb'), 1);
});

test('15 kt is one full + one half barb', () => {
  // 17.3 mph ≈ 15.03 kt → 15 kt
  const svg = buildWindBarb(17.3, 0);
  assert.equal(count(svg, 'wb-barb'), 2);
  assert.equal(count(svg, 'wb-pennant'), 0);
});

test('50 kt is one pennant', () => {
  // 57.6 mph ≈ 50.05 kt → 50 kt
  const svg = buildWindBarb(57.6, 0);
  assert.equal(count(svg, 'wb-pennant'), 1);
  assert.equal(count(svg, 'wb-barb'), 0);
});

test('65 kt is one pennant + one full + one half', () => {
  // 74.8 mph ≈ 65.0 kt
  const svg = buildWindBarb(74.8, 0);
  assert.equal(count(svg, 'wb-pennant'), 1);
  assert.equal(count(svg, 'wb-barb'), 2);
});

test('direction sets the group rotation', () => {
  assert.match(buildWindBarb(20, 45), /rotate\(45\)/);
  assert.match(buildWindBarb(20, 215), /rotate\(215\)/);
});

test('quantizeKnots rounds mph to the nearest 5 kt', () => {
  assert.equal(quantizeKnots(0), 0);
  assert.equal(quantizeKnots(2), 0); // ~1.7 kt
  assert.equal(quantizeKnots(11.5), 10); // ~10 kt
  assert.equal(quantizeKnots(null), 0);
  assert.equal(quantizeKnots(NaN), 0);
});

test('hasWindBarb is true only when a barb (not calm/empty) renders', () => {
  assert.equal(hasWindBarb(15, 90), true);
  assert.equal(hasWindBarb(2, 90), false); // rounds to calm
  assert.equal(hasWindBarb(0, 90), false);
  assert.equal(hasWindBarb(15, null), false); // no direction
  assert.equal(hasWindBarb(null, 90), false);
});
