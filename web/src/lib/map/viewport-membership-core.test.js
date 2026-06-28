import { test } from 'node:test';
import assert from 'node:assert/strict';
import { trailIntersectsBBox } from './viewport-membership-core.js';

// bbox covering roughly the Front Range; lon is negative (west).
const BBOX = { swLat: 39, swLon: -106, neLat: 41, neLon: -104 };

test('station whose head is inside the bbox is a member', () => {
  const positions = [{ lat: 40, lon: -105 }];
  assert.equal(trailIntersectsBBox(positions, BBOX), true);
});

test('moving station keeps membership while an older breadcrumb is still in view', () => {
  // Newest fix (positions[0]) has walked east out of the bbox, but earlier
  // breadcrumbs remain inside -- the #413 regression case.
  const positions = [
    { lat: 40, lon: -103.0 }, // head, outside
    { lat: 40, lon: -103.5 }, // outside
    { lat: 40, lon: -104.2 }, // inside
    { lat: 40, lon: -105.0 }, // inside
  ];
  assert.equal(trailIntersectsBBox(positions, BBOX), true);
});

test('station drops out only once the whole track has left view', () => {
  const positions = [
    { lat: 40, lon: -88 },
    { lat: 40, lon: -89 },
  ];
  assert.equal(trailIntersectsBBox(positions, BBOX), false);
});

test('bbox edges are inclusive on both layers', () => {
  assert.equal(trailIntersectsBBox([{ lat: 39, lon: -106 }], BBOX), true);
  assert.equal(trailIntersectsBBox([{ lat: 41, lon: -104 }], BBOX), true);
});

test('empty or missing inputs are non-members, not errors', () => {
  assert.equal(trailIntersectsBBox([], BBOX), false);
  assert.equal(trailIntersectsBBox(null, BBOX), false);
  assert.equal(trailIntersectsBBox([{ lat: 40, lon: -105 }], null), false);
});
