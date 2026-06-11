import { test } from 'node:test';
import assert from 'node:assert/strict';
import { buildWindBarb, quantizeKnots } from './wind-barb-glyph.js';

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

// Pull the y1 of the first <line class="wb-barb"> out of the markup so we
// can assert WHERE a barb sits, not just how many there are.
const firstBarbY1 = (svg) => {
  const m = svg.match(/<line x1="0" y1="(-?\d+(?:\.\d+)?)"[^>]*class="wb-barb"/);
  return m ? Number(m[1]) : null;
};

test('a lone half barb is inset one step from the tip', () => {
  // 6 mph → 5 kt → single half barb, inset; 11.5 mph → 10 kt → full barb
  // at the tip. The half barb must sit closer to the station (greater y).
  const halfY = firstBarbY1(buildWindBarb(6, 0));
  const fullAtTipY = firstBarbY1(buildWindBarb(11.5, 0));
  assert.ok(halfY != null && fullAtTipY != null);
  assert.ok(halfY > fullAtTipY, `expected inset half (${halfY}) below tip (${fullAtTipY})`);
});

test('calm renders identically regardless of direction', () => {
  // The open ring has no staff to orient, so direction must not matter.
  assert.equal(buildWindBarb(0, 0), buildWindBarb(0, 270));
  assert.doesNotMatch(buildWindBarb(0, 90), /rotate/);
});

test('quantizeKnots rounds mph to the nearest 5 kt', () => {
  assert.equal(quantizeKnots(0), 0);
  assert.equal(quantizeKnots(2), 0); // ~1.7 kt
  assert.equal(quantizeKnots(11.5), 10); // ~10 kt
  assert.equal(quantizeKnots(null), 0);
  assert.equal(quantizeKnots(NaN), 0);
});
