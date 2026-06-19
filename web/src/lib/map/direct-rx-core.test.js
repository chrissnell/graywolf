import { test } from 'node:test';
import assert from 'node:assert/strict';
import { directHeardWithin } from './direct-rx-core.js';

test('station heard directly within the window qualifies', () => {
  const nowMs = 1_000_000;
  const station = { last_direct_heard: new Date(nowMs - 10_000).toISOString() };
  assert.equal(directHeardWithin(station, nowMs - 60_000), true);
});

test('station last heard directly before the cutoff is excluded', () => {
  const nowMs = 1_000_000;
  const station = { last_direct_heard: new Date(nowMs - 120_000).toISOString() };
  assert.equal(directHeardWithin(station, nowMs - 60_000), false);
});

test('station never heard directly is excluded', () => {
  // Zero-time / missing last_direct_heard means never heard directly.
  assert.equal(directHeardWithin({ last_direct_heard: '0001-01-01T00:00:00Z' }, 0), false);
  assert.equal(directHeardWithin({}, 0), false);
  assert.equal(directHeardWithin(null, 0), false);
});
