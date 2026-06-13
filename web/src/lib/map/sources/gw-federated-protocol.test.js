import test from 'node:test';
import assert from 'node:assert/strict';
import { rankCoveringSlugs } from './gw-federated-protocol.js';

// tileToBBox returns [southLat, westLon, northLat, eastLon]; the bounds
// map holds [west, south, east, north]. These fixtures follow those
// conventions.
const bounds = new Map([
  ['world', [-180, -85, 180, 85]],
  ['state/colorado', [-109, 37, -102, 41]],
]);

test('rankCoveringSlugs: ranks smaller (more specific) bbox first, world last', () => {
  const tileBBox = [38, -105, 40, -103]; // inside Colorado
  const ranked = rankCoveringSlugs(tileBBox, new Set(['world', 'state/colorado']), bounds);
  assert.deepEqual(ranked, ['state/colorado', 'world']);
});

test('rankCoveringSlugs: omits non-intersecting slugs', () => {
  const farTile = [-40, 100, -38, 102]; // southern hemisphere, far from CO
  const ranked = rankCoveringSlugs(farTile, new Set(['state/colorado', 'world']), bounds);
  assert.deepEqual(ranked, ['world']);
});

test('rankCoveringSlugs: empty when nothing covers the tile', () => {
  const noBounds = new Map();
  const ranked = rankCoveringSlugs([0, 0, 1, 1], new Set(['state/colorado']), noBounds);
  assert.deepEqual(ranked, []);
});
