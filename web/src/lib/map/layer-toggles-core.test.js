import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
  LAYER_TOGGLES_DEFAULTS,
  parseLayerToggles,
} from './layer-toggles-core.js';

test('parseLayerToggles returns defaults for missing/empty input', () => {
  assert.deepEqual(parseLayerToggles(null), LAYER_TOGGLES_DEFAULTS);
  assert.deepEqual(parseLayerToggles(undefined), LAYER_TOGGLES_DEFAULTS);
  assert.deepEqual(parseLayerToggles(''), LAYER_TOGGLES_DEFAULTS);
});

test('parseLayerToggles returns defaults for corrupt JSON', () => {
  assert.deepEqual(parseLayerToggles('{not valid'), LAYER_TOGGLES_DEFAULTS);
  assert.deepEqual(parseLayerToggles('null'), LAYER_TOGGLES_DEFAULTS);
  assert.deepEqual(parseLayerToggles('42'), LAYER_TOGGLES_DEFAULTS);
  assert.deepEqual(parseLayerToggles('"trails"'), LAYER_TOGGLES_DEFAULTS);
});

test('parseLayerToggles preserves saved values', () => {
  const raw = JSON.stringify({ ...LAYER_TOGGLES_DEFAULTS, trails: false, fixedPoints: false });
  const got = parseLayerToggles(raw);
  assert.equal(got.trails, false);
  assert.equal(got.fixedPoints, false);
  assert.equal(got.stations, true);
});

test('parseLayerToggles merges a partial blob over defaults (forward compat)', () => {
  // An old blob predating the fixedPoints toggle: the missing key must fall
  // back to its default rather than becoming undefined.
  const raw = JSON.stringify({ stations: true, trails: false });
  const got = parseLayerToggles(raw);
  assert.equal(got.trails, false);
  assert.equal(got.fixedPoints, LAYER_TOGGLES_DEFAULTS.fixedPoints);
  assert.equal(got.rfOnly, LAYER_TOGGLES_DEFAULTS.rfOnly);
});

test('parseLayerToggles ignores stale extra keys harmlessly', () => {
  const raw = JSON.stringify({ trails: false, removedLegacyToggle: true });
  const got = parseLayerToggles(raw);
  assert.equal(got.trails, false);
  assert.equal(got.removedLegacyToggle, true); // carried but unread by consumers
  assert.equal(got.stations, true);
});

test('parseLayerToggles never returns the shared defaults object', () => {
  const got = parseLayerToggles(null);
  got.trails = false;
  assert.equal(LAYER_TOGGLES_DEFAULTS.trails, true);
});
