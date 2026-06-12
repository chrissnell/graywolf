import test from 'node:test';
import assert from 'node:assert/strict';
import { DBZ_THRESHOLDS, DBZ_COLORS, fillColorExpression } from './radar-source.js';

test('has NWS breakpoints 5..75 by 5', () => {
  assert.deepEqual(DBZ_THRESHOLDS, [5, 10, 15, 20, 25, 30, 35, 40, 45, 50, 55, 60, 65, 70, 75]);
});

test('maps every threshold to a hex color', () => {
  for (const t of DBZ_THRESHOLDS) assert.match(DBZ_COLORS[t], /^#[0-9a-f]{6}$/i);
});

test('builds a maplibre step expression keyed on dbz', () => {
  const expr = fillColorExpression();
  assert.equal(expr[0], 'step');
  assert.deepEqual(expr[1], ['get', 'dbz']);
});
